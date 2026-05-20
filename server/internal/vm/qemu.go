package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

// QEMUProvider implements VMProvider using QEMU on macOS.
// Networking uses vmnet-shared (Homebrew QEMU includes the required entitlement).
// The VM reports its IP address back to the server via a cloud-init runcmd callback,
// so GetVM returns VMStateRunning without an IPAddress; the session manager detects
// readiness via the /sessions/{id}/ready callback instead of polling the provider.
type QEMUProvider struct {
	images   *ImageStore
	isoDir   string
	stateDir string
}

// NewQEMUProvider creates a QEMUProvider. stateDir holds per-VM JSON records and
// snapshot qcow2 files; isoDir holds temporary NoCloud ISOs.
func NewQEMUProvider(images *ImageStore, isoDir, stateDir string) *QEMUProvider {
	return &QEMUProvider{images: images, isoDir: isoDir, stateDir: stateDir}
}

type qemuVMRecord struct {
	PID      int    `json:"pid"`
	Snapshot string `json:"snapshot"`
	ISOPath  string `json:"iso_path"`
}

// CreateVM creates a copy-on-write snapshot of the profile's qcow2 image, attaches
// a NoCloud ISO, and boots QEMU detached from the server process.
func (p *QEMUProvider) CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error) {
	binary := qemuBinaryName()
	if _, err := exec.LookPath(binary); err != nil {
		return nil, fmt.Errorf("%s not found in PATH: install QEMU (brew install qemu)", binary)
	}

	basePath, err := p.images.GetQEMUPath(req.ProfileName)
	if err != nil {
		return nil, fmt.Errorf("resolve image: %w", err)
	}

	vmName := fmt.Sprintf("agentsdx-%s-%d", req.ProfileName, time.Now().UnixMilli())

	snapshotPath := filepath.Join(p.stateDir, vmName+".qcow2")
	out, err := exec.CommandContext(ctx, "qemu-img", "create",
		"-f", "qcow2",
		"-b", basePath,
		"-F", "qcow2",
		snapshotPath,
	).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("create snapshot: %w\n%s", err, out)
	}

	isoPath, err := WriteNoCloudISO(
		p.isoDir,
		NoCloudMetaData(vmName),
		req.UserData,
	)
	if err != nil {
		os.Remove(snapshotPath)
		return nil, fmt.Errorf("write nocloud iso: %w", err)
	}

	args := buildQEMUArgs(vmName, snapshotPath, isoPath)
	cmd := exec.Command(binary, args...)
	// Detach from the server's process group so QEMU survives server restarts.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		os.Remove(snapshotPath)
		os.Remove(isoPath)
		return nil, fmt.Errorf("start qemu: %w", err)
	}

	rec := qemuVMRecord{
		PID:      cmd.Process.Pid,
		Snapshot: snapshotPath,
		ISOPath:  isoPath,
	}
	if err := p.saveRecord(vmName, rec); err != nil {
		cmd.Process.Kill()
		os.Remove(snapshotPath)
		return nil, fmt.Errorf("save vm record: %w", err)
	}

	return &VM{ID: vmName, State: VMStateStarting}, nil
}

// DestroyVM kills the QEMU process and removes the snapshot and ISO.
func (p *QEMUProvider) DestroyVM(ctx context.Context, vmID string) error {
	rec, err := p.loadRecord(vmID)
	if err != nil {
		return nil // already gone
	}

	syscall.Kill(rec.PID, syscall.SIGTERM)
	time.Sleep(500 * time.Millisecond)
	syscall.Kill(rec.PID, syscall.SIGKILL)

	os.Remove(rec.Snapshot)
	os.Remove(rec.ISOPath)
	os.Remove(p.recordPath(vmID))
	return nil
}

// GetVM checks whether the QEMU process is still alive. It does not return an
// IPAddress; IP is reported via the /sessions/{id}/ready callback in cloud-init.
func (p *QEMUProvider) GetVM(ctx context.Context, vmID string) (*VM, error) {
	rec, err := p.loadRecord(vmID)
	if err != nil {
		return &VM{ID: vmID, State: VMStateUnknown}, nil
	}

	if err := syscall.Kill(rec.PID, 0); err == syscall.ESRCH {
		return &VM{ID: vmID, State: VMStateStopped}, nil
	}

	return &VM{ID: vmID, State: VMStateRunning}, nil
}

func (p *QEMUProvider) saveRecord(vmID string, rec qemuVMRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return os.WriteFile(p.recordPath(vmID), data, 0600)
}

func (p *QEMUProvider) loadRecord(vmID string) (qemuVMRecord, error) {
	data, err := os.ReadFile(p.recordPath(vmID))
	if err != nil {
		return qemuVMRecord{}, err
	}
	var rec qemuVMRecord
	return rec, json.Unmarshal(data, &rec)
}

func (p *QEMUProvider) recordPath(vmID string) string {
	return filepath.Join(p.stateDir, vmID+".json")
}

func qemuBinaryName() string {
	if runtime.GOARCH == "arm64" {
		return "qemu-system-aarch64"
	}
	return "qemu-system-x86_64"
}

func buildQEMUArgs(vmName, snapshotPath, isoPath string) []string {
	args := []string{
		"-name", vmName,
		"-m", "2048",
		"-smp", "2",
		"-accel", "hvf",
		"-drive", fmt.Sprintf("file=%s,if=virtio,cache=writeback", snapshotPath),
		"-drive", fmt.Sprintf("file=%s,if=virtio,media=cdrom,readonly=on", isoPath),
		"-netdev", "vmnet-shared,id=vmnet0",
		"-device", fmt.Sprintf("virtio-net-pci,netdev=vmnet0,mac=%s", deterministicMAC(vmName)),
		"-display", "none",
	}

	if runtime.GOARCH == "arm64" {
		args = append(args,
			"-M", "virt",
			"-cpu", "host",
			"-drive", fmt.Sprintf("if=pflash,format=raw,readonly=on,file=%s", edk2FirmwarePath()),
		)
	} else {
		args = append(args, "-M", "q35", "-cpu", "host")
	}

	return args
}

func edk2FirmwarePath() string {
	return "/opt/homebrew/share/qemu/edk2-aarch64-code.fd"
}

// deterministicMAC generates a locally-administered MAC from the VM name so the
// same name always produces the same MAC (useful for DHCP lease stability).
func deterministicMAC(vmName string) string {
	h := fnv.New32a()
	h.Write([]byte(vmName))
	n := h.Sum32()
	return fmt.Sprintf("52:54:00:%02x:%02x:%02x", (n>>16)&0xff, (n>>8)&0xff, n&0xff)
}
