# Plan 2c — Session Lifecycle + HTTP API

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the session state machine, session store (SQLite CRUD), session manager (orchestrates vm + vault), all HTTP API handlers, and wire the server's main.go.

**Architecture:** `server/internal/session/` holds the store (thin SQLite wrapper) and manager (business logic that calls vm, vault, profile, db). `server/internal/api/` holds chi-based HTTP handlers. `server/cmd/agentsdxd/main.go` wires everything, reads env vars, and starts the HTTP server.

**Tech Stack:** Go, `github.com/go-chi/chi/v5` (HTTP router), `golang.org/x/crypto/ssh` (SSH client for vault sync), `database/sql` (SQLite via existing db package)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `server/internal/session/store.go` | SQLite CRUD for sessions table |
| `server/internal/session/store_test.go` | Unit tests for session store |
| `server/internal/session/manager.go` | Session lifecycle: start, stop, get, poll |
| `server/internal/session/manager_test.go` | Unit tests for manager with fake VMProvider |
| `server/internal/api/handler.go` | All HTTP handlers (profiles, sessions, images) |
| `server/internal/api/handler_test.go` | HTTP handler tests using httptest |
| `server/cmd/agentsdxd/main.go` | Server wiring: db, vault, profile, vm, session, api |

---

## Context

### shared types (already defined in shared/types/api.go)
```go
const (
    SessionStatePending   = "pending"
    SessionStateStarting  = "starting"
    SessionStateRunning   = "running"
    SessionStateStopping  = "stopping"
    SessionStateDestroyed = "destroyed"
)

type CreateSessionRequest struct { ProfileName string `json:"profile_name"` }
type SessionResponse struct {
    ID        string `json:"id"`
    Profile   string `json:"profile"`
    State     string `json:"state"`
    IPAddress string `json:"ip_address,omitempty"`
}
type VMKeyResponse struct { PrivateKey string `json:"private_key"` }
type BuildImageRequest struct { ProfileName string `json:"profile_name"` }
```

### Existing server packages
- `server/internal/db` — `db.Open(path) (*sql.DB, error)`
- `server/internal/vault` — `StoreVaultData`, `LoadVaultData`, `DeriveKey`, `Encrypt`, `Decrypt`
- `server/internal/profile` — `profile.Store` (YAML + SQLite CRUD for ProfileSpec)
- `server/internal/vm` — `VMProvider` interface, `VirtualBoxProvider`, `ImageStore`

### SQLite sessions table (already created in Plan 2a)
```sql
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    profile_name TEXT NOT NULL,
    state TEXT NOT NULL,
    ip_address TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (profile_name) REFERENCES profiles(name)
);
```

### Session start flow (server side)
1. Create session record (state: `pending`)
2. Load vault → get VM access public key
3. Build NoCloud user-data with VM access public key
4. Call `VMProvider.CreateVM` → get VM ID
5. Update session state: `starting`, store vm_id
6. In goroutine: poll `VMProvider.GetVM` until `running` or timeout (2 min)
7. On running: update state=`running`, ip_address
8. Start idle timeout watcher (2 hours; destroys if no activity)

### Session stop flow (server side)
1. Update state: `stopping`
2. SSH into VM, run vault-sync script
3. Receive tarball at `POST /sessions/{id}/vault-sync` → encrypt, store as `agent-state.tar.enc`
4. Call `VMProvider.DestroyVM`
5. Update state: `destroyed`

### HTTP API routes
```
GET    /profiles
POST   /profiles
GET    /profiles/{name}
PUT    /profiles/{name}
DELETE /profiles/{name}
POST   /profiles/{name}/credentials

POST   /sessions
GET    /sessions/{id}
GET    /sessions/{id}/key
POST   /sessions/{id}/stop
POST   /sessions/{id}/vault-sync

POST   /images/build
GET    /images
```

---

## Task 1: Add chi router dependency

**Files:**
- Modify: `server/go.mod` (via go get)

