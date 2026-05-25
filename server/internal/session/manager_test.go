package session_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/duck-labs/agentsdx-server/internal/session"
	"github.com/duck-labs/agentsdx-server/internal/vault"
	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/duck-labs/agentsdx-shared/types"
)

// fakeVM is an in-memory VMProvider for testing.
type fakeVM struct {
	createErr  error
	destroyErr error
	vms        map[string]*vm.VM
}

func newFakeVM() *fakeVM {
	return &fakeVM{vms: make(map[string]*vm.VM)}
}

func (f *fakeVM) CreateVM(_ context.Context, req vm.CreateVMRequest) (*vm.VM, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	v := &vm.VM{ID: "fake-" + req.ProfileName, State: vm.VMStateRunning, IPAddress: "192.168.56.100"}
	f.vms[v.ID] = v
	return v, nil
}

func (f *fakeVM) DestroyVM(_ context.Context, vmID string) error {
	if f.destroyErr != nil {
		return f.destroyErr
	}
	delete(f.vms, vmID)
	return nil
}

func (f *fakeVM) GetVM(_ context.Context, vmID string) (*vm.VM, error) {
	v, ok := f.vms[vmID]
	if !ok {
		return nil, fmt.Errorf("vm %q not found", vmID)
	}
	return v, nil
}

func fakeImages(t *testing.T, profileName, snapshotID string) *vm.ImageStore {
	t.Helper()
	store := vm.NewImageStore(filepath.Join(t.TempDir(), "images.json"))
	if err := store.SetImageID(vm.ProviderHetzner, profileName, snapshotID); err != nil {
		t.Fatalf("seed images: %v", err)
	}
	return store
}

func TestManager_StartSession_CreatesSession(t *testing.T) {
	store := newStore(t)
	store.DB().Exec("INSERT INTO profiles (name) VALUES (?)", "dev")

	vaultDir := t.TempDir()
	vaultSecret := "test-secret"

	vaultData := types.DefaultVaultData()
	vaultData.VMAccessPublicKey = "ssh-rsa AAAA..."
	vaultData.VMAccessPrivateKey = "-----BEGIN RSA PRIVATE KEY-----\n...\n-----END RSA PRIVATE KEY-----"
	if err := vault.StoreVaultData(vaultDir, "dev", vaultSecret, vaultData); err != nil {
		t.Fatalf("StoreVaultData: %v", err)
	}

	mgr := session.NewManager(store, map[string]vm.VMProvider{"hetzner": newFakeVM()}, fakeImages(t, "dev", "snap-1"), vaultDir, vaultSecret, "")
	spec := types.ProfileSpec{
		Name:           "dev",
		Infrastructure: types.InfrastructureConfig{Provider: "hetzner"},
	}
	id, err := mgr.Start(context.Background(), spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty session ID")
	}

	time.Sleep(100 * time.Millisecond)

	rec, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.State != types.SessionStateRunning {
		t.Errorf("State: got %q, want %q", rec.State, types.SessionStateRunning)
	}
	if rec.IPAddress != "192.168.56.100" {
		t.Errorf("IPAddress: got %q, want %q", rec.IPAddress, "192.168.56.100")
	}
}

func TestManager_StopSession_DestroysVM(t *testing.T) {
	store := newStore(t)
	store.DB().Exec("INSERT INTO profiles (name) VALUES (?)", "dev")

	vaultDir := t.TempDir()
	vaultSecret := "test-secret"
	vaultData := types.DefaultVaultData()
	vaultData.VMAccessPublicKey = "ssh-rsa AAAA..."
	vaultData.VMAccessPrivateKey = "-----BEGIN RSA PRIVATE KEY-----\n...\n-----END RSA PRIVATE KEY-----"
	vault.StoreVaultData(vaultDir, "dev", vaultSecret, vaultData)

	fakeProvider := newFakeVM()
	mgr := session.NewManager(store, map[string]vm.VMProvider{"hetzner": fakeProvider}, fakeImages(t, "dev", "snap-1"), vaultDir, vaultSecret, "")

	spec := types.ProfileSpec{
		Name:           "dev",
		Infrastructure: types.InfrastructureConfig{Provider: "hetzner"},
	}
	id, _ := mgr.Start(context.Background(), spec)
	time.Sleep(100 * time.Millisecond)

	if err := mgr.Stop(context.Background(), id); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	rec, _ := store.Get(id)
	if rec.State != types.SessionStateDestroyed {
		t.Errorf("State after stop: got %q, want %q", rec.State, types.SessionStateDestroyed)
	}
	if len(fakeProvider.vms) != 0 {
		t.Errorf("expected DestroyVM to be called: fakeProvider.vms has %d entries, want 0", len(fakeProvider.vms))
	}
}

func TestManager_ProviderNotConfigured_ReturnsError(t *testing.T) {
	store := newStore(t)
	store.DB().Exec("INSERT INTO profiles (name) VALUES (?)", "dev")

	mgr := session.NewManager(store, map[string]vm.VMProvider{}, fakeImages(t, "dev", "snap-1"), t.TempDir(), "test-secret", "")

	spec := types.ProfileSpec{
		Name:           "dev",
		Infrastructure: types.InfrastructureConfig{Provider: "hetzner"},
	}
	_, err := mgr.Start(context.Background(), spec)
	if err == nil {
		t.Fatal("expected error for unconfigured provider, got nil")
	}
}
