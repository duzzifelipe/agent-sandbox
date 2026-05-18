//go:build darwin && arm64

package applevz

import (
	"context"
	"os"
	"testing"

	vz "github.com/Code-Hex/vz/v3"

	"github.com/duck-labs/agentsdx-server/internal/vm"
)

func TestProvider_RegisterIP_StoresAndReturns(t *testing.T) {
	p := &Provider{
		vms: make(map[string]*vz.VirtualMachine),
		ips: make(map[string]string),
	}

	if err := p.RegisterIP(context.Background(), "fake-vm", "192.168.64.5"); err != nil {
		t.Fatalf("RegisterIP: %v", err)
	}
	p.mu.Lock()
	ip := p.ips["fake-vm"]
	p.mu.Unlock()
	if ip != "192.168.64.5" {
		t.Errorf("ip: got %q, want %q", ip, "192.168.64.5")
	}
}

func TestProvider_GetVM_UnknownVMReturnsStopped(t *testing.T) {
	p := NewProvider(nil, t.TempDir(), t.TempDir())
	v, err := p.GetVM(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("GetVM: %v", err)
	}
	if v.State != vm.VMStateStopped {
		t.Errorf("State: got %q, want %q", v.State, vm.VMStateStopped)
	}
}

func TestProvider_DestroyVM_UnknownVMIsNoop(t *testing.T) {
	p := NewProvider(nil, t.TempDir(), t.TempDir())
	if err := p.DestroyVM(context.Background(), "nonexistent"); err != nil {
		t.Fatalf("DestroyVM of unknown vm should not error: %v", err)
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := dir + "/src.img"
	dst := dir + "/dst.img"

	if err := os.WriteFile(src, []byte("hello disk"), 0644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(data) != "hello disk" {
		t.Errorf("content: got %q, want %q", string(data), "hello disk")
	}
}
