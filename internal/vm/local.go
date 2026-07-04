package vm

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

func generateNetdevArgs(sshPort int, portForward []string) string {
	var netdevArgs strings.Builder
	netdevArgs.WriteString(fmt.Sprintf("user,id=net0,hostfwd=tcp::%d-:22", sshPort))

	for _, mapping := range portForward {
		netdevArgs.WriteString(",")
		netdevArgs.WriteString(mapping)
	}

	return netdevArgs.String()
}

var knownImages = map[string]string{
	"ubuntu-24.04": "https://cloud-images.ubuntu.com/releases/24.04/release/ubuntu-24.04-server-cloudimg-arm64.img",
	"ubuntu-22.04": "https://cloud-images.ubuntu.com/releases/22.04/release/ubuntu-22.04-server-cloudimg-arm64.img",
	"ubuntu-20.04": "https://cloud-images.ubuntu.com/releases/20.04/release/focal-server-cloudimg-arm64.img",
}

type cmdExecutor interface {
	RunCmd(ctx context.Context, name string, args ...string) error
	StartDetached(logPath, name string, args ...string) error
}

type realCmdExecutor struct{}

func (r *realCmdExecutor) RunCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (r *realCmdExecutor) StartDetached(logPath, name string, args ...string) error {
	f, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("open qemu log: %w", err)
	}
	cmd := exec.Command(name, args...)
	cmd.Stdout = f
	cmd.Stderr = f
	if err := cmd.Start(); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return nil
}

type localVMRecord struct {
	pid         int
	sshPort     int
	overlayPath string
	seedISOPath string
}

// LocalProvider implements VMProvider and ImageProvider using local QEMU VMs.
type LocalProvider struct {
	dataDir string
	exec    cmdExecutor
	mu      sync.Mutex
	vms     map[string]*localVMRecord
}

// NewLocalProvider creates a LocalProvider backed by dataDir.
func NewLocalProvider(dataDir string) *LocalProvider {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		abs = dataDir
	}
	return &LocalProvider{
		dataDir: abs,
		exec:    &realCmdExecutor{},
		vms:     make(map[string]*localVMRecord),
	}
}

func (p *LocalProvider) CreateBuildVM(ctx context.Context, baseImage, authorizedKey string) (*VM, error) {
	tmpDir, err := os.MkdirTemp("", "agentsdx-build-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup := func() { os.RemoveAll(tmpDir) }

	userData := fmt.Sprintf("#cloud-config\nusers:\n  - name: root\n    ssh_authorized_keys:\n      - %s\n", authorizedKey)
	if err := os.WriteFile(filepath.Join(tmpDir, "user-data"), []byte(userData), 0644); err != nil {
		cleanup()
		return nil, fmt.Errorf("write user-data: %w", err)
	}

	metaData := "instance-id: agentsdx-build\nlocal-hostname: agentsdx-build\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "meta-data"), []byte(metaData), 0644); err != nil {
		cleanup()
		return nil, fmt.Errorf("write meta-data: %w", err)
	}

	seedISO := filepath.Join(tmpDir, "seed.iso")
	if err := p.exec.RunCmd(ctx, "hdiutil", "makehybrid", "-o", seedISO, "-joliet", "-iso", "-default-volume-name", "cidata", tmpDir); err != nil {
		cleanup()
		return nil, fmt.Errorf("create seed iso: %w", err)
	}

	resolvedImage, err := p.resolveBaseImage(ctx, baseImage)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("resolve base image: %w", err)
	}

	overlayPath := filepath.Join(tmpDir, "build-overlay.qcow2")
	if err := p.exec.RunCmd(ctx, "qemu-img", "create", "-f", "qcow2", "-b", resolvedImage, "-F", "qcow2", overlayPath); err != nil {
		cleanup()
		return nil, fmt.Errorf("create overlay: %w", err)
	}
	if err := p.exec.RunCmd(ctx, "qemu-img", "resize", overlayPath, "20G"); err != nil {
		cleanup()
		return nil, fmt.Errorf("resize overlay: %w", err)
	}

	port, err := findFreePort()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("find free port: %w", err)
	}

	vmID := uuid.New().String()
	qemuLog := filepath.Join(tmpDir, "qemu.log")
	pidFile := filepath.Join(tmpDir, "qemu.pid")
	log.Printf("local provider: starting build VM %s (logs: %s)", vmID, qemuLog)

	if err := p.exec.StartDetached(qemuLog, "qemu-system-aarch64",
		"-nographic", "-machine", "virt", "-accel", "hvf", "-cpu", "host",
		"-m", "2048", "-smp", "2",
		"-bios", "/opt/homebrew/share/qemu/edk2-aarch64-code.fd",
		"-drive", fmt.Sprintf("if=virtio,format=qcow2,file=%s", overlayPath),
		"-drive", fmt.Sprintf("if=virtio,format=raw,file=%s", seedISO),
		"-device", "virtio-net-pci,netdev=net0",
		"-netdev", fmt.Sprintf("user,id=net0,hostfwd=tcp::%d-:22", port),
		"-pidfile", pidFile,
	); err != nil {
		cleanup()
		return nil, fmt.Errorf("start qemu: %w", err)
	}

	if err := dialSSHPortReady(ctx, port); err != nil {
		cleanup()
		return nil, fmt.Errorf("wait for ssh: %w", err)
	}

	pid, err := readPidFile(pidFile)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("read pid file: %w", err)
	}

	p.mu.Lock()
	p.vms[vmID] = &localVMRecord{pid: pid, sshPort: port, overlayPath: overlayPath, seedISOPath: seedISO}
	p.mu.Unlock()

	return &VM{ID: vmID, IPAddress: "127.0.0.1", SSHPort: port, State: VMStateRunning}, nil
}

