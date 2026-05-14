package session

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/duck-labs/agentsdx-server/internal/vault"
	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/duck-labs/agentsdx-shared/types"
)

const (
	pollInterval = 5 * time.Second
	pollTimeout  = 2 * time.Minute
)

// Manager orchestrates session start and stop, delegating VM calls to a VMProvider.
type Manager struct {
	store       *Store
	provider    vm.VMProvider
	vaultDir    string
	vaultSecret string
}

// NewManager creates a Manager.
func NewManager(store *Store, provider vm.VMProvider, vaultDir, vaultSecret string) *Manager {
	return &Manager{
		store:       store,
		provider:    provider,
		vaultDir:    vaultDir,
		vaultSecret: vaultSecret,
	}
}

// Start creates a session, launches the VM, and returns the session ID immediately.
// VM polling and state updates happen in a background goroutine.
func (m *Manager) Start(ctx context.Context, profileName string) (string, error) {
	vaultData, err := vault.LoadVaultData(m.vaultDir, profileName, m.vaultSecret)
	if err != nil {
		return "", fmt.Errorf("load vault: %w", err)
	}

	id, err := m.store.Create(profileName)
	if err != nil {
		return "", fmt.Errorf("create session record: %w", err)
	}

	createReq := vm.CreateVMRequest{
		ProfileName:   profileName,
		AuthorizedKey: vaultData.VMAccessPublicKey,
		UserData:      vm.NoCloudUserData(vaultData.VMAccessPublicKey),
	}

	v, err := m.provider.CreateVM(ctx, createReq)
	if err != nil {
		_ = m.store.UpdateState(id, types.SessionStateDestroyed, "")
		return "", fmt.Errorf("create vm: %w", err)
	}

	_ = m.store.UpdateState(id, types.SessionStateStarting, "")

	go m.pollUntilRunning(id, v.ID)
	return id, nil
}

// Stop transitions the session to destroying and marks it destroyed.
// For MVP, the VM ID is not stored in the sessions table, so actual VM destruction
// is handled via vault-sync + external orchestration. A vm_id column will be added later.
func (m *Manager) Stop(ctx context.Context, sessionID string) error {
	rec, err := m.store.Get(sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}

	_ = m.store.UpdateState(sessionID, types.SessionStateStopping, rec.IPAddress)
	_ = m.store.UpdateState(sessionID, types.SessionStateDestroyed, "")
	return nil
}

// Get returns the current session state as a SessionResponse.
func (m *Manager) Get(sessionID string) (types.SessionResponse, error) {
	rec, err := m.store.Get(sessionID)
	if err != nil {
		return types.SessionResponse{}, err
	}
	return types.SessionResponse{
		ID:        rec.ID,
		Profile:   rec.Profile,
		State:     rec.State,
		IPAddress: rec.IPAddress,
	}, nil
}

// pollUntilRunning polls GetVM until the VM is running, then updates session state.
// It polls immediately on first iteration to avoid waiting the full interval for fast VMs.
func (m *Manager) pollUntilRunning(sessionID, vmID string) {
	ctx, cancel := context.WithTimeout(context.Background(), pollTimeout)
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		v, err := m.provider.GetVM(ctx, vmID)
		if err == nil && v.State == vm.VMStateRunning && v.IPAddress != "" {
			_ = m.store.UpdateState(sessionID, types.SessionStateRunning, v.IPAddress)
			return
		}
		if err != nil {
			log.Printf("session %s: GetVM error: %v", sessionID, err)
		}

		select {
		case <-ctx.Done():
			log.Printf("session %s: timed out waiting for VM to start", sessionID)
			_ = m.store.UpdateState(sessionID, types.SessionStateDestroyed, "")
			_ = m.provider.DestroyVM(context.Background(), vmID)
			return
		case <-ticker.C:
		}
	}
}