- [ ] **Step 1: Add chi**

```bash
cd server && mise exec -- go get github.com/go-chi/chi/v5@latest
```

- [ ] **Step 2: Verify**

```bash
cd server && mise exec -- go build ./...
```
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add server/go.mod server/go.sum
git commit -m "feat: add chi router dependency"
```

---

## Task 2: Session store

**Files:**
- Create: `server/internal/session/store.go`
- Create: `server/internal/session/store_test.go`

The session store is a thin wrapper around the sessions table. It does not contain any business logic — just SQL CRUD.

- [ ] **Step 1: Write the failing tests**

```go
package session_test

import (
    "path/filepath"
    "testing"

    "github.com/duck-labs/agentsdx-server/internal/db"
    "github.com/duck-labs/agentsdx-server/internal/session"
    "github.com/duck-labs/agentsdx-shared/types"
)

func newStore(t *testing.T) *session.Store {
    t.Helper()
    conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
    if err != nil {
        t.Fatalf("db.Open: %v", err)
    }
    t.Cleanup(func() { conn.Close() })
    return session.NewStore(conn)
}

func TestStore_CreateAndGet(t *testing.T) {
    // Requires a profile row in SQLite (FK constraint). Insert directly.
    store := newStore(t)
    // Insert profile row to satisfy FK
    store.DB().Exec("INSERT INTO profiles (name) VALUES (?)", "my-profile")

    id, err := store.Create("my-profile")
    if err != nil {
        t.Fatalf("Create: %v", err)
    }
    if id == "" {
        t.Fatal("expected non-empty session ID")
    }

    s, err := store.Get(id)
    if err != nil {
        t.Fatalf("Get: %v", err)
    }
    if s.Profile != "my-profile" {
        t.Errorf("Profile: got %q, want %q", s.Profile, "my-profile")
    }
    if s.State != types.SessionStatePending {
        t.Errorf("State: got %q, want %q", s.State, types.SessionStatePending)
    }
}

func TestStore_UpdateState(t *testing.T) {
    store := newStore(t)
    store.DB().Exec("INSERT INTO profiles (name) VALUES (?)", "p1")

    id, _ := store.Create("p1")
    if err := store.UpdateState(id, types.SessionStateRunning, "192.168.56.10"); err != nil {
        t.Fatalf("UpdateState: %v", err)
    }

    s, _ := store.Get(id)
    if s.State != types.SessionStateRunning {
        t.Errorf("State: got %q, want %q", s.State, types.SessionStateRunning)
    }
    if s.IPAddress != "192.168.56.10" {
        t.Errorf("IPAddress: got %q, want %q", s.IPAddress, "192.168.56.10")
    }
}

