package vm

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// cmdExecutor is an injectable interface for running external commands.
type cmdExecutor interface {
	RunCmd(ctx context.Context, name string, args ...string) error
	StartDetached(name string, args ...string) error
}

// realCmdExecutor implements cmdExecutor using os/exec.
type realCmdExecutor struct{}

func (r *realCmdExecutor) RunCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (r *realCmdExecutor) StartDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}

// LocalProvider implements VMProvider and ImageProvider using local QEMU VMs.
type LocalProvider struct {
	db      *sql.DB
	dataDir string
	exec    cmdExecutor
}

// NewLocalProvider creates a LocalProvider backed by the given DB and data directory.
func NewLocalProvider(db *sql.DB, dataDir string) *LocalProvider {
	return &LocalProvider{db: db, dataDir: dataDir, exec: &realCmdExecutor{}}
}

// --- ImageProvider methods ---

// CreateBuildVM creates a QEMU VM from baseImage and waits until SSH is ready.
func (p *LocalProvider) CreateBuildVM(ctx context.Context, baseImage, authorizedKey string) (*VM, error) {
	tmpDir, err := os.MkdirTemp("", "agentsdx-build-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup := func() { os.RemoveAll(tmpDir) }

	// Write cloud-init user-data
	userData := fmt.Sprintf("#cloud-config\nusers:\n  - name: root\n    ssh_authorized_keys:\n      - %s\n", authorizedKey)
	if err := os.WriteFile(filepath.Join(tmpDir, "user-data"), []byte(userData), 0644); err != nil {
		cleanup()
		return nil, fmt.Errorf("write user-data: %w", err)
	}

	// Write cloud-init meta-data
	metaData := "instance-id: agentsdx-build\nlocal-hostname: agentsdx-build\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "meta-data"), []byte(metaData), 0644); err != nil {
		cleanup()
		return nil, fmt.Errorf("write meta-data: %w", err)
	}

	// Create seed ISO
	seedISO := filepath.Join(tmpDir, "seed.iso")
	if err := p.exec.RunCmd(ctx, "hdiutil", "makehybrid", "-o", seedISO, "-joliet", "-iso", "-default-volume-name", "cidata", tmpDir); err != nil {
		cleanup()
		return nil, fmt.Errorf("create seed iso: %w", err)
	}

	// Create overlay
	overlayPath := filepath.Join(tmpDir, "build-overlay.qcow2")
	if err := p.exec.RunCmd(ctx, "qemu-img", "create", "-f", "qcow2", "-b", baseImage, "-F", "qcow2", overlayPath); err != nil {
		cleanup()
		return nil, fmt.Errorf("create overlay: %w", err)
	}

	// Pick free port
	port, err := findFreePort()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("find free port: %w", err)
	}

	// Generate VM ID
	vmID := uuid.New().String()

	// Launch QEMU
	pidFile := filepath.Join(tmpDir, "qemu.pid")
	if err := p.exec.StartDetached(
		"qemu-system-aarch64",
		"-nographic",
		"-machine", "virt",
		"-accel", "hvf",
		"-cpu", "host",
		"-m", "2048",
		"-smp", "2",
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

	// Wait for SSH readiness
	if err := dialSSHPortReady(ctx, port); err != nil {
		cleanup()
		return nil, fmt.Errorf("wait for ssh: %w", err)
	}

	// Read PID from pidfile
	pid, err := readPidFile(pidFile)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("read pid file: %w", err)
	}

	// Insert into DB
	_, err = p.db.ExecContext(ctx,
		`INSERT INTO qemu_vms (id, pid, ssh_port, overlay_path, seed_iso_path) VALUES (?, ?, ?, ?, ?)`,
		vmID, pid, port, overlayPath, seedISO,
	)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("insert qemu_vm: %w", err)
	}

	return &VM{ID: vmID, IPAddress: "127.0.0.1", SSHPort: port, State: VMStateRunning}, nil
}

// SnapshotVM stops the build VM, converts the overlay to a snapshot image, and cleans up.
func (p *LocalProvider) SnapshotVM(ctx context.Context, vmID, snapshotName string) (string, error) {
	var pid int
	var overlayPath, seedISOPath string
	err := p.db.QueryRowContext(ctx,
		`SELECT pid, overlay_path, seed_iso_path FROM qemu_vms WHERE id = ?`, vmID,
	).Scan(&pid, &overlayPath, &seedISOPath)
	if err != nil {
		return "", fmt.Errorf("query qemu_vm: %w", err)
	}

	killProcess(pid)

	snapshotsDir := filepath.Join(p.dataDir, "snapshots")
	if err := os.MkdirAll(snapshotsDir, 0755); err != nil {
		return "", fmt.Errorf("create snapshots dir: %w", err)
	}

	snapshotPath := filepath.Join(snapshotsDir, snapshotName+".qcow2")
	if err := p.exec.RunCmd(ctx, "qemu-img", "convert", "-O", "qcow2", overlayPath, snapshotPath); err != nil {
		return "", fmt.Errorf("convert overlay: %w", err)
	}

	os.Remove(overlayPath)
	os.Remove(seedISOPath)
	// Try to remove the parent temp dir if empty
	parentDir := filepath.Dir(overlayPath)
	os.Remove(parentDir) // nolint: errcheck (may not be empty, that's fine)

	if _, err := p.db.ExecContext(ctx, `DELETE FROM qemu_vms WHERE id = ?`, vmID); err != nil {
		return "", fmt.Errorf("delete qemu_vm: %w", err)
	}

	return snapshotPath, nil
}

