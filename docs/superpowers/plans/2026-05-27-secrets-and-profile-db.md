# Secrets Management & Profile DB Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate profile storage from YAML+SQLite hybrid to full SQLite, and add per-profile generic secrets (key/value pairs) stored encrypted in the vault and injected as env vars into VM sessions at boot.

**Architecture:** Profile specs are stored as JSON in a new `spec` column on the existing `profiles` SQLite table; on startup, any YAML files on disk that are missing from the DB are migrated in. Secrets live in the existing per-profile AES-256-GCM vault file (a new `Secrets map[string]string` field on `VaultData`); three new API routes let the server load/modify/save secrets, and `BuildUserData` appends them to `/etc/agentsdx.env` at session start.

**Tech Stack:** Go, SQLite (`modernc.org/sqlite`), `net/http` + chi, AES-256-GCM vault (existing), cobra CLI

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `shared/types/vault.go` | Modify | Add `Secrets map[string]string` to `VaultData` |
| `server/internal/db/db.go` | Modify | Add `spec TEXT` column migration; YAML-to-DB startup migration helper |
| `server/internal/profile/store.go` | Modify | Remove all YAML file I/O; read/write `spec` JSON column only |
| `server/internal/profile/store_test.go` | Modify | Update test names/assertions to match DB-only store |
| `server/internal/api/handler.go` | Modify | Add `setSecret`, `deleteSecret`, `listSecrets` handlers; register routes |
| `server/internal/api/handler_test.go` | Modify | Add tests for the three new secret routes |
| `server/internal/vm/userdata.go` | Modify | Add `secrets map[string]string` param; append secrets to env file content |
| `server/internal/vm/userdata_test.go` | Modify | Update existing call sites; add test for secret injection |
| `server/internal/session/manager.go` | Modify | Pass `vaultData.Secrets` to `BuildUserData` |
| `cli/internal/client/client.go` | Modify | Add `SetSecret`, `DeleteSecret`, `ListSecrets` methods |
| `cli/cmd/agentsdx/secrets.go` | Create | `secrets set/delete/list` cobra commands |
| `cli/cmd/agentsdx/main.go` | Modify | Register `secrets` command |
| `server/cmd/agentsdxd/serve.go` | Modify | Call YAML migration on startup |

---

## Task 1: Add `Secrets` field to `VaultData`

**Files:**
- Modify: `shared/types/vault.go`
- Modify: `shared/types/vault_test.go`

- [ ] **Step 1: Write a failing test for the new field**

In `shared/types/vault_test.go`, add:

```go
func TestVaultData_SecretsField(t *testing.T) {
	vd := types.VaultData{
		Secrets: map[string]string{"GITHUB_PAT": "ghp_abc", "OPENAI_API_KEY": "sk-xyz"},
	}
	data, err := json.Marshal(vd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got types.VaultData
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Secrets["GITHUB_PAT"] != "ghp_abc" {
		t.Errorf("GITHUB_PAT: got %q, want %q", got.Secrets["GITHUB_PAT"], "ghp_abc")
	}
	if got.Secrets["OPENAI_API_KEY"] != "sk-xyz" {
		t.Errorf("OPENAI_API_KEY: got %q, want %q", got.Secrets["OPENAI_API_KEY"], "sk-xyz")
	}
}
```

You'll also need `"encoding/json"` in the imports if not already present. Check `shared/types/vault_test.go` first.

- [ ] **Step 2: Run the test to confirm it fails**

```bash
cd shared && go test ./types/... -run TestVaultData_SecretsField -v
```

Expected: FAIL — field does not exist yet.

- [ ] **Step 3: Add the field to `VaultData`**

In `shared/types/vault.go`, change the struct to:

