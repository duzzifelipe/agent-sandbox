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
	providers   map[string]vm.VMProvider // key: provider name
	images      *vm.ImageStore
	vaultDir    string
	vaultSecret string
	serverURL   string
}

// NewManager creates a Manager.
func NewManager(store *Store, providers map[string]vm.VMProvider, images *vm.ImageStore, vaultDir, vaultSecret, serverURL string) *Manager {
	return &Manager{
		store:       store,
		providers:   providers,
		images:      images,
		vaultDir:    vaultDir,
		vaultSecret: vaultSecret,
		serverURL:   serverURL,
	}
}

// Start creates a session, launches the VM, and returns the session ID immediately.
func (m *Manager) Start(ctx context.Context, spec types.ProfileSpec) (string, error) {
	provider, ok := m.providers[spec.Infrastructure.Provider]
	if !ok {
		return "", fmt.Errorf("provider %q not configured — check server credentials", spec.Infrastructure.Provider)
	}

	profileName := spec.Name

	if !vault.VaultExists(m.vaultDir, profileName) {
		if err := m.initVault(profileName); err != nil {
			return "", fmt.Errorf("init vault: %w", err)
		}
	}

	vaultData, err := vault.LoadVaultData(m.vaultDir, profileName, m.vaultSecret)
	if err != nil {
		return "", fmt.Errorf("load vault: %w", err)
	}

	imageID, err := m.images.GetImageID(vm.Provider(spec.Infrastructure.Provider), profileName)
	if err != nil {
		return "", fmt.Errorf("get image id: %w", err)
	}

	id, err := m.store.Create(profileName)
	if err != nil {
		return "", fmt.Errorf("create session record: %w", err)
	}

	createReq := vm.CreateVMRequest{
		ProfileName:   profileName,
		ImageID:       imageID,
		AuthorizedKey: vaultData.VMAccessPublicKey,
		UserData: vm.BuildUserData(
			vaultData.VMAccessPublicKey,
			vaultData.GitPrivateKey,
			id,
			m.serverURL,
			profileName,
			vaultData.Secrets,
		),
	}

	v, err := provider.CreateVM(ctx, createReq)
	if err != nil {
		_ = m.store.UpdateState(id, types.SessionStateDestroyed, "", 0)
		return "", fmt.Errorf("create vm: %w", err)
	}

	_ = m.store.UpdateVMID(id, v.ID)
	_ = m.store.UpdateState(id, types.SessionStateStarting, "", 0)
	go m.pollUntilRunning(id, v.ID, provider)
	return id, nil
}

// Stop transitions the session to destroying and marks it destroyed.
func (m *Manager) Stop(ctx context.Context, sessionID string) error {
	rec, err := m.store.Get(sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}
	_ = m.store.UpdateState(sessionID, types.SessionStateStopping, rec.IPAddress, rec.SSHPort)
	if rec.VMID != "" {
		for _, provider := range m.providers {
			if err := provider.DestroyVM(ctx, rec.VMID); err == nil {
				break
			}
		}
	}
	_ = m.store.UpdateState(sessionID, types.SessionStateDestroyed, "", 0)
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
		SSHPort:   rec.SSHPort,
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

func (m *Manager) pollUntilRunning(sessionID, vmID string, provider vm.VMProvider) {
	ctx, cancel := context.WithTimeout(context.Background(), pollTimeout)
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		v, err := provider.GetVM(ctx, vmID)
		if err == nil && v.State == vm.VMStateRunning && v.IPAddress != "" {
			_ = m.store.UpdateState(sessionID, types.SessionStateRunning, v.IPAddress, v.SSHPort)
			return
		}
		if err != nil {
			log.Printf("session %s: GetVM error: %v", sessionID, err)
		}

		select {
		case <-ctx.Done():
			log.Printf("session %s: timed out waiting for VM to start", sessionID)
			_ = m.store.UpdateState(sessionID, types.SessionStateDestroyed, "", 0)
			_ = provider.DestroyVM(context.Background(), vmID)
			return
		case <-ticker.C:
		}
	}
}