// DestroyBuildVM kills the build VM process and cleans up all associated files.
func (p *LocalProvider) DestroyBuildVM(ctx context.Context, vmID string) error {
	return p.destroyQemuVM(ctx, vmID)
}

// --- VMProvider methods ---

// CreateVM creates a session VM from a snapshot image.
func (p *LocalProvider) CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error) {
	vmID := uuid.New().String()

	sessionsDir := filepath.Join(p.dataDir, "sessions")
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

	// Write user-data
	if err := os.WriteFile(filepath.Join(tmpDir, "user-data"), []byte(req.UserData), 0644); err != nil {
		cleanup()
		return nil, fmt.Errorf("write user-data: %w", err)
	}

	// Write meta-data
	metaData := "instance-id: agentsdx-session\nlocal-hostname: agentsdx-session\n"
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

	pidFile := filepath.Join(tmpDir, "qemu.pid")
	if err := p.exec.StartDetached(
		"qemu-system-aarch64",
		"-nographic",
		"-machine", "virt",
		"-accel", "hvf",
		"-cpu", "host",
		"-m", "2048",
		"-smp", "2",
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

	_, err = p.db.ExecContext(ctx,
		`INSERT INTO qemu_vms (id, pid, ssh_port, overlay_path, seed_iso_path) VALUES (?, ?, ?, ?, ?)`,
		vmID, pid, port, overlayPath, seedISO,
	)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("insert qemu_vm: %w", err)
	}

	return &VM{ID: vmID, IPAddress: "127.0.0.1", SSHPort: port, State: VMStateStarting}, nil
}

// GetVM returns the current state of a QEMU VM by checking if the process is alive.
func (p *LocalProvider) GetVM(ctx context.Context, vmID string) (*VM, error) {
	var pid, sshPort int
	err := p.db.QueryRowContext(ctx,
		`SELECT pid, ssh_port FROM qemu_vms WHERE id = ?`, vmID,
	).Scan(&pid, &sshPort)
	if err != nil {
		if err == sql.ErrNoRows {
			return &VM{ID: vmID, State: VMStateUnknown}, nil
		}
		return nil, fmt.Errorf("query qemu_vms: %w", err)
	}

	state := VMStateUnknown
	if syscall.Kill(pid, 0) == nil {
		state = VMStateRunning
	}

	return &VM{ID: vmID, IPAddress: "127.0.0.1", SSHPort: sshPort, State: state}, nil
}

// DestroyVM kills the session VM and cleans up all associated files.
func (p *LocalProvider) DestroyVM(ctx context.Context, vmID string) error {
	return p.destroyQemuVM(ctx, vmID)
}

// --- shared helpers ---

func (p *LocalProvider) destroyQemuVM(ctx context.Context, vmID string) error {
	var pid int
	var overlayPath, seedISOPath string
	err := p.db.QueryRowContext(ctx,
		`SELECT pid, overlay_path, seed_iso_path FROM qemu_vms WHERE id = ?`, vmID,
	).Scan(&pid, &overlayPath, &seedISOPath)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("query qemu_vm: %w", err)
	}

	killProcess(pid) // ignore if already dead

	os.Remove(overlayPath)
	os.Remove(seedISOPath)
	parentDir := filepath.Dir(overlayPath)
	os.Remove(parentDir) // nolint: errcheck

	_, err = p.db.ExecContext(ctx, `DELETE FROM qemu_vms WHERE id = ?`, vmID)
	return err
}

// findFreePort probes ports in 10000–20000 and returns the first available one.
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

// dialSSHPortReady retries a TCP dial to 127.0.0.1:<port> until success or context expiry.
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

// readPidFile retries reading and parsing a pid file up to 10 times with 500ms delays.
func readPidFile(path string) (int, error) {
	for i := 0; i < 10; i++ {
		data, err := os.ReadFile(path)
		if err == nil {
			var pid int
			_, scanErr := fmt.Sscanf(string(data), "%d", &pid)
			if scanErr == nil && pid > 0 {
				return pid, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return 0, fmt.Errorf("could not read pid from %s after 10 attempts", path)
}

// killProcess sends SIGTERM to the process and waits up to 10s, then sends SIGKILL.
func killProcess(pid int) {
	syscall.Kill(pid, syscall.SIGTERM) //nolint:errcheck
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return // process gone
		}
		time.Sleep(500 * time.Millisecond)
	}
	syscall.Kill(pid, syscall.SIGKILL) //nolint:errcheck
}
