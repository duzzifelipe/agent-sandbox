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
	// Disable journaling in tests so no journal files are left behind in TempDir
	// after background goroutines (e.g. pollUntilRunning) write to SQLite.
	conn.SetMaxOpenConns(1)
	conn.Exec("PRAGMA journal_mode=OFF")
	t.Cleanup(func() { conn.Close() })

	_ = os.MkdirAll(filepath.Join(dir, "profiles"), 0755)
	profileStore := profile.NewStore(conn, filepath.Join(dir, "profiles"))

	sessionStore := session.NewStore(conn)
	images := vm.NewImageStore(filepath.Join(dir, "images.json"))

	fakeProvider := &fakeVM{}
	mgr := session.NewManager(sessionStore, fakeProvider, dir, "test-secret", "")

	h := api.NewHandler(profileStore, mgr, images, &fakeBuilder{}, dir, "test-secret")
	return h, dir
}

// fakeBuilder satisfies api.ImageBuilder for handler tests.
type fakeBuilder struct {
	profile string
	err     error
}

func (f *fakeBuilder) BuildVirtualBox(_ context.Context, p types.ProfileSpec) (string, error) {
	f.profile = p.Name
	return "/tmp/fake.ova", f.err
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
func (f *fakeVM) RegisterIP(_ context.Context, _, _ string) error { return nil }

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

func TestBuildImage_Accepted(t *testing.T) {
	h, dir := newHandler(t)

	conn, _ := db.Open(filepath.Join(dir, "test.db"))
	conn.Exec("INSERT INTO profiles (name, spec) VALUES (?, ?)", "dev", `{"name":"dev","infrastructure":{"provider":"virtualbox","image":"ubuntu-24.04"}}`)
	conn.Close()

	// Create the profile in the profile store via the API.
	spec := types.ProfileSpec{
		Name:           "dev",
		Infrastructure: types.InfrastructureConfig{Provider: "virtualbox", Image: "ubuntu-24.04"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}
	body, _ := json.Marshal(spec)
	req := httptest.NewRequest(http.MethodPost, "/profiles", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /profiles: got %d — %s", rec.Code, rec.Body.String())
	}

	buildBody, _ := json.Marshal(types.BuildImageRequest{ProfileName: "dev"})
	req = httptest.NewRequest(http.MethodPost, "/images/build", bytes.NewReader(buildBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /images/build: got %d — %s", rec.Code, rec.Body.String())
	}
	var result map[string]string
	json.NewDecoder(rec.Body).Decode(&result)
	if result["status"] != "building" {
		t.Errorf("expected status=building, got %q", result["status"])
	}
	if result["profile"] != "dev" {
		t.Errorf("expected profile=dev, got %q", result["profile"])
	}
}

func TestListImages_Empty(t *testing.T) {
	h, _ := newHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/images", nil)
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /images: got %d — %s", rec.Code, rec.Body.String())
	}
	var entries []types.ImageEntry
	json.NewDecoder(rec.Body).Decode(&entries)
	if len(entries) != 0 {
		t.Errorf("expected empty array, got %v", entries)
	}
}

func TestGetAgentState_NoState(t *testing.T) {
	h, dir := newHandler(t)

	// Create profile and session.
	spec := types.ProfileSpec{
		Name:           "dev",
		Infrastructure: types.InfrastructureConfig{Provider: "virtualbox", Image: "ubuntu-24.04"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}
	body, _ := json.Marshal(spec)
	req := httptest.NewRequest(http.MethodPost, "/profiles", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)

	vault.StoreVaultData(dir, "dev", "test-secret", types.VaultData{VMAccessPublicKey: "ssh-rsa AAAA..."})

	body, _ = json.Marshal(types.CreateSessionRequest{ProfileName: "dev"})
	req = httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /sessions: got %d — %s", rec.Code, rec.Body.String())
	}
	var sessResp types.SessionResponse
	json.NewDecoder(rec.Body).Decode(&sessResp)

	req = httptest.NewRequest(http.MethodGet, "/sessions/"+sessResp.ID+"/agent-state", nil)
	rec = httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("GET /sessions/{id}/agent-state: got %d, want 204 — %s", rec.Code, rec.Body.String())
	}
}

func TestGetAgentState_ReturnsVaultData(t *testing.T) {
	h, dir := newHandler(t)

	// Create profile and session.
	spec := types.ProfileSpec{
		Name:           "dev",
		Infrastructure: types.InfrastructureConfig{Provider: "virtualbox", Image: "ubuntu-24.04"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}
	body, _ := json.Marshal(spec)
	req := httptest.NewRequest(http.MethodPost, "/profiles", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)

	vault.StoreVaultData(dir, "dev", "test-secret", types.VaultData{VMAccessPublicKey: "ssh-rsa AAAA..."})

	body, _ = json.Marshal(types.CreateSessionRequest{ProfileName: "dev"})
	req = httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /sessions: got %d — %s", rec.Code, rec.Body.String())
	}
	var sessResp types.SessionResponse
	json.NewDecoder(rec.Body).Decode(&sessResp)

	// Write a fake agent state tarball via vault-sync.
	agentStateBytes := []byte("fake-tar-content")
	body = agentStateBytes
	req = httptest.NewRequest(http.MethodPost, "/sessions/"+sessResp.ID+"/vault-sync", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /sessions/{id}/vault-sync: got %d — %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/sessions/"+sessResp.ID+"/agent-state", nil)
	rec = httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /sessions/{id}/agent-state: got %d, want 200 — %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("expected Content-Type application/octet-stream, got %q", ct)
	}
	if !bytes.Equal(rec.Body.Bytes(), agentStateBytes) {
		t.Errorf("expected %q, got %q", agentStateBytes, rec.Body.Bytes())
	}
}
