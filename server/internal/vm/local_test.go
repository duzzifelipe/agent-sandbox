package vm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/db"
)

// fakeCmdExecutor records command invocations for testing.
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
	conn, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer conn.Close()

	p := &LocalProvider{db: conn, dataDir: t.TempDir(), exec: &fakeCmdExecutor{}}

	vmID := "test-alive-vm"
	pid := os.Getpid() // current process, guaranteed alive

	_, err = conn.Exec(
		`INSERT INTO qemu_vms (id, pid, ssh_port, overlay_path, seed_iso_path) VALUES (?, ?, ?, ?, ?)`,
		vmID, pid, 12345, "/tmp/overlay.qcow2", "/tmp/seed.iso",
	)
	if err != nil {
		t.Fatalf("insert row: %v", err)
	}

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
	conn, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer conn.Close()

	p := &LocalProvider{db: conn, dataDir: t.TempDir(), exec: &fakeCmdExecutor{}}

	vmID := "test-dead-vm"
	pid := 99999999 // extremely unlikely to exist

	_, err = conn.Exec(
		`INSERT INTO qemu_vms (id, pid, ssh_port, overlay_path, seed_iso_path) VALUES (?, ?, ?, ?, ?)`,
		vmID, pid, 12346, "/tmp/overlay2.qcow2", "/tmp/seed2.iso",
	)
	if err != nil {
		t.Fatalf("insert row: %v", err)
	}

	vm, err := p.GetVM(context.Background(), vmID)
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

	p := &LocalProvider{dataDir: t.TempDir(), exec: &fakeCmdExecutor{}}
	got, err := p.resolveBaseImage(context.Background(), f.Name())
	if err != nil {
		t.Fatalf("resolveBaseImage() error: %v", err)
	}
	if got != f.Name() {
		t.Errorf("got %q, want %q", got, f.Name())
	}
}

func TestLocalProvider_ResolveBaseImage_AbsolutePath_Missing_ReturnsError(t *testing.T) {
	p := &LocalProvider{dataDir: t.TempDir(), exec: &fakeCmdExecutor{}}
	_, err := p.resolveBaseImage(context.Background(), "/nonexistent/path/image.img")
	if err == nil {
		t.Fatal("expected error for missing absolute path, got nil")
	}
}

func TestLocalProvider_ResolveBaseImage_KnownName_HitsCache(t *testing.T) {
	dataDir := t.TempDir()
	cacheDir := filepath.Join(dataDir, "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	// Pre-populate cache
	cachedPath := filepath.Join(cacheDir, "ubuntu-24.04.img")
	if err := os.WriteFile(cachedPath, []byte("fake image"), 0644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	p := &LocalProvider{dataDir: dataDir, exec: &fakeCmdExecutor{}}
	got, err := p.resolveBaseImage(context.Background(), "ubuntu-24.04")
	if err != nil {
		t.Fatalf("resolveBaseImage() error: %v", err)
	}
	if got != cachedPath {
		t.Errorf("got %q, want %q", got, cachedPath)
	}
}

func TestLocalProvider_ResolveBaseImage_UnknownName_ReturnsError(t *testing.T) {
	p := &LocalProvider{dataDir: t.TempDir(), exec: &fakeCmdExecutor{}}
	_, err := p.resolveBaseImage(context.Background(), "debian-12")
	if err == nil {
		t.Fatal("expected error for unknown name, got nil")
	}
	if !strings.Contains(err.Error(), "unknown image") {
		t.Errorf("error %q should mention 'unknown image'", err.Error())
	}
}

func TestLocalProvider_GetVM_NotFound_ReturnsUnknown(t *testing.T) {
	conn, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer conn.Close()

	p := &LocalProvider{db: conn, dataDir: t.TempDir(), exec: &fakeCmdExecutor{}}

	vm, err := p.GetVM(context.Background(), "nonexistent-vm-id")
	if err != nil {
		t.Fatalf("GetVM() error: %v", err)
	}
	if vm.State != VMStateUnknown {
		t.Errorf("GetVM().State = %q, want %q", vm.State, VMStateUnknown)
	}
}