func (p *LocalProvider) SnapshotVM(ctx context.Context, vmID, snapshotName string) (string, error) {
	p.mu.Lock()
	rec, ok := p.vms[vmID]
	if ok {
		delete(p.vms, vmID)
	}
	p.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("vm %q not found", vmID)
	}

	killProcess(rec.pid)

	snapshotsDir := filepath.Join(p.dataDir, "qemu", "snapshots")
	if err := os.MkdirAll(snapshotsDir, 0755); err != nil {
		return "", fmt.Errorf("create snapshots dir: %w", err)
	}

	snapshotPath := filepath.Join(snapshotsDir, snapshotName+".qcow2")
	if err := p.exec.RunCmd(ctx, "qemu-img", "convert", "-O", "qcow2", rec.overlayPath, snapshotPath); err != nil {
		return "", fmt.Errorf("convert overlay: %w", err)
	}

	os.Remove(rec.overlayPath)
	os.Remove(rec.seedISOPath)
	os.Remove(filepath.Dir(rec.overlayPath)) //nolint:errcheck

	return snapshotPath, nil
}

func (p *LocalProvider) DestroyBuildVM(ctx context.Context, vmID string) error {
	return p.destroyQemuVM(vmID)
}

func (p *LocalProvider) CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error) {
	vmID := uuid.New().String()

	sessionsDir := filepath.Join(p.dataDir, "qemu", "sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		return nil, fmt.Errorf("create sessions dir: %w", err)
	}

	overlayPath := filepath.Join(sessionsDir, vmID+"-overlay.qcow2")
	if err := p.exec.RunCmd(ctx, "qemu-img", "create", "-f", "qcow2", "-b", req.ImageID, "-F", "qcow2", overlayPath); err != nil {
		return nil, fmt.Errorf("create session overlay: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "agentsdx-session-*")
	if err != nil {
		os.Remove(overlayPath)
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup := func() {
		os.Remove(overlayPath)
		os.RemoveAll(tmpDir)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "user-data"), []byte(req.UserData), 0644); err != nil {
		cleanup()
		return nil, fmt.Errorf("write user-data: %w", err)
	}

	metaData := fmt.Sprintf("instance-id: %s\nlocal-hostname: agentsdx-session\n", vmID)
	if err := os.WriteFile(filepath.Join(tmpDir, "meta-data"), []byte(metaData), 0644); err != nil {
		cleanup()
		return nil, fmt.Errorf("write meta-data: %w", err)
	}

	seedISO := filepath.Join(tmpDir, "seed.iso")
	if err := p.exec.RunCmd(ctx, "hdiutil", "makehybrid", "-o", seedISO, "-joliet", "-iso", "-default-volume-name", "cidata", tmpDir); err != nil {
		cleanup()
		return nil, fmt.Errorf("create seed iso: %w", err)
	}

	port, err := findFreePort()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("find free port: %w", err)
	}

	qemuLog := filepath.Join(tmpDir, "qemu.log")
	pidFile := filepath.Join(tmpDir, "qemu.pid")
	log.Printf("local provider: starting session VM %s (logs: %s)", vmID, qemuLog)

	if err := p.exec.StartDetached(qemuLog, "qemu-system-aarch64",
		"-nographic", "-machine", "virt", "-accel", "hvf", "-cpu", "host",
		"-m", "2048", "-smp", "2",
		"-bios", "/opt/homebrew/share/qemu/edk2-aarch64-code.fd",
		"-drive", fmt.Sprintf("if=virtio,format=qcow2,file=%s", overlayPath),
		"-drive", fmt.Sprintf("if=virtio,format=raw,file=%s", seedISO),
		"-device", "virtio-net-pci,netdev=net0",
		"-netdev", generateNetdevArgs(port, req.PortForward),
		"-pidfile", pidFile,
	); err != nil {
		cleanup()
		return nil, fmt.Errorf("start qemu: %w", err)
	}

	if err := dialSSHPortReady(ctx, port); err != nil {
		cleanup()
		return nil, fmt.Errorf("wait for ssh: %w", err)
	}

	pid, err := readPidFile(pidFile)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("read pid file: %w", err)
	}

	p.mu.Lock()
	p.vms[vmID] = &localVMRecord{pid: pid, sshPort: port, overlayPath: overlayPath, seedISOPath: seedISO}
	p.mu.Unlock()

	return &VM{ID: vmID, IPAddress: "127.0.0.1", SSHPort: port, State: VMStateStarting}, nil
}

