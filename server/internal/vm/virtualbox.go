package vm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// VirtualBoxProvider implements VMProvider using VBoxManage.
// The Packer-built VM image must have VirtualBox Guest Additions installed
// so that guest properties (including the IP address) are available.
type VirtualBoxProvider struct {
	images *ImageStore
	isoDir string // directory for temporary NoCloud ISOs
}

// NewVirtualBoxProvider creates a VirtualBoxProvider.
// isoDir is used to write temporary NoCloud ISOs before attaching them to VMs.
func NewVirtualBoxProvider(images *ImageStore, isoDir string) *VirtualBoxProvider {
	return &VirtualBoxProvider{images: images, isoDir: isoDir}
}

// CreateVM imports the profile's OVA, configures host-only networking,
// attaches a NoCloud ISO with the authorized key, and starts the VM headlessly.
// The returned VM has State=VMStateStarting; call GetVM to poll until running.
func (p *VirtualBoxProvider) CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error) {
	if _, err := exec.LookPath("VBoxManage"); err != nil {
		return nil, fmt.Errorf("VBoxManage not found in PATH: VirtualBox must be installed to use the VirtualBox provider")
	}

	ovaPath, err := p.images.GetVirtualBoxPath(req.ProfileName)
	if err != nil {
		return nil, fmt.Errorf("resolve image: %w", err)
	}

	vmName := fmt.Sprintf("agentsdx-%s-%d", req.ProfileName, time.Now().UnixMilli())

	// Import OVA
	if _, err := runVBoxManage(ctx, "import", ovaPath, "--vsys", "0", "--vmname", vmName); err != nil {
		return nil, fmt.Errorf("import ova: %w", err)
	}

	// Configure host-only networking
	if _, err := runVBoxManage(ctx, "modifyvm", vmName, "--nic1", "hostonly", "--hostonlyadapter1", "vboxnet0"); err != nil {
		_ = p.forceDelete(ctx, vmName)
		return nil, fmt.Errorf("configure network: %w", err)
	}

	// Generate and attach NoCloud ISO
	isoPath, err := WriteNoCloudISO(
		p.isoDir,
		NoCloudMetaData(vmName),
		req.UserData,
	)
	if err != nil {
		_ = p.forceDelete(ctx, vmName)
		return nil, fmt.Errorf("generate nocloud iso: %w", err)
	}

	if _, err := runVBoxManage(ctx, "storageattach", vmName,
		"--storagectl", "IDE",
		"--port", "1",
		"--device", "0",
		"--type", "dvddrive",
		"--medium", isoPath,
	); err != nil {
		_ = p.forceDelete(ctx, vmName)
		_ = os.Remove(isoPath)
		return nil, fmt.Errorf("attach nocloud iso: %w", err)
	}

	// Start VM
	if _, err := runVBoxManage(ctx, "startvm", vmName, "--type", "headless"); err != nil {
		_ = p.forceDelete(ctx, vmName)
		return nil, fmt.Errorf("start vm: %w", err)
	}

	return &VM{ID: vmName, State: VMStateStarting}, nil
}

// DestroyVM powers off and deletes the VM unconditionally.
func (p *VirtualBoxProvider) DestroyVM(ctx context.Context, vmID string) error {
	return p.forceDelete(ctx, vmID)
}

// GetVM returns current state and IP of the VM.
// IPAddress is populated only when the VM is running and has reported its IP via Guest Additions.
func (p *VirtualBoxProvider) GetVM(ctx context.Context, vmID string) (*VM, error) {
	out, err := runVBoxManage(ctx, "showvminfo", vmID, "--machinereadable")
	if err != nil {
		return nil, fmt.Errorf("showvminfo: %w", err)
	}

	info := ParseVMInfo(out)
	rawState := info["VMState"]
	state := mapVMState(rawState)

	v := &VM{ID: vmID, State: state}

	if state == VMStateRunning {
		ipOut, err := runVBoxManage(ctx, "guestproperty", "get", vmID, "/VirtualBox/GuestInfo/Net/0/V4/IP")
		if err == nil {
			if ip, ok := ParseGuestProperty(ipOut); ok {
				v.IPAddress = ip
			}
		}
	}

	return v, nil
}

func (p *VirtualBoxProvider) forceDelete(ctx context.Context, vmID string) error {
	// Try graceful poweroff first; ignore errors (VM may already be off)
	_, _ = runVBoxManage(ctx, "controlvm", vmID, "poweroff")
	time.Sleep(500 * time.Millisecond)
	_, err := runVBoxManage(ctx, "unregistervm", vmID, "--delete")
	if err != nil {
		return fmt.Errorf("unregistervm: %w", err)
	}
	return nil
}

func mapVMState(raw string) string {
	switch strings.ToLower(raw) {
	case "running":
		return VMStateRunning
	case "poweroff", "aborted", "saved":
		return VMStateStopped
	case "starting", "restoring":
		return VMStateStarting
	default:
		return VMStateUnknown
	}
}