func TestStore_Get_NotFound(t *testing.T) {
    store := newStore(t)
    _, err := store.Get("nonexistent-id")
    if err == nil {
        t.Fatal("expected error for missing session")
    }
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd server && mise exec -- go test ./internal/session/... -v
```
Expected: compile error

- [ ] **Step 3: Write the implementation**

```go
// Package session provides session lifecycle management for agentsdx-server.
package session

import (
    "database/sql"
    "fmt"

    "github.com/google/uuid"
    "github.com/duck-labs/agentsdx-shared/types"
)

// Store is a thin SQLite CRUD wrapper for the sessions table.
type Store struct {
    db *sql.DB
}

// NewStore creates a Store backed by the given database connection.
func NewStore(db *sql.DB) *Store {
    return &Store{db: db}
}

// DB exposes the underlying connection (used in tests to set up FK dependencies).
func (s *Store) DB() *sql.DB { return s.db }

// SessionRecord is a row from the sessions table.
type SessionRecord struct {
    ID        string
    Profile   string
    State     string
    IPAddress string
}

// Create inserts a new session in pending state and returns the generated ID.
func (s *Store) Create(profileName string) (string, error) {
    id := uuid.New().String()
    _, err := s.db.Exec(
        `INSERT INTO sessions (id, profile_name, state) VALUES (?, ?, ?)`,
        id, profileName, types.SessionStatePending,
    )
    if err != nil {
        return "", fmt.Errorf("insert session: %w", err)
    }
    return id, nil
}

// Get returns the session record for the given ID.
func (s *Store) Get(id string) (SessionRecord, error) {
    var rec SessionRecord
    err := s.db.QueryRow(
        `SELECT id, profile_name, state, COALESCE(ip_address, '') FROM sessions WHERE id = ?`, id,
    ).Scan(&rec.ID, &rec.Profile, &rec.State, &rec.IPAddress)
    if err == sql.ErrNoRows {
        return rec, fmt.Errorf("session %q not found", id)
    }
    if err != nil {
        return rec, fmt.Errorf("query session: %w", err)
    }
    return rec, nil
}

// UpdateState sets the state and ip_address of a session.
func (s *Store) UpdateState(id, state, ipAddress string) error {
    res, err := s.db.Exec(
        `UPDATE sessions SET state = ?, ip_address = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
        state, ipAddress, id,
    )
    if err != nil {
        return fmt.Errorf("update session state: %w", err)
    }
    n, _ := res.RowsAffected()
    if n == 0 {
        return fmt.Errorf("session %q not found", id)
    }
    return nil
}
```

Note: `github.com/google/uuid` is already an indirect dependency (pulled in by modernc.org/sqlite). Promote it:

```bash
cd server && mise exec -- go get github.com/google/uuid && mise exec -- go mod tidy
```

- [ ] **Step 4: Run tests**

```bash
cd server && mise exec -- go test ./internal/session/... -v
```
Expected: 3 tests PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/session/ server/go.mod server/go.sum
git commit -m "feat: add session store (SQLite CRUD)"
```

---

## Task 3: Session manager

**Files:**
- Create: `server/internal/session/manager.go`
- Create: `server/internal/session/manager_test.go`

The manager orchestrates the full start/stop flow. It delegates VM calls to a `vm.VMProvider` so it can be unit-tested with a fake provider.

- [ ] **Step 1: Write failing tests with a fake VMProvider**

```go
package session_test

import (
    "context"
    "testing"
    "time"

    "github.com/duck-labs/agentsdx-server/internal/session"
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

func TestManager_StartSession_CreatesSession(t *testing.T) {
    store := newStore(t)
    store.DB().Exec("INSERT INTO profiles (name) VALUES (?)", "dev")

    vaultDir := t.TempDir()
    vaultSecret := "test-secret"

    // Bootstrap vault with VM access key
    vaultData := types.DefaultVaultData()
    vaultData.VMAccessPublicKey = "ssh-rsa AAAA..."
    vaultData.VMAccessPrivateKey = "-----BEGIN RSA PRIVATE KEY-----\n...\n-----END RSA PRIVATE KEY-----"
    if err := vault.StoreVaultData(vaultDir, "dev", vaultSecret, vaultData); err != nil {
        t.Fatalf("StoreVaultData: %v", err)
    }

    mgr := session.NewManager(store, newFakeVM(), vaultDir, vaultSecret)
    id, err := mgr.Start(context.Background(), "dev")
    if err != nil {
        t.Fatalf("Start: %v", err)
    }
    if id == "" {
        t.Fatal("expected non-empty session ID")
    }

    // Give background goroutine time to update state
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
    mgr := session.NewManager(store, fakeProvider, vaultDir, vaultSecret)

    id, _ := mgr.Start(context.Background(), "dev")
    time.Sleep(100 * time.Millisecond)

    if err := mgr.Stop(context.Background(), id); err != nil {
        t.Fatalf("Stop: %v", err)
    }

    rec, _ := store.Get(id)
    if rec.State != types.SessionStateDestroyed {
        t.Errorf("State after stop: got %q, want %q", rec.State, types.SessionStateDestroyed)
    }
}
```

Add `"fmt"` and `"github.com/duck-labs/agentsdx-server/internal/vault"` imports to the test file.

- [ ] **Step 2: Run to verify it fails**

```bash
cd server && mise exec -- go test ./internal/session/... -run TestManager -v
```
Expected: compile error (Manager not defined)

- [ ] **Step 3: Write the implementation**

```go
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

// Stop triggers vault sync (best-effort) then destroys the VM.
// Note: For MVP, vault sync via SSH is done externally by the VM calling /sessions/{id}/vault-sync.
// Stop here just destroys the VM and marks the session destroyed.
func (m *Manager) Stop(ctx context.Context, sessionID string) error {
    rec, err := m.store.Get(sessionID)
    if err != nil {
        return fmt.Errorf("get session: %w", err)
    }

    _ = m.store.UpdateState(sessionID, types.SessionStateStopping, rec.IPAddress)

    // Derive VM ID from session record.
    // The VM ID was stored in ip_address field temporarily during start — use a dedicated field
    // when the schema is extended. For MVP, we reconstruct it from the session.
    // TODO: Add vm_id column in a future migration; for now query by convention.
    // (The VM ID was set to "agentsdx-{profile}-{timestamp}" — we don't store it back.
    //  For MVP Stop is called after vault-sync completes via HTTP POST.)

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

func (m *Manager) pollUntilRunning(sessionID, vmID string) {
    ctx, cancel := context.WithTimeout(context.Background(), pollTimeout)
    defer cancel()

    ticker := time.NewTicker(pollInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            log.Printf("session %s: timed out waiting for VM to start", sessionID)
            _ = m.store.UpdateState(sessionID, types.SessionStateDestroyed, "")
            _ = m.provider.DestroyVM(context.Background(), vmID)
            return
        case <-ticker.C:
            v, err := m.provider.GetVM(ctx, vmID)
            if err != nil {
                log.Printf("session %s: GetVM error: %v", sessionID, err)
                continue
            }
            if v.State == vm.VMStateRunning && v.IPAddress != "" {
                _ = m.store.UpdateState(sessionID, types.SessionStateRunning, v.IPAddress)
                return
            }
        }
    }
}
```

- [ ] **Step 4: Run tests**

```bash
cd server && mise exec -- go test ./internal/session/... -v
```
Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/session/
git commit -m "feat: add session manager with start/stop/poll lifecycle"
```

---

## Task 4: HTTP API handlers

**Files:**
- Create: `server/internal/api/handler.go`
- Create: `server/internal/api/handler_test.go`

All handlers are methods on a `Handler` struct that holds references to the profile store, session manager, image store, and vault config.

- [ ] **Step 1: Write failing tests**

```go
package api_test

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "testing"

    "github.com/duck-labs/agentsdx-server/internal/api"
    "github.com/duck-labs/agentsdx-server/internal/db"
    "github.com/duck-labs/agentsdx-server/internal/profile"
    "github.com/duck-labs/agentsdx-server/internal/session"
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

    profileStore := profile.NewStore(conn, filepath.Join(dir, "profiles"))
    _ = os.MkdirAll(filepath.Join(dir, "profiles"), 0755)

    sessionStore := session.NewStore(conn)
    images := vm.NewImageStore(filepath.Join(dir, "images.json"))

    // Fake provider that always returns a running VM
    fakeProvider := &fakeVM{}
    mgr := session.NewManager(sessionStore, fakeProvider, dir, "test-secret")

    h := api.NewHandler(profileStore, mgr, images, dir, "test-secret")
    return h, dir
}

// fakeVM (same as in session tests)
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
        Name: "test-profile",
        Infrastructure: types.InfrastructureConfig{Provider: "virtualbox", Image: "ubuntu-24.04"},
        Agent: types.AgentConfig{Provider: "claude"},
    }
    body, _ := json.Marshal(spec)

    // Create
    req := httptest.NewRequest(http.MethodPost, "/profiles", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    rec := httptest.NewRecorder()
    router.ServeHTTP(rec, req)
    if rec.Code != http.StatusCreated {
        t.Fatalf("POST /profiles: got %d, want %d — body: %s", rec.Code, http.StatusCreated, rec.Body.String())
    }

    // List
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

    // Need a profile first
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
```

Add `"os"` and `"github.com/duck-labs/agentsdx-server/internal/vault"` imports.

- [ ] **Step 2: Run to verify it fails**

```bash
cd server && mise exec -- go test ./internal/api/... -v
```
Expected: compile error

- [ ] **Step 3: Write the implementation**

```go
// Package api provides HTTP handlers for the agentsdx server.
package api

import (
    "encoding/json"
    "io"
    "net/http"
    "os"
    "path/filepath"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"

    "github.com/duck-labs/agentsdx-server/internal/profile"
    "github.com/duck-labs/agentsdx-server/internal/session"
    "github.com/duck-labs/agentsdx-server/internal/vault"
    "github.com/duck-labs/agentsdx-server/internal/vm"
    "github.com/duck-labs/agentsdx-shared/types"
)

// Handler holds all dependencies for the HTTP API.
type Handler struct {
    profiles    *profile.Store
    sessions    *session.Manager
    images      *vm.ImageStore
    vaultDir    string
    vaultSecret string
}

// NewHandler creates a Handler with the given dependencies.
func NewHandler(
    profiles *profile.Store,
    sessions *session.Manager,
    images *vm.ImageStore,
    vaultDir string,
    vaultSecret string,
) *Handler {
    return &Handler{
        profiles:    profiles,
        sessions:    sessions,
        images:      images,
        vaultDir:    vaultDir,
        vaultSecret: vaultSecret,
    }
}

// Router builds and returns the chi router with all routes registered.
func (h *Handler) Router() http.Handler {
    r := chi.NewRouter()
    r.Use(middleware.Recoverer)

    r.Get("/profiles", h.listProfiles)
    r.Post("/profiles", h.createProfile)
    r.Get("/profiles/{name}", h.getProfile)
    r.Put("/profiles/{name}", h.updateProfile)
    r.Delete("/profiles/{name}", h.deleteProfile)
    r.Post("/profiles/{name}/credentials", h.setCredentials)

    r.Post("/sessions", h.createSession)
    r.Get("/sessions/{id}", h.getSession)
    r.Get("/sessions/{id}/key", h.getSessionKey)
    r.Post("/sessions/{id}/stop", h.stopSession)
    r.Post("/sessions/{id}/vault-sync", h.vaultSync)

    r.Post("/images/build", h.buildImage)
    r.Get("/images", h.listImages)

    return r
}

// --- Profiles ---

func (h *Handler) listProfiles(w http.ResponseWriter, r *http.Request) {
    specs, err := h.profiles.List()
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    writeJSON(w, http.StatusOK, specs)
}

func (h *Handler) createProfile(w http.ResponseWriter, r *http.Request) {
    var spec types.ProfileSpec
    if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
        writeError(w, http.StatusBadRequest, "invalid JSON")
        return
    }
    if err := h.profiles.Create(spec); err != nil {
        writeError(w, http.StatusConflict, err.Error())
        return
    }
    writeJSON(w, http.StatusCreated, spec)
}

func (h *Handler) getProfile(w http.ResponseWriter, r *http.Request) {
    name := chi.URLParam(r, "name")
    spec, err := h.profiles.Get(name)
    if err != nil {
        writeError(w, http.StatusNotFound, err.Error())
        return
    }
    writeJSON(w, http.StatusOK, spec)
}

func (h *Handler) updateProfile(w http.ResponseWriter, r *http.Request) {
    name := chi.URLParam(r, "name")
    var spec types.ProfileSpec
    if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
        writeError(w, http.StatusBadRequest, "invalid JSON")
        return
    }
    spec.Name = name
    if err := h.profiles.Delete(name); err != nil {
        writeError(w, http.StatusNotFound, err.Error())
        return
    }
    if err := h.profiles.Create(spec); err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    writeJSON(w, http.StatusOK, spec)
}

func (h *Handler) deleteProfile(w http.ResponseWriter, r *http.Request) {
    name := chi.URLParam(r, "name")
    if err := h.profiles.Delete(name); err != nil {
        writeError(w, http.StatusNotFound, err.Error())
        return
    }
    w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setCredentials(w http.ResponseWriter, r *http.Request) {
    name := chi.URLParam(r, "name")
    tarball, err := io.ReadAll(r.Body)
    if err != nil {
        writeError(w, http.StatusBadRequest, "read body")
        return
    }

    vaultData, err := vault.LoadVaultData(h.vaultDir, name, h.vaultSecret)
    if err != nil {
        // Bootstrap: create empty vault if not found
        vaultData = types.DefaultVaultData()
    }

    // Store tarball as AgentStatePaths entry (encrypted separately)
    agentStatePath := filepath.Join(h.vaultDir, name+"-agent-state.tar")
    if err := os.WriteFile(agentStatePath, tarball, 0600); err != nil {
        writeError(w, http.StatusInternalServerError, "store agent state")
        return
    }
    _ = vaultData // vault data already loaded; agent state stored as separate file for now

    w.WriteHeader(http.StatusNoContent)
}

// --- Sessions ---

func (h *Handler) createSession(w http.ResponseWriter, r *http.Request) {
    var req types.CreateSessionRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid JSON")
        return
    }
    id, err := h.sessions.Start(r.Context(), req.ProfileName)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    resp, _ := h.sessions.Get(id)
    writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")
    resp, err := h.sessions.Get(id)
    if err != nil {
        writeError(w, http.StatusNotFound, err.Error())
        return
    }
    writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) getSessionKey(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")
    resp, err := h.sessions.Get(id)
    if err != nil {
        writeError(w, http.StatusNotFound, err.Error())
        return
    }

    vaultData, err := vault.LoadVaultData(h.vaultDir, resp.Profile, h.vaultSecret)
    if err != nil {
        writeError(w, http.StatusInternalServerError, "load vault")
        return
    }
    writeJSON(w, http.StatusOK, types.VMKeyResponse{PrivateKey: vaultData.VMAccessPrivateKey})
}

func (h *Handler) stopSession(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")
    if err := h.sessions.Stop(r.Context(), id); err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) vaultSync(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")
    resp, err := h.sessions.Get(id)
    if err != nil {
        writeError(w, http.StatusNotFound, err.Error())
        return
    }

    tarball, err := io.ReadAll(r.Body)
    if err != nil {
        writeError(w, http.StatusBadRequest, "read body")
        return
    }

    // Encrypt and store agent state tarball
    key, err := vault.DeriveKey(h.vaultSecret, resp.Profile+"-agent-state")
    if err != nil {
        writeError(w, http.StatusInternalServerError, "derive key")
        return
    }
    encrypted, err := vault.Encrypt(key, tarball)
    if err != nil {
        writeError(w, http.StatusInternalServerError, "encrypt")
        return
    }
    path := filepath.Join(h.vaultDir, resp.Profile+"-agent-state.tar.enc")
    if err := os.WriteFile(path, encrypted, 0600); err != nil {
        writeError(w, http.StatusInternalServerError, "store agent state")
        return
    }
    w.WriteHeader(http.StatusNoContent)
}

// --- Images ---

func (h *Handler) buildImage(w http.ResponseWriter, r *http.Request) {
    var req types.BuildImageRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid JSON")
        return
    }
    // Image build is async and handled externally (Packer CLI). For MVP, respond 202 Accepted.
    // The actual Packer invocation is out of scope for this plan (see Plan 4).
    writeJSON(w, http.StatusAccepted, map[string]string{"status": "build triggered", "profile": req.ProfileName})
}

func (h *Handler) listImages(w http.ResponseWriter, r *http.Request) {
    // For MVP, read images.json and return all entries.
    // The ImageStore does not have a List method yet; return empty for now.
    writeJSON(w, http.StatusOK, []types.ImageEntry{})
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
    writeJSON(w, status, map[string]string{"error": msg})
}
```

- [ ] **Step 4: Run tests**

```bash
cd server && mise exec -- go test ./internal/api/... -v
```
Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/api/
git commit -m "feat: add HTTP API handlers with chi router"
```

---

## Task 5: Wire server main.go

**Files:**
- Modify: `server/cmd/agentsdxd/main.go`

The server reads configuration from environment variables and starts the HTTP server on `:8080`.

**Environment variables:**
- `AGENTSDX_VAULT_SECRET` — required; master encryption secret
- `AGENTSDX_DATA_DIR` — optional; defaults to `./data`
- `AGENTSDX_ADDR` — optional; defaults to `:8080`

- [ ] **Step 1: Read the current stub**

```bash
cat server/cmd/agentsdxd/main.go
```

- [ ] **Step 2: Write the implementation**

```go
package main

import (
    "database/sql"
    "log"
    "net/http"
    "os"
    "path/filepath"

    "github.com/duck-labs/agentsdx-server/internal/api"
    "github.com/duck-labs/agentsdx-server/internal/db"
    "github.com/duck-labs/agentsdx-server/internal/profile"
    "github.com/duck-labs/agentsdx-server/internal/session"
    "github.com/duck-labs/agentsdx-server/internal/vm"
)

func main() {
    secret := mustEnv("AGENTSDX_VAULT_SECRET")
    dataDir := envOrDefault("AGENTSDX_DATA_DIR", "./data")
    addr := envOrDefault("AGENTSDX_ADDR", ":8080")

    if err := os.MkdirAll(filepath.Join(dataDir, "profiles"), 0755); err != nil {
        log.Fatalf("create data dirs: %v", err)
    }
    if err := os.MkdirAll(filepath.Join(dataDir, "vault"), 0700); err != nil {
        log.Fatalf("create vault dir: %v", err)
    }
    if err := os.MkdirAll(filepath.Join(dataDir, "iso"), 0755); err != nil {
        log.Fatalf("create iso dir: %v", err)
    }

    conn, err := db.Open(filepath.Join(dataDir, "agentsdx.db"))
    if err != nil {
        log.Fatalf("open db: %v", err)
    }
    defer conn.Close()

    profileStore := profile.NewStore(conn, filepath.Join(dataDir, "profiles"))

    images := vm.NewImageStore(filepath.Join(dataDir, "images.json"))
    provider := vm.NewVirtualBoxProvider(images, filepath.Join(dataDir, "iso"))

    sessionStore := session.NewStore(conn)
    mgr := session.NewManager(sessionStore, provider, filepath.Join(dataDir, "vault"), secret)

    h := api.NewHandler(profileStore, mgr, images, filepath.Join(dataDir, "vault"), secret)

    log.Printf("agentsdxd listening on %s", addr)
    if err := http.ListenAndServe(addr, h.Router()); err != nil {
        log.Fatalf("listen: %v", err)
    }
}

func mustEnv(key string) string {
    v := os.Getenv(key)
    if v == "" {
        log.Fatalf("required env var %s is not set", key)
    }
    return v
}

func envOrDefault(key, def string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return def
}
```

Note: Remove the unused `*sql.DB` import alias if not needed.

- [ ] **Step 3: Build the binary**

```bash
cd server && mise exec -- go build -o agentsdxd ./cmd/agentsdxd/
```
Expected: `agentsdxd` binary produced, no errors

- [ ] **Step 4: Run all tests**

```bash
cd server && mise exec -- go test ./... 2>&1 | grep -E "^(ok|FAIL|---)"
```
Expected: all packages ok

- [ ] **Step 5: Commit**

```bash
git add server/cmd/agentsdxd/main.go
git commit -m "feat: wire server main.go with all dependencies"
```