func (p *LocalProvider) GetVM(ctx context.Context, vmID string) (*VM, error) {
	p.mu.Lock()
	rec, ok := p.vms[vmID]
	p.mu.Unlock()
	if !ok {
		return &VM{ID: vmID, State: VMStateUnknown}, nil
	}
	state := VMStateUnknown
	if syscall.Kill(rec.pid, 0) == nil {
		state = VMStateRunning
	}
	return &VM{ID: vmID, IPAddress: "127.0.0.1", SSHPort: rec.sshPort, State: state}, nil
}

func (p *LocalProvider) DestroyVM(ctx context.Context, vmID string) error {
	return p.destroyQemuVM(vmID)
}

func (p *LocalProvider) destroyQemuVM(vmID string) error {
	p.mu.Lock()
	rec, ok := p.vms[vmID]
	if ok {
		delete(p.vms, vmID)
	}
	p.mu.Unlock()
	if !ok {
		return nil
	}
	killProcess(rec.pid)
	os.Remove(rec.overlayPath)
	os.Remove(rec.seedISOPath)
	os.Remove(filepath.Dir(rec.overlayPath)) //nolint:errcheck
	return nil
}

func (p *LocalProvider) resolveBaseImage(ctx context.Context, baseImage string) (string, error) {
	if filepath.IsAbs(baseImage) {
		if _, err := os.Stat(baseImage); err != nil {
			return "", fmt.Errorf("image file not found: %s", baseImage)
		}
		return baseImage, nil
	}
	url, ok := knownImages[baseImage]
	if !ok {
		names := make([]string, 0, len(knownImages))
		for k := range knownImages {
			names = append(names, k)
		}
		sort.Strings(names)
		return "", fmt.Errorf("unknown image %q — use an absolute path or one of: %s", baseImage, strings.Join(names, ", "))
	}
	cacheDir := filepath.Join(p.dataDir, "qemu", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}
	cachePath := filepath.Join(cacheDir, baseImage+".img")
	if _, err := os.Stat(cachePath); err == nil {
		log.Printf("local provider: using cached image %s", cachePath)
		return cachePath, nil
	}
	log.Printf("local provider: downloading %s from %s", baseImage, url)
	if err := downloadToFile(ctx, url, cachePath); err != nil {
		return "", fmt.Errorf("download %s: %w", baseImage, err)
	}
	return cachePath, nil
}

func downloadToFile(ctx context.Context, url, destPath string) error {
	tmpPath := destPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		f.Close()
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.Close()
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		f.Close()
		return fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("write image: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, destPath)
}

func findFreePort() (int, error) {
	for i := 0; i < 10; i++ {
		port := 10000 + rand.Intn(10000)
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			l.Close()
			return port, nil
		}
	}
	return 0, fmt.Errorf("could not find free port after 10 attempts")
}

func dialSSHPortReady(ctx context.Context, port int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("ssh port %d not ready after 3 minutes", port)
}

func readPidFile(path string) (int, error) {
	for i := 0; i < 10; i++ {
		data, err := os.ReadFile(path)
		if err == nil {
			var pid int
			if _, err := fmt.Sscanf(string(data), "%d", &pid); err == nil && pid > 0 {
				return pid, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return 0, fmt.Errorf("could not read pid from %s after 10 attempts", path)
}

func killProcess(pid int) {
	syscall.Kill(pid, syscall.SIGTERM) //nolint:errcheck
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	syscall.Kill(pid, syscall.SIGKILL) //nolint:errcheck
}