```go
type VaultData struct {
	GitPrivateKey      string            `json:"git_private_key"`
	GitPublicKey       string            `json:"git_public_key"`
	VMAccessPrivateKey string            `json:"vm_access_private_key"`
	VMAccessPublicKey  string            `json:"vm_access_public_key"`
	AgentStatePaths    []string          `json:"agent_state_paths"`
	Secrets            map[string]string `json:"secrets,omitempty"`
}
```

- [ ] **Step 4: Run the test to confirm it passes**

```bash
cd shared && go test ./types/... -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add shared/types/vault.go shared/types/vault_test.go
git commit -m "feat(shared): add Secrets field to VaultData"
```

---

## Task 2: Migrate profile store to full SQLite

**Files:**
- Modify: `server/internal/db/db.go`
- Modify: `server/internal/profile/store.go`
- Modify: `server/internal/profile/store_test.go`

- [ ] **Step 1: Add `spec` column to the schema migration in `db.go`**

In `server/internal/db/db.go`, in the `migrate` function, replace the `CREATE TABLE IF NOT EXISTS profiles` statement with:

```go
if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS profiles (
    name TEXT PRIMARY KEY,
    spec TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`); err != nil {
    return fmt.Errorf("create profiles table: %w", err)
}
// Add spec column to existing databases (safe no-op on new DBs).
_, _ = tx.Exec(`ALTER TABLE profiles ADD COLUMN spec TEXT NOT NULL DEFAULT ''`)
```

- [ ] **Step 2: Rewrite `profile/store.go` to use DB only**

Replace the entire file content with:

```go
// Package profile provides a profile store backed by SQLite.
package profile

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/duck-labs/agentsdx-shared/types"
)

// Store manages profiles using SQLite as the single source of truth.
type Store struct {
	db *sql.DB
}

// NewStore creates a new Store. profilesDir is accepted but ignored (kept for
// call-site compatibility during migration; remove after all callers are updated).
func NewStore(db *sql.DB, _ string) *Store {
	return &Store{db: db}
}

func (s *Store) Create(spec types.ProfileSpec) error {
	data, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshal profile spec: %w", err)
	}
	_, err = s.db.Exec(
		`INSERT INTO profiles (name, spec) VALUES (?, ?)`,
		spec.Name, string(data),
	)
	if err != nil {
		return fmt.Errorf("insert profile: %w", err)
	}
	return nil
}

func (s *Store) Get(name string) (types.ProfileSpec, error) {
	var raw string
	err := s.db.QueryRow(`SELECT spec FROM profiles WHERE name = ?`, name).Scan(&raw)
	if err == sql.ErrNoRows {
		return types.ProfileSpec{}, fmt.Errorf("profile %q not found", name)
	}
	if err != nil {
		return types.ProfileSpec{}, fmt.Errorf("query profile: %w", err)
	}
	var spec types.ProfileSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return types.ProfileSpec{}, fmt.Errorf("unmarshal profile spec: %w", err)
	}
	return spec, nil
}

func (s *Store) List() ([]types.ProfileSpec, error) {
	rows, err := s.db.Query(`SELECT spec FROM profiles ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query profiles: %w", err)
	}
	defer rows.Close()

	specs := make([]types.ProfileSpec, 0)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan profile spec: %w", err)
		}
		var spec types.ProfileSpec
		if err := json.Unmarshal([]byte(raw), &spec); err != nil {
			return nil, fmt.Errorf("unmarshal profile spec: %w", err)
		}
		specs = append(specs, spec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate profiles: %w", err)
	}
	return specs, nil
}

func (s *Store) Delete(name string) error {
	res, err := s.db.Exec(`DELETE FROM profiles WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete profile: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("profile %q not found", name)
	}
	return nil
}
```

- [ ] **Step 3: Update `profile/store_test.go` to match DB-only store**

Replace the entire file content with:

```go
package profile_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/db"
	"github.com/duck-labs/agentsdx-server/internal/profile"
	"github.com/duck-labs/agentsdx-shared/types"
)

