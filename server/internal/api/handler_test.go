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

	fakeProvider := &fakeVM{}
	// sessionImages is pre-seeded so Start("dev") can resolve a snapshot ID.
	sessionImages := vm.NewImageStore(filepath.Join(dir, "session-images.json"))
	_ = sessionImages.SetImageID(vm.ProviderHetzner, "dev", "snap-1")

	mgr := session.NewManager(sessionStore, map[string]vm.VMProvider{"hetzner": fakeProvider}, sessionImages, dir, "test-secret", "")

	h := api.NewHandler(profileStore, mgr, &fakeBuilder{}, dir, "test-secret")
	return h, dir
}

// fakeBuilder satisfies api.ImageBuilder for handler tests.
type fakeBuilder struct {
	profile string
	err     error
}

func (f *fakeBuilder) Build(_ context.Context, p types.ProfileSpec) (string, error) {
	f.profile = p.Name
	return "snap-42", f.err
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
		Infrastructure: types.InfrastructureConfig{Provider: "hetzner", Image: "ubuntu-24.04"},
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

func TestHandler_CreateSession(t *testing.T) {
	h, dir := newHandler(t)

	// Create the profile via the API so the YAML file exists.
	devSpec := types.ProfileSpec{
		Name:           "dev",
		Infrastructure: types.InfrastructureConfig{Provider: "hetzner", Image: "ubuntu-24.04"},
	}
	profileBody, _ := json.Marshal(devSpec)
	profileReq := httptest.NewRequest(http.MethodPost, "/profiles", bytes.NewReader(profileBody))
	profileReq.Header.Set("Content-Type", "application/json")
	profileRec := httptest.NewRecorder()
	h.Router().ServeHTTP(profileRec, profileReq)
	if profileRec.Code != http.StatusCreated {
		t.Fatalf("POST /profiles: got %d — %s", profileRec.Code, profileRec.Body.String())
	}

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
	h, _ := newHandler(t)

	// Create the profile in the profile store via the API.
	spec := types.ProfileSpec{
		Name:           "dev",
		Infrastructure: types.InfrastructureConfig{Provider: "hetzner", Image: "ubuntu-24.04"},
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

func TestHandler_SetSecret_CreatesAndUpdates(t *testing.T) {
	h, dir := newHandler(t)
	router := h.Router()

	// Create a profile first.
	spec := types.ProfileSpec{
		Name:           "dev",
		Infrastructure: types.InfrastructureConfig{Provider: "hetzner", Image: "ubuntu-24.04"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}
	body, _ := json.Marshal(spec)
	req := httptest.NewRequest(http.MethodPost, "/profiles", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /profiles: %d — %s", rec.Code, rec.Body)
	}

	// Set a secret.
	body, _ = json.Marshal(map[string]string{"value": "ghp_abc123"})
	req = httptest.NewRequest(http.MethodPut, "/profiles/dev/secrets/GITHUB_PAT", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("PUT /profiles/dev/secrets/GITHUB_PAT: got %d — %s", rec.Code, rec.Body)
	}

	// List secrets — should return key name only.
	req = httptest.NewRequest(http.MethodGet, "/profiles/dev/secrets", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /profiles/dev/secrets: got %d — %s", rec.Code, rec.Body)
	}
	var keys []string
	json.NewDecoder(rec.Body).Decode(&keys)
	if len(keys) != 1 || keys[0] != "GITHUB_PAT" {
		t.Errorf("expected [GITHUB_PAT], got %v", keys)
	}

	// Verify the secret is encrypted in the vault.
	vd, err := vault.LoadVaultData(dir, "dev", "test-secret")
	if err != nil {
		t.Fatalf("load vault: %v", err)
	}
	if vd.Secrets["GITHUB_PAT"] != "ghp_abc123" {
		t.Errorf("vault GITHUB_PAT: got %q, want %q", vd.Secrets["GITHUB_PAT"], "ghp_abc123")
	}
}

func TestHandler_DeleteSecret(t *testing.T) {
	h, dir := newHandler(t)
	router := h.Router()

	spec := types.ProfileSpec{
		Name:           "dev",
		Infrastructure: types.InfrastructureConfig{Provider: "hetzner", Image: "ubuntu-24.04"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}
	body, _ := json.Marshal(spec)
	req := httptest.NewRequest(http.MethodPost, "/profiles", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	body, _ = json.Marshal(map[string]string{"value": "sk-abc"})
	req = httptest.NewRequest(http.MethodPut, "/profiles/dev/secrets/OPENAI_KEY", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Delete it.
	req = httptest.NewRequest(http.MethodDelete, "/profiles/dev/secrets/OPENAI_KEY", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE: got %d — %s", rec.Code, rec.Body)
	}

	// Verify removed from vault.
	vd, err := vault.LoadVaultData(dir, "dev", "test-secret")
	if err != nil {
		t.Fatalf("load vault: %v", err)
	}
	if _, exists := vd.Secrets["OPENAI_KEY"]; exists {
		t.Error("expected OPENAI_KEY to be removed from vault")
	}
}

func TestHandler_ListSecrets_EmptyWhenNoVault(t *testing.T) {
	h, _ := newHandler(t)
	router := h.Router()

	spec := types.ProfileSpec{
		Name:           "new-profile",
		Infrastructure: types.InfrastructureConfig{Provider: "hetzner", Image: "ubuntu-24.04"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}
	body, _ := json.Marshal(spec)
	req := httptest.NewRequest(http.MethodPost, "/profiles", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodGet, "/profiles/new-profile/secrets", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /profiles/new-profile/secrets: got %d", rec.Code)
	}
	var keys []string
	json.NewDecoder(rec.Body).Decode(&keys)
	if len(keys) != 0 {
		t.Errorf("expected empty list, got %v", keys)
	}
}

func TestAddProject_Success(t *testing.T) {
	h, _ := newHandler(t)

	spec := types.ProfileSpec{
		Name:           "dev",
		Infrastructure: types.InfrastructureConfig{Provider: "hetzner", Image: "ubuntu-24.04"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}
	body, _ := json.Marshal(spec)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/profiles", bytes.NewReader(body))
	h.Router().ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("create profile got %d", w.Code)
	}

	proj := types.ProjectConfig{
		Repo: "https://github.com/org/api.git",
		Path: "~/api",
	}
	body, _ = json.Marshal(proj)
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/profiles/dev/projects", bytes.NewReader(body))
	h.Router().ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("add project got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/profiles", nil)
	h.Router().ServeHTTP(w, r)
	var profiles []types.ProfileSpec
	if err := json.NewDecoder(w.Body).Decode(&profiles); err != nil {
		t.Fatalf("decode profiles: %v", err)
	}
	if len(profiles) != 1 || len(profiles[0].Projects) != 1 {
		t.Fatalf("expected 1 profile with 1 project, got %+v", profiles)
	}
	if profiles[0].Projects[0].Repo != proj.Repo {
		t.Errorf("unexpected repo: %s", profiles[0].Projects[0].Repo)
	}
}

func TestAddProject_ProfileNotFound(t *testing.T) {
	h, _ := newHandler(t)
	proj := types.ProjectConfig{Repo: "https://github.com/org/api.git", Path: "~/api"}
	body, _ := json.Marshal(proj)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/profiles/no-such/projects", bytes.NewReader(body))
	h.Router().ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAddProject_InvalidJSON(t *testing.T) {
	h, _ := newHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/profiles/dev/projects", bytes.NewReader([]byte("not-json")))
	h.Router().ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAddProject_MissingRepo(t *testing.T) {
	h, _ := newHandler(t)

	spec := types.ProfileSpec{
		Name:           "dev",
		Infrastructure: types.InfrastructureConfig{Provider: "hetzner", Image: "ubuntu-24.04"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}
	body, _ := json.Marshal(spec)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/profiles", bytes.NewReader(body))
	h.Router().ServeHTTP(w, r)

	proj := types.ProjectConfig{Repo: "", Path: "~/api"}
	body, _ = json.Marshal(proj)
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/profiles/dev/projects", bytes.NewReader(body))
	h.Router().ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
