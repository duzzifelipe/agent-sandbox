package vm_test

import (
	"context"
	"os/exec"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vm"
)

func skipIfNoVBoxManage(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("VBoxManage"); err != nil {
		t.Skip("VBoxManage not found in PATH — skipping integration test")
	}
}

func TestVirtualBoxProvider_ImplementsInterface(t *testing.T) {
	dir := t.TempDir()
	images := vm.NewImageStore(dir + "/images.json")
	var _ vm.VMProvider = vm.NewVirtualBoxProvider(images, dir)
}

func TestVirtualBoxProvider_GetVM_NotFound(t *testing.T) {
	skipIfNoVBoxManage(t)
	dir := t.TempDir()
	images := vm.NewImageStore(dir + "/images.json")
	provider := vm.NewVirtualBoxProvider(images, dir)

	_, err := provider.GetVM(context.Background(), "agentsdx-nonexistent-vm-xyz")
	if err == nil {
		t.Fatal("expected error for non-existent VM")
	}
}