func sampleSpec(name string) types.ProfileSpec {
	return types.ProfileSpec{
		Name: name,
		Infrastructure: types.InfrastructureConfig{
			Provider: "hetzner",
			Image:    "ubuntu-24.04",
			Tooling:  []string{"git", "node"},
		},
		Projects: []types.ProjectConfig{
			{Repo: "https://github.com/example/repo", Path: "/workspace/repo"},
		},
		Agent: types.AgentConfig{
			Provider: "claude",
			Skills:   []string{"coding"},
		},
	}
}

func newStore(t *testing.T) *profile.Store {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return profile.NewStore(conn, "")
}

func TestStore_CreateAndGet(t *testing.T) {
	s := newStore(t)
	spec := sampleSpec("test-profile")

	if err := s.Create(spec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Get(spec.Name)
	if err != nil {
		t.Fatalf("Get after Create: %v", err)
	}
	if !reflect.DeepEqual(spec, got) {
		t.Errorf("Get returned\n%+v\nwant\n%+v", got, spec)
	}
}

func TestStore_List_ReturnsAllProfiles(t *testing.T) {
	s := newStore(t)

	spec1 := sampleSpec("alpha")
	spec2 := sampleSpec("beta")

	if err := s.Create(spec1); err != nil {
		t.Fatalf("Create spec1: %v", err)
	}
	if err := s.Create(spec2); err != nil {
		t.Fatalf("Create spec2: %v", err)
	}

	all, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(all))
	}
	names := map[string]bool{}
	for _, p := range all {
		names[p.Name] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("missing profiles in list: %v", names)
	}
}

