package vm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalProvider_FindFreePort_ReturnsPortInRange(t *testing.T) {
	port, err := findFreePort()
	if err != nil {
		t.Fatalf("findFreePort() error: %v", err)
	}
	if port < 10000 || port >= 20000 {
		t.Errorf("findFreePort() = %d, want port in [10000, 20000)", port)
	}
}

func TestLocalProvider_GetVM_ProcessAlive_ReturnsRunning(t *testing.T) {
	p := &LocalProvider{dataDir: t.TempDir(), exec: &fakeCmdExecutor{}, vms: make(map[string]*localVMRecord)}
	vmID := "test-alive-vm"
	pid := os.Getpid()
	p.vms[vmID] = &localVMRecord{pid: pid, sshPort: 12345, overlayPath: "/tmp/o.qcow2", seedISOPath: "/tmp/s.iso"}

	vm, err := p.GetVM(context.Background(), vmID)
	if err != nil {
		t.Fatalf("GetVM() error: %v", err)
	}
	if vm.State != VMStateRunning {
		t.Errorf("GetVM().State = %q, want %q", vm.State, VMStateRunning)
	}
	if vm.SSHPort != 12345 {
		t.Errorf("GetVM().SSHPort = %d, want 12345", vm.SSHPort)
	}
}

func TestLocalProvider_GetVM_ProcessDead_ReturnsUnknown(t *testing.T) {
	p := &LocalProvider{dataDir: t.TempDir(), exec: &fakeCmdExecutor{}, vms: make(map[string]*localVMRecord)}
	vmID := "test-dead-vm"
	p.vms[vmID] = &localVMRecord{pid: 99999999, sshPort: 12346, overlayPath: "/tmp/o2.qcow2", seedISOPath: "/tmp/s2.iso"}

	vm, err := p.GetVM(context.Background(), vmID)
	if err != nil {
		t.Fatalf("GetVM() error: %v", err)
	}
	if vm.State != VMStateUnknown {
		t.Errorf("GetVM().State = %q, want %q", vm.State, VMStateUnknown)
	}
}

func TestLocalProvider_GetVM_NotFound_ReturnsUnknown(t *testing.T) {
	p := &LocalProvider{dataDir: t.TempDir(), exec: &fakeCmdExecutor{}, vms: make(map[string]*localVMRecord)}
	vm, err := p.GetVM(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("GetVM() error: %v", err)
	}
	if vm.State != VMStateUnknown {
		t.Errorf("GetVM().State = %q, want %q", vm.State, VMStateUnknown)
	}
}

func TestLocalProvider_ResolveBaseImage_AbsolutePath_Exists(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "base-*.img")
	if err != nil {
		t.Fatalf("create temp img: %v", err)
	}
	f.Close()

	p := &LocalProvider{dataDir: t.TempDir(), exec: &fakeCmdExecutor{}, vms: make(map[string]*localVMRecord)}
	got, err := p.resolveBaseImage(context.Background(), f.Name())
	if err != nil {
		t.Fatalf("resolveBaseImage() error: %v", err)
	}
	if got != f.Name() {
		t.Errorf("got %q, want %q", got, f.Name())
	}
}

func TestLocalProvider_ResolveBaseImage_AbsolutePath_Missing_ReturnsError(t *testing.T) {
	p := &LocalProvider{dataDir: t.TempDir(), exec: &fakeCmdExecutor{}, vms: make(map[string]*localVMRecord)}
	_, err := p.resolveBaseImage(context.Background(), "/nonexistent/path/image.img")
	if err == nil {
		t.Fatal("expected error for missing absolute path, got nil")
	}
}

func TestLocalProvider_ResolveBaseImage_KnownName_HitsCache(t *testing.T) {
	dataDir := t.TempDir()
	cacheDir := filepath.Join(dataDir, "qemu", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	cachedPath := filepath.Join(cacheDir, "ubuntu-24.04.img")
	if err := os.WriteFile(cachedPath, []byte("fake image"), 0644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	p := &LocalProvider{dataDir: dataDir, exec: &fakeCmdExecutor{}, vms: make(map[string]*localVMRecord)}
	got, err := p.resolveBaseImage(context.Background(), "ubuntu-24.04")
	if err != nil {
		t.Fatalf("resolveBaseImage() error: %v", err)
	}
	if got != cachedPath {
		t.Errorf("got %q, want %q", got, cachedPath)
	}
}

func TestLocalProvider_ResolveBaseImage_UnknownName_ReturnsError(t *testing.T) {
	p := &LocalProvider{dataDir: t.TempDir(), exec: &fakeCmdExecutor{}, vms: make(map[string]*localVMRecord)}
	_, err := p.resolveBaseImage(context.Background(), "debian-12")
	if err == nil {
		t.Fatal("expected error for unknown name, got nil")
	}
	if !strings.Contains(err.Error(), "unknown image") {
		t.Errorf("error %q should mention 'unknown image'", err.Error())
	}
}

type fakeCmdExecutor struct {
	runCalls   [][]string
	startCalls [][]string
	runErr     error
	startErr   error
}

func (f *fakeCmdExecutor) RunCmd(_ context.Context, name string, args ...string) error {
	call := append([]string{name}, args...)
	f.runCalls = append(f.runCalls, call)
	return f.runErr
}

func (f *fakeCmdExecutor) StartDetached(logPath, name string, args ...string) error {
	call := append([]string{name}, args...)
	f.startCalls = append(f.startCalls, call)
	return f.startErr
}
