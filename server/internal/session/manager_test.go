package session_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/duck-labs/agentsdx-server/internal/session"
	"github.com/duck-labs/agentsdx-server/internal/vault"
	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/duck-labs/agentsdx-shared/types"
)

// fakeVM is an in-memory VMProvider for testing.
type fakeVM struct {
	createErr     error
	destroyErr    error
	vms           map[string]*vm.VM
	registeredIPs map[string]string
}

func newFakeVM() *fakeVM {
	return &fakeVM{
		vms:           make(map[string]*vm.VM),
		registeredIPs: make(map[string]string),
	}
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

func (f *fakeVM) RegisterIP(_ context.Context, vmID, ip string) error {
	f.registeredIPs[vmID] = ip
	return nil
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

	mgr := session.NewManager(store, newFakeVM(), vaultDir, vaultSecret, "")
	id, err := mgr.Start(context.Background(), "dev")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty session ID")
	}

	// Give background goroutine time to update state.
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
	mgr := session.NewManager(store, fakeProvider, vaultDir, vaultSecret, "")

	id, _ := mgr.Start(context.Background(), "dev")
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

func TestManager_RegisterVMIP_StoresIPInProviderAndStore(t *testing.T) {
	store := newStore(t)
	store.DB().Exec("INSERT INTO profiles (name) VALUES (?)", "dev")

	vaultDir := t.TempDir()
	vaultSecret := "test-secret"
	vaultData := types.DefaultVaultData()
	vaultData.VMAccessPublicKey = "ssh-rsa AAAA..."
	vaultData.VMAccessPrivateKey = "-----BEGIN RSA PRIVATE KEY-----\n...\n-----END RSA PRIVATE KEY-----"
	vault.StoreVaultData(vaultDir, "dev", vaultSecret, vaultData)

	fakeProvider := newFakeVM()
	mgr := session.NewManager(store, fakeProvider, vaultDir, vaultSecret, "")

	id, _ := mgr.Start(context.Background(), "dev")
	time.Sleep(50 * time.Millisecond)

	if err := mgr.RegisterVMIP(id, "192.168.64.5"); err != nil {
		t.Fatalf("RegisterVMIP: %v", err)
	}

	rec, _ := store.Get(id)
	if rec.IPAddress != "192.168.64.5" {
		t.Errorf("IPAddress: got %q, want %q", rec.IPAddress, "192.168.64.5")
	}

	vmID := "fake-dev" // matches CreateVM's "fake-" + profileName pattern
	if fakeProvider.registeredIPs[vmID] != "192.168.64.5" {
		t.Errorf("provider.RegisterIP: got %q, want %q", fakeProvider.registeredIPs[vmID], "192.168.64.5")
	}
}
