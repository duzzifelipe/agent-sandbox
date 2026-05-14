package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/api"
	"github.com/duck-labs/agentsdx-server/internal/db"
	"github.com/duck-labs/agentsdx-server/internal/profile"
	"github.com/duck-labs/agentsdx-server/internal/session"
	"github.com/duck-labs/agentsdx-server/internal/vault"
	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/duck-labs/agentsdx-shared/types"
)

func newHandler(t *testing.T) (*api.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	_ = os.MkdirAll(filepath.Join(dir, "profiles"), 0755)
	profileStore := profile.NewStore(conn, filepath.Join(dir, "profiles"))

	sessionStore := session.NewStore(conn)
	images := vm.NewImageStore(filepath.Join(dir, "images.json"))

	fakeProvider := &fakeVM{}
	mgr := session.NewManager(sessionStore, fakeProvider, dir, "test-secret")

	h := api.NewHandler(profileStore, mgr, images, dir, "test-secret")
	return h, dir
}

// fakeVM satisfies vm.VMProvider for handler tests.
type fakeVM struct{}

func (f *fakeVM) CreateVM(_ context.Context, req vm.CreateVMRequest) (*vm.VM, error) {
	return &vm.VM{ID: "fake-" + req.ProfileName, State: vm.VMStateRunning, IPAddress: "192.168.56.1"}, nil
}
func (f *fakeVM) DestroyVM(_ context.Context, _ string) error { return nil }
func (f *fakeVM) GetVM(_ context.Context, vmID string) (*vm.VM, error) {
	return &vm.VM{ID: vmID, State: vm.VMStateRunning, IPAddress: "192.168.56.1"}, nil
}

func TestHandler_CreateAndListProfiles(t *testing.T) {
	h, _ := newHandler(t)
	router := h.Router()

	spec := types.ProfileSpec{
		Name:           "test-profile",
		Infrastructure: types.InfrastructureConfig{Provider: "virtualbox", Image: "ubuntu-24.04"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}
	body, _ := json.Marshal(spec)

	req := httptest.NewRequest(http.MethodPost, "/profiles", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /profiles: got %d, want %d — body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/profiles", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /profiles: got %d", rec.Code)
	}
	var profiles []types.ProfileSpec
	json.NewDecoder(rec.Body).Decode(&profiles)
	if len(profiles) != 1 || profiles[0].Name != "test-profile" {
		t.Errorf("expected 1 profile named test-profile, got %v", profiles)
	}
}

func TestHandler_GetProfile_NotFound(t *testing.T) {
	h, _ := newHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/profiles/nonexistent", nil)
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestHandler_CreateSession(t *testing.T) {
	h, dir := newHandler(t)

	conn, _ := db.Open(filepath.Join(dir, "test.db"))
	conn.Exec("INSERT INTO profiles (name) VALUES (?)", "dev")
	conn.Close()

	vault.StoreVaultData(dir, "dev", "test-secret", types.VaultData{VMAccessPublicKey: "ssh-rsa AAAA..."})

	body, _ := json.Marshal(types.CreateSessionRequest{ProfileName: "dev"})
	req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /sessions: got %d — %s", rec.Code, rec.Body.String())
	}
	var resp types.SessionResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.ID == "" {
		t.Error("expected non-empty session ID in response")
	}
}