func TestStore_Delete_RemovesProfile(t *testing.T) {
	s := newStore(t)
	spec := sampleSpec("to-delete")

	if err := s.Create(spec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Delete(spec.Name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(spec.Name); err == nil {
		t.Error("Get after Delete: expected error, got nil")
	}
	all, err := s.List()
	if err != nil {
		t.Fatalf("List after Delete: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected empty list after delete, got %v", all)
	}
}

func TestStore_Create_FailsOnDuplicate(t *testing.T) {
	s := newStore(t)
	spec := sampleSpec("dup-profile")

	if err := s.Create(spec); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if err := s.Create(spec); err == nil {
		t.Fatal("expected error on duplicate Create, got nil")
	}

	got, err := s.Get(spec.Name)
	if err != nil {
		t.Fatalf("Get after failed duplicate: %v", err)
	}
	if !reflect.DeepEqual(spec, got) {
		t.Errorf("original profile corrupted")
	}
}

func TestStore_Get_NotFound(t *testing.T) {
	s := newStore(t)
	if _, err := s.Get("no-such-profile"); err == nil {
		t.Error("expected error for missing profile, got nil")
	}
}
```

- [ ] **Step 4: Run all profile tests**

```bash
cd server && go test ./internal/profile/... -v
```

Expected: all PASS.

- [ ] **Step 5: Run all server tests to check nothing else broke**

```bash
cd server && go test ./... 
```

Expected: all PASS (handler tests use `profile.NewStore(conn, filepath.Join(dir, "profiles"))` which still works because the second arg is now ignored).

- [ ] **Step 6: Commit**

```bash
git add server/internal/db/db.go server/internal/profile/store.go server/internal/profile/store_test.go
git commit -m "feat(server): migrate profile store from YAML+SQLite to full SQLite"
```

---

## Task 3: YAML-to-DB startup migration

**Files:**
- Modify: `server/internal/db/db.go` (add exported migration helper)
- Modify: `server/cmd/agentsdxd/serve.go` (call it on startup)

- [ ] **Step 1: Add `MigrateYAMLProfiles` to `db.go`**

At the bottom of `server/internal/db/db.go`, add:

```go
import (
    // existing imports...
    "encoding/json"
    "os"
    "path/filepath"
    "strings"

    "gopkg.in/yaml.v3"

    "github.com/duck-labs/agentsdx-shared/types"
)

// MigrateYAMLProfiles scans profilesDir for *.yaml files and inserts any profile
// whose name is not already in the DB (i.e., has an empty or missing spec column).
// YAML files are left on disk untouched after migration.
func MigrateYAMLProfiles(conn *sql.DB, profilesDir string) error {
    entries, err := os.ReadDir(profilesDir)
    if err != nil {
        if os.IsNotExist(err) {
            return nil
        }
        return fmt.Errorf("read profiles dir: %w", err)
    }
    for _, e := range entries {
        if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
            continue
        }
        data, err := os.ReadFile(filepath.Join(profilesDir, e.Name()))
        if err != nil {
            return fmt.Errorf("read %s: %w", e.Name(), err)
        }
        var spec types.ProfileSpec
        if err := yaml.Unmarshal(data, &spec); err != nil {
            return fmt.Errorf("unmarshal %s: %w", e.Name(), err)
        }
        // Check if already present with a non-empty spec.
        var existing string
        _ = conn.QueryRow(`SELECT spec FROM profiles WHERE name = ?`, spec.Name).Scan(&existing)
        if existing != "" {
            continue
        }
        specJSON, err := json.Marshal(spec)
        if err != nil {
            return fmt.Errorf("marshal %s: %w", e.Name(), err)
        }
        _, err = conn.Exec(
            `INSERT INTO profiles (name, spec) VALUES (?, ?)
             ON CONFLICT(name) DO UPDATE SET spec = excluded.spec`,
            spec.Name, string(specJSON),
        )
        if err != nil {
            return fmt.Errorf("insert migrated profile %s: %w", spec.Name, err)
        }
    }
    return nil
}
```

Also add the required imports at the top of `db.go` (merge with existing imports):
- `"encoding/json"`
- `"os"`
- `"path/filepath"`
- `"strings"`
- `"gopkg.in/yaml.v3"`
- `"github.com/duck-labs/agentsdx-shared/types"`

Check `server/go.mod` — if `gopkg.in/yaml.v3` is not already a direct dependency, add it:

```bash
cd server && go get gopkg.in/yaml.v3
```

- [ ] **Step 2: Call `MigrateYAMLProfiles` in `serve.go`**

In `server/cmd/agentsdxd/serve.go`, after `db.Open(...)` succeeds and before starting the HTTP server, add:

```go
if err := db.MigrateYAMLProfiles(conn, profilesDir); err != nil {
    log.Printf("WARN: YAML profile migration: %v", err)
}
```

Where `profilesDir` is whatever variable holds the profiles directory path in that file. Read `serve.go` to find the exact variable name.

- [ ] **Step 3: Build to confirm it compiles**

```bash
cd server && go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add server/internal/db/db.go server/cmd/agentsdxd/serve.go server/go.mod server/go.sum
git commit -m "feat(server): migrate YAML profiles to SQLite on startup"
```

---

## Task 4: Add secret management API handlers

**Files:**
- Modify: `server/internal/api/handler.go`
- Modify: `server/internal/api/handler_test.go`

- [ ] **Step 1: Write failing tests for the three new routes**

In `server/internal/api/handler_test.go`, add these tests (before the final closing brace of the file):

```go
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

	// Create profile and set a secret.
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
```

- [ ] **Step 2: Run to confirm they fail**

```bash
cd server && go test ./internal/api/... -run "TestHandler_SetSecret|TestHandler_DeleteSecret|TestHandler_ListSecrets" -v
```

Expected: FAIL — routes return 404.

- [ ] **Step 3: Add the three handlers to `handler.go`**

In `server/internal/api/handler.go`, add the new routes to `Router()`:

```go
r.Put("/profiles/{name}/secrets/{key}", h.setSecret)
r.Delete("/profiles/{name}/secrets/{key}", h.deleteSecret)
r.Get("/profiles/{name}/secrets", h.listSecrets)
```

Then add the three handler functions at the bottom of the file (before `writeJSON` and `writeError`):

```go
func (h *Handler) setSecret(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	key := chi.URLParam(r, "key")

	var req struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	var vd types.VaultData
	if vault.VaultExists(h.vaultDir, name) {
		loaded, err := vault.LoadVaultData(h.vaultDir, name, h.vaultSecret)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load vault")
			return
		}
		vd = loaded
	} else {
		vmPriv, vmPub, err := vault.GenerateKeyPair()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate vm key pair")
			return
		}
		gitPriv, gitPub, err := vault.GenerateKeyPair()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate git key pair")
			return
		}
		vd = types.DefaultVaultData()
		vd.VMAccessPrivateKey = vmPriv
		vd.VMAccessPublicKey = vmPub
		vd.GitPrivateKey = gitPriv
		vd.GitPublicKey = gitPub
	}

	if vd.Secrets == nil {
		vd.Secrets = make(map[string]string)
	}
	vd.Secrets[key] = req.Value

	if err := vault.StoreVaultData(h.vaultDir, name, h.vaultSecret, vd); err != nil {
		writeError(w, http.StatusInternalServerError, "store vault")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteSecret(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	key := chi.URLParam(r, "key")

	if !vault.VaultExists(h.vaultDir, name) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	vd, err := vault.LoadVaultData(h.vaultDir, name, h.vaultSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load vault")
		return
	}
	delete(vd.Secrets, key)
	if err := vault.StoreVaultData(h.vaultDir, name, h.vaultSecret, vd); err != nil {
		writeError(w, http.StatusInternalServerError, "store vault")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listSecrets(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	if !vault.VaultExists(h.vaultDir, name) {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	vd, err := vault.LoadVaultData(h.vaultDir, name, h.vaultSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load vault")
		return
	}
	keys := make([]string, 0, len(vd.Secrets))
	for k := range vd.Secrets {
		keys = append(keys, k)
	}
	writeJSON(w, http.StatusOK, keys)
}
```

- [ ] **Step 4: Run the new tests**

```bash
cd server && go test ./internal/api/... -run "TestHandler_SetSecret|TestHandler_DeleteSecret|TestHandler_ListSecrets" -v
```

Expected: all PASS.

- [ ] **Step 5: Run full server test suite**

```bash
cd server && go test ./...
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add server/internal/api/handler.go server/internal/api/handler_test.go
git commit -m "feat(server): add secret set/delete/list API routes"
```

---

## Task 5: Inject secrets into VM user-data

**Files:**
- Modify: `server/internal/vm/userdata.go`
- Modify: `server/internal/vm/userdata_test.go`
- Modify: `server/internal/session/manager.go`

- [ ] **Step 1: Write a failing test for secret injection**

In `server/internal/vm/userdata_test.go`, add:

```go
func TestBuildUserData_InjectsSecrets(t *testing.T) {
	secrets := map[string]string{
		"GITHUB_PAT":    "ghp_abc",
		"OPENAI_API_KEY": "sk-xyz",
	}
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-1", "http://server:8080", "myprofile", secrets)
	if !strings.Contains(ud, "GITHUB_PAT=ghp_abc") {
		t.Errorf("user-data missing GITHUB_PAT=ghp_abc")
	}
	if !strings.Contains(ud, "OPENAI_API_KEY=sk-xyz") {
		t.Errorf("user-data missing OPENAI_API_KEY=sk-xyz")
	}
}

func TestBuildUserData_NoSecretsOmitsExtraLines(t *testing.T) {
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-1", "http://server:8080", "myprofile", nil)
	if !strings.Contains(ud, "AGENTSDX_PROFILE=myprofile") {
		t.Errorf("user-data missing AGENTSDX_PROFILE")
	}
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
cd server && go test ./internal/vm/... -run "TestBuildUserData_InjectsSecrets|TestBuildUserData_NoSecretsOmitsExtraLines" -v
```

Expected: FAIL — `BuildUserData` does not accept a `secrets` parameter.

- [ ] **Step 3: Update `BuildUserData` signature and implementation**

Replace `server/internal/vm/userdata.go` with:

```go
package vm

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// BuildUserData returns cloud-init user-data that injects SSH keys, agent env vars,
// and any per-profile secrets as additional env vars in /etc/agentsdx.env.
func BuildUserData(authorizedKey, gitPrivateKey, sessionID, serverURL, profileName string, secrets map[string]string) string {
	encodedKey := base64.StdEncoding.EncodeToString([]byte(gitPrivateKey))

	var extraEnv strings.Builder
	for k, v := range secrets {
		fmt.Fprintf(&extraEnv, "      %s=%s\n", k, v)
	}

	return fmt.Sprintf(`#cloud-config
ssh_authorized_keys:
  - %s
write_files:
  - path: /root/.ssh/id_rsa
    permissions: '0600'
    encoding: b64
    content: %s
  - path: /etc/agentsdx.env
    permissions: '0644'
    content: |
      AGENTSDX_SERVER_URL=%s
      AGENTSDX_SESSION_ID=%s
      AGENTSDX_PROFILE=%s
%sruncmd:
  - mkdir -p /root/.ssh && chmod 700 /root/.ssh
`, authorizedKey, encodedKey, serverURL, sessionID, profileName, extraEnv.String())
}
```

- [ ] **Step 4: Fix existing `BuildUserData` call sites in `userdata_test.go`**

Update the existing tests that call `BuildUserData` without a secrets parameter to pass `nil`:

```go
func TestBuildUserData_ContainsSSHKey(t *testing.T) {
	ud := vm.BuildUserData("ssh-rsa AAAA...", "-----BEGIN OPENSSH PRIVATE KEY-----\nABC\n-----END OPENSSH PRIVATE KEY-----", "sess-1", "http://server:8080", "myprofile", nil)
	// ... rest unchanged
}

func TestBuildUserData_ContainsEnvFile(t *testing.T) {
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-42", "http://server:8080", "work-backend", nil)
	// ... rest unchanged
}
```

- [ ] **Step 5: Fix the call site in `session/manager.go`**

In `server/internal/session/manager.go`, find the `BuildUserData(...)` call inside `Start` and update it to pass `vaultData.Secrets`:

```go
UserData: vm.BuildUserData(
    vaultData.VMAccessPublicKey,
    vaultData.GitPrivateKey,
    id,
    m.serverURL,
    profileName,
    vaultData.Secrets,
),
```

- [ ] **Step 6: Run all vm and session tests**

```bash
cd server && go test ./internal/vm/... ./internal/session/... -v
```

Expected: all PASS.

- [ ] **Step 7: Run full server test suite**

```bash
cd server && go test ./...
```

Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add server/internal/vm/userdata.go server/internal/vm/userdata_test.go server/internal/session/manager.go
git commit -m "feat(server): inject profile secrets into VM user-data env file"
```

---

## Task 6: CLI secrets commands

**Files:**
- Create: `cli/cmd/agentsdx/secrets.go`
- Modify: `cli/internal/client/client.go`
- Modify: `cli/cmd/agentsdx/main.go`

- [ ] **Step 1: Add client methods**

In `cli/internal/client/client.go`, append:

```go
func (c *Client) SetSecret(profile, key, value string) error {
	body, _ := json.Marshal(map[string]string{"value": value})
	req, err := http.NewRequest(http.MethodPut, c.base+"/profiles/"+profile+"/secrets/"+key, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) DeleteSecret(profile, key string) error {
	req, err := http.NewRequest(http.MethodDelete, c.base+"/profiles/"+profile+"/secrets/"+key, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) ListSecrets(profile string) ([]string, error) {
	resp, err := c.http.Get(c.base + "/profiles/" + profile + "/secrets")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}
	var keys []string
	return keys, json.NewDecoder(resp.Body).Decode(&keys)
}
```

- [ ] **Step 2: Create `cli/cmd/agentsdx/secrets.go`**

```go
package main

import (
	"fmt"

	"github.com/duck-labs/agentsdx-cli/internal/client"
	"github.com/spf13/cobra"
)

func newSecretsCmd(c *client.Client) *cobra.Command {
	parent := &cobra.Command{
		Use:   "secrets",
		Short: "Manage per-profile secrets",
	}
	parent.AddCommand(newSecretsSetCmd(c))
	parent.AddCommand(newSecretsDeleteCmd(c))
	parent.AddCommand(newSecretsListCmd(c))
	return parent
}

func newSecretsSetCmd(c *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "set <profile> <KEY> <VALUE>",
		Short: "Set or overwrite a secret for a profile",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile, key, value := args[0], args[1], args[2]
			if err := c.SetSecret(profile, key, value); err != nil {
				return fmt.Errorf("set secret: %w", err)
			}
			fmt.Printf("Secret %q set for profile %q.\n", key, profile)
			return nil
		},
	}
}

func newSecretsDeleteCmd(c *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <profile> <KEY>",
		Short: "Remove a secret from a profile",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile, key := args[0], args[1]
			if err := c.DeleteSecret(profile, key); err != nil {
				return fmt.Errorf("delete secret: %w", err)
			}
			fmt.Printf("Secret %q deleted from profile %q.\n", key, profile)
			return nil
		},
	}
}

func newSecretsListCmd(c *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "list <profile>",
		Short: "List secret key names for a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keys, err := c.ListSecrets(args[0])
			if err != nil {
				return fmt.Errorf("list secrets: %w", err)
			}
			if len(keys) == 0 {
				fmt.Println("No secrets set.")
				return nil
			}
			for _, k := range keys {
				fmt.Println(k)
			}
			return nil
		},
	}
}
```

- [ ] **Step 3: Register the command in `main.go`**

In `cli/cmd/agentsdx/main.go`, find where other subcommands are added to the root command (look for `rootCmd.AddCommand(...)` calls) and add:

```go
rootCmd.AddCommand(newSecretsCmd(c))
```

- [ ] **Step 4: Build the CLI to confirm it compiles**

```bash
cd cli && go build ./...
```

Expected: no errors.

- [ ] **Step 5: Run CLI tests**

```bash
cd cli && go test ./...
```

Expected: all PASS.

- [ ] **Step 6: Smoke test the new commands appear in help**

```bash
cd cli && go run ./cmd/agentsdx secrets --help
```

Expected: output shows `set`, `delete`, `list` subcommands.

- [ ] **Step 7: Commit**

```bash
git add cli/cmd/agentsdx/secrets.go cli/cmd/agentsdx/main.go cli/internal/client/client.go
git commit -m "feat(cli): add secrets set/delete/list commands"
```

---

## Self-Review Checklist

- [x] **Spec coverage:**
  - Profile store → full SQLite: Tasks 2 & 3
  - YAML migration on startup: Task 3
  - `Secrets` field on VaultData: Task 1
  - API routes (PUT/DELETE/GET): Task 4
  - CLI `secrets set/delete/list`: Task 6
  - Client methods `SetSecret/DeleteSecret/ListSecrets`: Task 6
  - VM injection via cloud-init: Task 5
  - Values never returned via API (GET returns keys only): Task 4 handler + test

- [x] **Placeholder scan:** No TBDs, all code blocks are complete.

- [x] **Type consistency:**
  - `BuildUserData` signature updated in Task 5 and all call sites (test + manager) are fixed in the same task.
  - `profile.NewStore` second arg made `_` in Task 2 — existing test helpers pass `""` in Task 2's updated test, and handler tests pass `filepath.Join(dir, "profiles")` which is silently ignored (still valid).
  - `VaultData.Secrets` defined in Task 1, used in Tasks 4 and 5.
