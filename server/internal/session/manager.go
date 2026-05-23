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
	images      *vm.ImageStore
	vaultDir    string
	vaultSecret string
	serverURL   string
}

// NewManager creates a Manager.
func NewManager(store *Store, provider vm.VMProvider, images *vm.ImageStore, vaultDir, vaultSecret, serverURL string) *Manager {
	return &Manager{
		store:       store,
		provider:    provider,
		images:      images,
		vaultDir:    vaultDir,
		vaultSecret: vaultSecret,
		serverURL:   serverURL,
	}
}

// Start creates a session, launches the VM, and returns the session ID immediately.
func (m *Manager) Start(ctx context.Context, profileName string) (string, error) {
	if !vault.VaultExists(m.vaultDir, profileName) {
		if err := m.initVault(profileName); err != nil {
			return "", fmt.Errorf("init vault: %w", err)
		}
	}

	vaultData, err := vault.LoadVaultData(m.vaultDir, profileName, m.vaultSecret)
	if err != nil {
		return "", fmt.Errorf("load vault: %w", err)
	}

	snapshotID, err := m.images.GetHetznerSnapshotID(profileName)
	if err != nil {
		return "", fmt.Errorf("get snapshot id: %w", err)
	}

	id, err := m.store.Create(profileName)
	if err != nil {
		return "", fmt.Errorf("create session record: %w", err)
	}

	createReq := vm.CreateVMRequest{
		ProfileName:   profileName,
		ImageID:       snapshotID,
		AuthorizedKey: vaultData.VMAccessPublicKey,
		UserData: vm.BuildUserData(
			vaultData.VMAccessPublicKey,
			vaultData.GitPrivateKey,
			id,
			m.serverURL,
			profileName,
		),
	}

	v, err := m.provider.CreateVM(ctx, createReq)
	if err != nil {
		_ = m.store.UpdateState(id, types.SessionStateDestroyed, "")
		return "", fmt.Errorf("create vm: %w", err)
	}

	_ = m.store.UpdateVMID(id, v.ID)
	_ = m.store.UpdateState(id, types.SessionStateStarting, "")
	go m.pollUntilRunning(id, v.ID)
	return id, nil
}

// Stop transitions the session to destroying and marks it destroyed.
func (m *Manager) Stop(ctx context.Context, sessionID string) error {
	rec, err := m.store.Get(sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}
	_ = m.store.UpdateState(sessionID, types.SessionStateStopping, rec.IPAddress)
	if rec.VMID != "" {
		if err := m.provider.DestroyVM(ctx, rec.VMID); err != nil {
			log.Printf("session %s: DestroyVM error: %v", sessionID, err)
		}
	}
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

func (m *Manager) initVault(profileName string) error {
	vmPriv, vmPub, err := vault.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("generate vm key pair: %w", err)
	}
	gitPriv, gitPub, err := vault.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("generate git key pair: %w", err)
	}
	vd := types.DefaultVaultData()
	vd.VMAccessPrivateKey = vmPriv
	vd.VMAccessPublicKey = vmPub
	vd.GitPrivateKey = gitPriv
	vd.GitPublicKey = gitPub
	return vault.StoreVaultData(m.vaultDir, profileName, m.vaultSecret, vd)
}

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
