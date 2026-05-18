//go:build darwin && arm64

package applevz

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	vz "github.com/Code-Hex/vz/v3"

	"github.com/duck-labs/agentsdx-server/internal/vm"
)

// Provider implements vm.VMProvider using Apple's Virtualization.framework.
// Requires sudo or com.apple.vm.networking entitlement for NAT networking.
type Provider struct {
	images  *vm.ImageStore
	isoDir  string
	workDir string

	mu  sync.Mutex
	vms map[string]*vz.VirtualMachine
	ips map[string]string
}

// NewProvider creates an AppleVZ provider.
// isoDir holds per-VM NoCloud ISO subdirectories.
// workDir holds per-VM disk copies and EFI variable stores.
func NewProvider(images *vm.ImageStore, isoDir, workDir string) *Provider {
	return &Provider{
		images:  images,
		isoDir:  isoDir,
		workDir: workDir,
		vms:     make(map[string]*vz.VirtualMachine),
		ips:     make(map[string]string),
	}
}

// CreateVM copies the disk image, writes the NoCloud ISO, configures and starts the VM.
func (p *Provider) CreateVM(ctx context.Context, req vm.CreateVMRequest) (*vm.VM, error) {
	imgPath, err := p.images.GetAppleVZPath(req.ProfileName)
	if err != nil {
		return nil, fmt.Errorf("resolve image: %w", err)
	}

	vmName := fmt.Sprintf("agentsdx-%s-%d", req.ProfileName, time.Now().UnixMilli())

	diskPath := filepath.Join(p.workDir, vmName+".img")
	if err := copyFile(imgPath, diskPath); err != nil {
		return nil, fmt.Errorf("copy disk image: %w", err)
	}

	vmISODir := filepath.Join(p.isoDir, vmName)
	if err := os.MkdirAll(vmISODir, 0755); err != nil {
		os.Remove(diskPath)
		return nil, fmt.Errorf("create iso dir: %w", err)
	}
	isoPath, err := vm.WriteNoCloudISO(vmISODir, vm.NoCloudMetaData(vmName), req.UserData)
	if err != nil {
		os.Remove(diskPath)
		os.RemoveAll(vmISODir)
		return nil, fmt.Errorf("write nocloud iso: %w", err)
	}

	efiPath := filepath.Join(p.workDir, vmName+".efi")

	vzVM, err := buildVZVM(diskPath, isoPath, efiPath)
	if err != nil {
		os.Remove(diskPath)
		os.RemoveAll(vmISODir)
		return nil, fmt.Errorf("configure vm: %w", err)
	}

	if err := vzVM.Start(); err != nil {
		os.Remove(diskPath)
		os.RemoveAll(vmISODir)
		os.Remove(efiPath)
		return nil, fmt.Errorf("start vm: %w", err)
	}

	p.mu.Lock()
	p.vms[vmName] = vzVM
	p.mu.Unlock()

	return &vm.VM{ID: vmName, State: vm.VMStateStarting}, nil
}

// GetVM returns the current state of the VM. Returns VMStateRunning only when
// both the VZ machine state is Running AND an IP has been registered via RegisterIP.
func (p *Provider) GetVM(_ context.Context, vmID string) (*vm.VM, error) {
	p.mu.Lock()
	vzVM, ok := p.vms[vmID]
	ip := p.ips[vmID]
	p.mu.Unlock()

	if !ok {
		return &vm.VM{ID: vmID, State: vm.VMStateStopped}, nil
	}

	state := mapState(vzVM.State())
	if state == vm.VMStateRunning && ip == "" {
		state = vm.VMStateStarting
	}
	return &vm.VM{ID: vmID, State: state, IPAddress: ip}, nil
}

// DestroyVM stops the VM and removes its disk copy and NoCloud ISO.
func (p *Provider) DestroyVM(_ context.Context, vmID string) error {
	p.mu.Lock()
	vzVM, ok := p.vms[vmID]
	delete(p.vms, vmID)
	delete(p.ips, vmID)
	p.mu.Unlock()

	if ok && vzVM.CanStop() {
		_ = vzVM.Stop()
	}

	os.Remove(filepath.Join(p.workDir, vmID+".img"))
	os.Remove(filepath.Join(p.workDir, vmID+".efi"))
	os.RemoveAll(filepath.Join(p.isoDir, vmID))
	return nil
}

// RegisterIP stores the VM's IP address so GetVM can report VMStateRunning.
func (p *Provider) RegisterIP(_ context.Context, vmID, ip string) error {
	p.mu.Lock()
	p.ips[vmID] = ip
	p.mu.Unlock()
	return nil
}

func buildVZVM(diskPath, isoPath, efiPath string) (*vz.VirtualMachine, error) {
	efiVarStore, err := vz.NewEFIVariableStore(efiPath, vz.WithCreatingEFIVariableStore())
	if err != nil {
		return nil, fmt.Errorf("efi var store: %w", err)
	}
	bootLoader, err := vz.NewEFIBootLoader(vz.WithEFIVariableStore(efiVarStore))
	if err != nil {
		return nil, fmt.Errorf("boot loader: %w", err)
	}

	config, err := vz.NewVirtualMachineConfiguration(bootLoader, 2, 2*1024*1024*1024)
	if err != nil {
		return nil, fmt.Errorf("vm configuration: %w", err)
	}

	natAttach, err := vz.NewNATNetworkDeviceAttachment()
	if err != nil {
		return nil, fmt.Errorf("nat attachment: %w", err)
	}
	netDev, err := vz.NewVirtioNetworkDeviceConfiguration(natAttach)
	if err != nil {
		return nil, fmt.Errorf("net device: %w", err)
	}
	config.SetNetworkDevicesVirtualMachineConfiguration([]*vz.VirtioNetworkDeviceConfiguration{netDev})

	diskAttach, err := vz.NewDiskImageStorageDeviceAttachment(diskPath, false)
	if err != nil {
		return nil, fmt.Errorf("disk attachment: %w", err)
	}
	diskDev, err := vz.NewVirtioBlockDeviceConfiguration(diskAttach)
	if err != nil {
		return nil, fmt.Errorf("disk device: %w", err)
	}

	isoAttach, err := vz.NewDiskImageStorageDeviceAttachment(isoPath, true)
	if err != nil {
		return nil, fmt.Errorf("iso attachment: %w", err)
	}
	isoDev, err := vz.NewVirtioBlockDeviceConfiguration(isoAttach)
	if err != nil {
		return nil, fmt.Errorf("iso device: %w", err)
	}

	config.SetStorageDevicesVirtualMachineConfiguration([]vz.StorageDeviceConfiguration{diskDev, isoDev})

	valid, err := config.Validate()
	if err != nil {
		return nil, fmt.Errorf("config validate: %w", err)
	}
	if !valid {
		return nil, fmt.Errorf("vm configuration is invalid")
	}

	return vz.NewVirtualMachine(config)
}

func mapState(s vz.VirtualMachineState) string {
	switch s {
	case vz.VirtualMachineStateRunning:
		return vm.VMStateRunning
	case vz.VirtualMachineStateStopped, vz.VirtualMachineStateError:
		return vm.VMStateStopped
	case vz.VirtualMachineStateStarting:
		return vm.VMStateStarting
	default:
		return vm.VMStateUnknown
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
