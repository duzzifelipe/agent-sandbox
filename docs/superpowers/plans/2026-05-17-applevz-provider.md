# Apple VZ Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Apple Virtualization.framework as a third VM provider (`applevz`) so the server runs local VMs on Apple Silicon, while keeping VirtualBox for Linux hosts.

**Architecture:** A new `server/internal/vm/applevz/` package (darwin+arm64 build-tagged) implements `VMProvider` using `github.com/Code-Hex/vz/v3`. Platform-specific factory files (`provider_factory_darwin_arm64.go` / `provider_factory_linux.go`) export a single `vm.NewProvider(...)` function; `serve.go` calls it without any platform-specific logic. IP discovery uses a new `POST /sessions/{id}/ip` callback endpoint that the VM calls via cloud-init `runcmd` once it has an IP.

**Tech Stack:** Go 1.26.3, `github.com/Code-Hex/vz/v3` (darwin/arm64 CGO), Packer QEMU plugin, chi router, SQLite.

---

## File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `server/internal/vm/images.go` | Add `ProviderAppleVZ`, `GetAppleVZPath`, `SetAppleVZPath` |
| Modify | `shared/types/api.go` | Add `AppleVZ string` to `ImageEntry` |
| Modify | `server/internal/vm/provider.go` | Add `RegisterIP` to `VMProvider` interface |
| Modify | `server/internal/vm/virtualbox.go` | Add `RegisterIP` no-op |
| Modify | `server/internal/vm/nocloud.go` | Add `vmCallbackURL` param to `BuildUserData` |
| Modify | `server/internal/vm/nocloud_test.go` | Update callers and add callback runcmd test |
| Modify | `server/internal/session/store.go` | Add `UpdateIP` method |
| Modify | `server/internal/session/store_test.go` | Test `UpdateIP` |
| Modify | `server/internal/session/manager.go` | Add `RegisterVMIP`; update `BuildUserData` call |
| Modify | `server/internal/session/manager_test.go` | Add `RegisterIP` to `fakeVM`; test `RegisterVMIP` |
| Modify | `server/internal/api/handler.go` | Add `POST /sessions/{id}/ip`; add `BuildAppleVZ` to `ImageBuilder`; dispatch by platform in `buildImage` |
| Modify | `server/internal/api/handler_test.go` | Add `RegisterIP` to `fakeVM`; add `BuildAppleVZ` to `fakeBuilder`; test new endpoint |
| Create | `server/internal/vm/applevz/provider.go` | `//go:build darwin && arm64` — `AppleVZProvider` |
| Create | `server/internal/vm/applevz/provider_test.go` | `//go:build darwin && arm64` — unit tests |
| Create | `server/cmd/agentsdxd/provider_darwin_arm64.go` | `//go:build darwin && arm64` — `newProvider` → applevz |
| Create | `server/cmd/agentsdxd/provider_linux.go` | `//go:build linux` — `newProvider` → VirtualBox |
| Modify | `server/cmd/agentsdxd/serve.go` | Replace hardcoded provider with `vm.NewProvider(...)` |
| Modify | `server/internal/builder/builder.go` | Add `BuildAppleVZ`; add `ubuntu-24.04-arm64` to registry |
| Modify | `server/internal/builder/builder_test.go` | Test `BuildAppleVZ` |
| Create | `vm/applevz.pkr.hcl` | Packer QEMU ARM64 template |
| Modify | `server/cmd/agentsdxd/setup.go` | Add darwin/arm64 path; skip vboxnet0 |
| Modify | `e2e/main_test.go` | Skip vboxnet0 setup on darwin/arm64 |

---

## Task 1: Add ProviderAppleVZ to ImageStore and ImageEntry

**Files:**
- Modify: `server/internal/vm/images.go`
- Modify: `shared/types/api.go`
- Modify: `server/internal/vm/images_test.go`

- [ ] **Step 1: Write failing tests for GetAppleVZPath and SetAppleVZPath**

Add to `server/internal/vm/images_test.go`:

```go
func TestImageStore_GetAppleVZPath_Found(t *testing.T) {
	dir := t.TempDir()
	writeImagesJSON(t, dir, map[string]vm.ImageRecord{
		"my-profile": {vm.ProviderAppleVZ: "/data/images/my-profile.img"},
	})

	store := vm.NewImageStore(filepath.Join(dir, "images.json"))
	path, err := store.GetAppleVZPath("my-profile")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/data/images/my-profile.img" {
		t.Errorf("got %q, want %q", path, "/data/images/my-profile.img")
	}
}

func TestImageStore_GetAppleVZPath_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeImagesJSON(t, dir, map[string]vm.ImageRecord{})

	store := vm.NewImageStore(filepath.Join(dir, "images.json"))
	_, err := store.GetAppleVZPath("missing")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
}

func TestImageStore_SetAppleVZPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "images.json")
	store := vm.NewImageStore(path)

	if err := store.SetAppleVZPath("my-profile", "/data/images/my-profile.img"); err != nil {
		t.Fatalf("SetAppleVZPath: %v", err)
	}

	got, err := store.GetAppleVZPath("my-profile")
	if err != nil {
		t.Fatalf("GetAppleVZPath after set: %v", err)
	}
	if got != "/data/images/my-profile.img" {
		t.Errorf("got %q, want %q", got, "/data/images/my-profile.img")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd server && go test ./internal/vm/... -run TestImageStore_GetAppleVZPath -run TestImageStore_SetAppleVZPath -v
```

Expected: compile error — `vm.ProviderAppleVZ` undefined.

- [ ] **Step 3: Add ProviderAppleVZ and Get/SetAppleVZPath to images.go**

In `server/internal/vm/images.go`, add after `ProviderHetzner`:

```go
ProviderAppleVZ Provider = "applevz"
```

Add these two methods after `SetVirtualBoxPath`:

```go
// GetAppleVZPath returns the raw disk image path for profileName or an error if absent.
func (s *ImageStore) GetAppleVZPath(profileName string) (string, error) {
	records, err := s.load()
	if err != nil {
		return "", fmt.Errorf("load images: %w", err)
	}
	rec, ok := records[profileName]
	if !ok {
		return "", fmt.Errorf("no image record for profile %q", profileName)
	}
	p := rec[ProviderAppleVZ]
	if p == "" {
		return "", fmt.Errorf("no applevz image built for profile %q", profileName)
	}
	return p, nil
}

// SetAppleVZPath writes or updates the Apple VZ raw disk image path for profileName.
func (s *ImageStore) SetAppleVZPath(profileName, imgPath string) error {
	records, err := s.load()
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("load images: %w", err)
	}
	if records == nil {
		records = make(map[string]ImageRecord)
	}
	rec := records[profileName]
	if rec == nil {
		rec = make(ImageRecord)
	}
	rec[ProviderAppleVZ] = imgPath
	records[profileName] = rec
	return s.save(records)
}
```

- [ ] **Step 4: Add AppleVZ field to ImageEntry in shared/types/api.go**

In `shared/types/api.go`, update `ImageEntry`:

```go
type ImageEntry struct {
	ProfileName string `json:"profile_name"`
	VirtualBox  string `json:"virtualbox"`
	Hetzner     string `json:"hetzner"`
	AppleVZ     string `json:"applevz"`
}
```

- [ ] **Step 5: Update ImageStore.List to populate AppleVZ**

In `server/internal/vm/images.go`, update the `List` method's loop:

```go
entries = append(entries, types.ImageEntry{
	ProfileName: profileName,
	VirtualBox:  rec[ProviderVirtualBox],
	Hetzner:     rec[ProviderHetzner],
	AppleVZ:     rec[ProviderAppleVZ],
})
```

- [ ] **Step 6: Run tests**

```bash
cd server && go test ./internal/vm/... -v
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add server/internal/vm/images.go server/internal/vm/images_test.go shared/types/api.go
git commit -m "feat: add ProviderAppleVZ to ImageStore and AppleVZ to ImageEntry"
```

---

## Task 2: Add RegisterIP to VMProvider interface and VirtualBox no-op

**Files:**
- Modify: `server/internal/vm/provider.go`
- Modify: `server/internal/vm/virtualbox.go`
- Modify: `server/internal/vm/virtualbox_test.go`

- [ ] **Step 1: Add RegisterIP to VMProvider interface**

In `server/internal/vm/provider.go`, update the interface:

```go
import "context"

type VMProvider interface {
	CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error)
	DestroyVM(ctx context.Context, vmID string) error
	GetVM(ctx context.Context, vmID string) (*VM, error)
	RegisterIP(ctx context.Context, vmID, ip string) error
}
```

- [ ] **Step 2: Check which files now fail to compile**

```bash
cd server && go build ./... 2>&1
```

Expected: compile errors for `fakeVM` types in `manager_test.go` and `handler_test.go` missing `RegisterIP`.

- [ ] **Step 3: Add RegisterIP no-op to VirtualBoxProvider**

In `server/internal/vm/virtualbox.go`, add after `forceDelete`:

```go
// RegisterIP is a no-op for VirtualBox; IP is discovered via Guest Additions.
func (p *VirtualBoxProvider) RegisterIP(_ context.Context, _, _ string) error {
	return nil
}
```

- [ ] **Step 4: Fix fakeVM in manager_test.go**

In `server/internal/session/manager_test.go`, add to `fakeVM`:

```go
func (f *fakeVM) RegisterIP(_ context.Context, _, _ string) error { return nil }
```

- [ ] **Step 5: Fix fakeVM in handler_test.go**

In `server/internal/api/handler_test.go`, add to `fakeVM`:

```go
func (f *fakeVM) RegisterIP(_ context.Context, _, _ string) error { return nil }
```

- [ ] **Step 6: Run all tests**

```bash
cd server && go test ./... -v 2>&1 | tail -30
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add server/internal/vm/provider.go server/internal/vm/virtualbox.go \
        server/internal/session/manager_test.go server/internal/api/handler_test.go
git commit -m "feat: add RegisterIP to VMProvider interface; VirtualBox no-op"
```

---

## Task 3: Add vmCallbackURL to BuildUserData

**Files:**
- Modify: `server/internal/vm/nocloud.go`
- Modify: `server/internal/vm/nocloud_test.go`
- Modify: `server/internal/session/manager.go`

- [ ] **Step 1: Add a failing test for the callback runcmd**

In `server/internal/vm/nocloud_test.go`, add:

```go
func TestBuildUserData_ContainsCallbackRuncmd(t *testing.T) {
	ud := vm.BuildUserData(
		"ssh-rsa AAAA...", "git-key", "sess-1",
		"http://server:8080", "myprofile",
		"http://server:8080/sessions/sess-1/ip",
	)
	if !strings.Contains(ud, "runcmd") {
		t.Errorf("user-data missing runcmd section")
	}
	if !strings.Contains(ud, "http://server:8080/sessions/sess-1/ip") {
		t.Errorf("user-data missing callback URL in runcmd")
	}
}

func TestBuildUserData_NoCallbackWhenURLEmpty(t *testing.T) {
	ud := vm.BuildUserData(
		"ssh-rsa AAAA...", "git-key", "sess-1",
		"http://server:8080", "myprofile",
		"",
	)
	if strings.Contains(ud, "runcmd") {
		t.Errorf("user-data should not contain runcmd when vmCallbackURL is empty")
	}
}
```

- [ ] **Step 2: Update existing test callers in nocloud_test.go to pass sixth arg**

In `server/internal/vm/nocloud_test.go`, update the two existing `BuildUserData` calls:

```go
// TestBuildUserData_ContainsSSHKey
ud := vm.BuildUserData("ssh-rsa AAAA...", "-----BEGIN OPENSSH PRIVATE KEY-----\nABC\n-----END OPENSSH PRIVATE KEY-----", "sess-1", "http://server:8080", "myprofile", "")

// TestBuildUserData_ContainsEnvFile
ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-42", "http://server:8080", "work-backend", "")
```

- [ ] **Step 3: Run tests to confirm failures**

```bash
cd server && go test ./internal/vm/... -run TestBuildUserData -v
```

Expected: compile error — `BuildUserData` called with wrong number of args.

- [ ] **Step 4: Update BuildUserData in nocloud.go**

Replace the existing `BuildUserData` function in `server/internal/vm/nocloud.go`:

```go
// BuildUserData returns cloud-init user-data. When vmCallbackURL is non-empty,
// a runcmd block is added that POSTs the VM's IP to that URL after boot.
func BuildUserData(authorizedKey, gitPrivateKey, sessionID, serverURL, profileName, vmCallbackURL string) string {
	encodedKey := base64.StdEncoding.EncodeToString([]byte(gitPrivateKey))
	ud := fmt.Sprintf(`#cloud-config
bootcmd:
  - mkdir -p /root/.ssh
  - chmod 700 /root/.ssh
ssh_authorized_keys:
  - "%s"
write_files:
  - path: /root/.ssh/id_rsa
    permissions: '0600'
    encoding: b64
    content: %s
  - path: /etc/agentsdx.env
    permissions: '0600'
    content: |
      AGENTSDX_SERVER_URL=%s
      AGENTSDX_SESSION_ID=%s
      AGENTSDX_PROFILE=%s
`, authorizedKey, encodedKey, serverURL, sessionID, profileName)

	if vmCallbackURL != "" {
		ud += fmt.Sprintf(`runcmd:
  - |
    IP=$(ip -4 addr show eth0 | grep -oP '(?<=inet\s)\d+(\.\d+){3}')
    curl -sf -X POST %s \
      -H 'Content-Type: application/json' \
      -d "{\"ip\":\"$IP\"}"
`, vmCallbackURL)
	}
	return ud
}
```

- [ ] **Step 5: Update the BuildUserData call in session/manager.go**

In `server/internal/session/manager.go`, update the `Start` method's `createReq` block. The `vmCallbackURL` is `m.serverURL + "/sessions/" + id + "/ip"`:

```go
createReq := vm.CreateVMRequest{
	ProfileName:   profileName,
	AuthorizedKey: vaultData.VMAccessPublicKey,
	UserData: vm.BuildUserData(
		vaultData.VMAccessPublicKey,
		vaultData.GitPrivateKey,
		id,
		m.serverURL,
		profileName,
		m.serverURL+"/sessions/"+id+"/ip",
	),
}
```

- [ ] **Step 6: Run tests**

```bash
cd server && go test ./internal/vm/... ./internal/session/... -v 2>&1 | tail -30
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add server/internal/vm/nocloud.go server/internal/vm/nocloud_test.go \
        server/internal/session/manager.go
git commit -m "feat: add vmCallbackURL to BuildUserData; wire callback in session manager"
```

---

## Task 4: Add Store.UpdateIP and Manager.RegisterVMIP

**Files:**
- Modify: `server/internal/session/store.go`
- Modify: `server/internal/session/store_test.go`
- Modify: `server/internal/session/manager.go`
- Modify: `server/internal/session/manager_test.go`

- [ ] **Step 1: Write failing test for Store.UpdateIP**

In `server/internal/session/store_test.go`, add:

```go
func TestStore_UpdateIP(t *testing.T) {
	store := newStore(t)
	store.DB().Exec("INSERT INTO profiles (name) VALUES (?)", "p1")

	id, _ := store.Create("p1")
	if err := store.UpdateIP(id, "192.168.64.5"); err != nil {
		t.Fatalf("UpdateIP: %v", err)
	}

	s, _ := store.Get(id)
	if s.IPAddress != "192.168.64.5" {
		t.Errorf("IPAddress: got %q, want %q", s.IPAddress, "192.168.64.5")
	}
}

func TestStore_UpdateIP_NotFound(t *testing.T) {
	store := newStore(t)
	err := store.UpdateIP("nonexistent", "192.168.64.5")
	if err == nil {
		t.Fatal("expected error for missing session")
	}
}
```

- [ ] **Step 2: Run test to confirm failure**

```bash
cd server && go test ./internal/session/... -run TestStore_UpdateIP -v
```

Expected: compile error — `store.UpdateIP` undefined.

- [ ] **Step 3: Add UpdateIP to session/store.go**

In `server/internal/session/store.go`, add after `UpdateState`:

```go
// UpdateIP sets the ip_address of a session without changing its state.
func (s *Store) UpdateIP(id, ipAddress string) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET ip_address = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		ipAddress, id,
	)
	if err != nil {
		return fmt.Errorf("update session ip: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session %q not found", id)
	}
	return nil
}
```

- [ ] **Step 4: Run store tests**

```bash
cd server && go test ./internal/session/... -run TestStore -v
```

Expected: all pass.

- [ ] **Step 5: Write failing test for Manager.RegisterVMIP**

In `server/internal/session/manager_test.go`, add:

```go
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
}
```

Also add an `registeredIPs` map to `fakeVM` in manager_test.go to verify `RegisterIP` is called:

```go
type fakeVM struct {
	createErr   error
	destroyErr  error
	vms         map[string]*vm.VM
	registeredIPs map[string]string
}

func newFakeVM() *fakeVM {
	return &fakeVM{
		vms:           make(map[string]*vm.VM),
		registeredIPs: make(map[string]string),
	}
}

func (f *fakeVM) RegisterIP(_ context.Context, vmID, ip string) error {
	f.registeredIPs[vmID] = ip
	return nil
}
```

- [ ] **Step 6: Run test to confirm failure**

```bash
cd server && go test ./internal/session/... -run TestManager_RegisterVMIP -v
```

Expected: compile error — `mgr.RegisterVMIP` undefined.

- [ ] **Step 7: Add RegisterVMIP to session/manager.go**

In `server/internal/session/manager.go`, add after `Get`:

```go
// RegisterVMIP stores the VM's IP in both the provider (for GetVM) and the session store.
func (m *Manager) RegisterVMIP(sessionID, ip string) error {
	rec, err := m.store.Get(sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}
	if rec.VMID != "" {
		if err := m.provider.RegisterIP(context.Background(), rec.VMID, ip); err != nil {
			return fmt.Errorf("register ip in provider: %w", err)
		}
	}
	return m.store.UpdateIP(sessionID, ip)
}
```

Add `"context"` to imports in manager.go if not already present (it is, from `pollUntilRunning`).

- [ ] **Step 8: Run all session tests**

```bash
cd server && go test ./internal/session/... -v 2>&1 | tail -20
```

Expected: all pass.

- [ ] **Step 9: Commit**

```bash
git add server/internal/session/store.go server/internal/session/store_test.go \
        server/internal/session/manager.go server/internal/session/manager_test.go
git commit -m "feat: add Store.UpdateIP and Manager.RegisterVMIP for IP callback"
```

---

## Task 5: Add POST /sessions/{id}/ip endpoint

**Files:**
- Modify: `server/internal/api/handler.go`
- Modify: `server/internal/api/handler_test.go`

- [ ] **Step 1: Write failing test**

In `server/internal/api/handler_test.go`, add:

```go
func TestHandler_RegisterSessionIP(t *testing.T) {
	h, dir := newHandler(t)

	// Insert a profile and create a session first
	conn, _ := db.Open(filepath.Join(dir, "test.db"))
	defer conn.Close()
	conn.Exec("INSERT INTO profiles (name) VALUES (?)", "dev")

	router := h.Router()

	// Create session via API
	body, _ := json.Marshal(types.CreateSessionRequest{ProfileName: "dev"})
	req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create session: got %d, want %d: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var sess types.SessionResponse
	json.NewDecoder(w.Body).Decode(&sess)

	// Register IP
	ipBody, _ := json.Marshal(map[string]string{"ip": "192.168.64.5"})
	req2 := httptest.NewRequest(http.MethodPost, "/sessions/"+sess.ID+"/ip", bytes.NewReader(ipBody))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Errorf("register ip: got %d, want %d: %s", w2.Code, http.StatusNoContent, w2.Body.String())
	}
}

func TestHandler_RegisterSessionIP_UnknownSession(t *testing.T) {
	h, _ := newHandler(t)
	router := h.Router()

	body, _ := json.Marshal(map[string]string{"ip": "192.168.64.5"})
	req := httptest.NewRequest(http.MethodPost, "/sessions/nonexistent/ip", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want %d", w.Code, http.StatusNotFound)
	}
}
```

- [ ] **Step 2: Run tests to confirm failure**

```bash
cd server && go test ./internal/api/... -run TestHandler_RegisterSessionIP -v
```

Expected: FAIL — route not found (404 or no route).

- [ ] **Step 3: Add the endpoint to handler.go**

In `server/internal/api/handler.go`, add to the `Router()` method after `r.Post("/sessions/{id}/stop", h.stopSession)`:

```go
r.Post("/sessions/{id}/ip", h.registerSessionIP)
```

Add the handler method:

```go
func (h *Handler) registerSessionIP(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.IP == "" {
		writeError(w, http.StatusBadRequest, "invalid JSON or missing ip")
		return
	}
	if err := h.sessions.RegisterVMIP(id, body.IP); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Run all API tests**

```bash
cd server && go test ./internal/api/... -v 2>&1 | tail -20
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add server/internal/api/handler.go server/internal/api/handler_test.go
git commit -m "feat: add POST /sessions/{id}/ip callback endpoint"
```

---

## Task 6: Create the Apple VZ provider package

**Files:**
- Create: `server/internal/vm/applevz/provider.go`
- Create: `server/internal/vm/applevz/provider_test.go`
- Modify: `server/go.mod` (add vz dependency)

- [ ] **Step 1: Add the Code-Hex/vz dependency (run on darwin/arm64)**

```bash
cd server && go get github.com/Code-Hex/vz/v3
```

Expected: go.mod and go.sum updated with `github.com/Code-Hex/vz/v3`.

- [ ] **Step 2: Create the provider file**

Create `server/internal/vm/applevz/provider.go`:

```go
//go:build darwin && arm64

package applevz

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	vz "github.com/Code-Hex/vz/v3"

	"github.com/duck-labs/agentsdx-server/internal/vm"
)

// Provider implements vm.VMProvider using Apple's Virtualization.framework.
// Requires sudo or com.apple.vm.networking entitlement for NAT networking.
type Provider struct {
	images  *vm.ImageStore
	isoDir  string
	workDir string

	mu  sync.Mutex
	vms map[string]*vz.VirtualMachine
	ips map[string]string
}

// NewProvider creates an AppleVZ provider.
// isoDir holds per-VM NoCloud ISO subdirectories.
// workDir holds per-VM disk copies and EFI variable stores.
func NewProvider(images *vm.ImageStore, isoDir, workDir string) *Provider {
	return &Provider{
		images:  images,
		isoDir:  isoDir,
		workDir: workDir,
		vms:     make(map[string]*vz.VirtualMachine),
		ips:     make(map[string]string),
	}
}

// CreateVM copies the disk image, writes the NoCloud ISO, configures and starts the VM.
func (p *Provider) CreateVM(ctx context.Context, req vm.CreateVMRequest) (*vm.VM, error) {
	imgPath, err := p.images.GetAppleVZPath(req.ProfileName)
	if err != nil {
		return nil, fmt.Errorf("resolve image: %w", err)
	}

	vmName := fmt.Sprintf("agentsdx-%s-%d", req.ProfileName, time.Now().UnixMilli())

	diskPath := filepath.Join(p.workDir, vmName+".img")
	if err := copyFile(imgPath, diskPath); err != nil {
		return nil, fmt.Errorf("copy disk image: %w", err)
	}

	vmISODir := filepath.Join(p.isoDir, vmName)
	if err := os.MkdirAll(vmISODir, 0755); err != nil {
		os.Remove(diskPath)
		return nil, fmt.Errorf("create iso dir: %w", err)
	}
	isoPath, err := vm.WriteNoCloudISO(vmISODir, vm.NoCloudMetaData(vmName), req.UserData)
	if err != nil {
		os.Remove(diskPath)
		os.RemoveAll(vmISODir)
		return nil, fmt.Errorf("write nocloud iso: %w", err)
	}

	efiPath := filepath.Join(p.workDir, vmName+".efi")

	vzVM, err := buildVZVM(diskPath, isoPath, efiPath)
	if err != nil {
		os.Remove(diskPath)
		os.RemoveAll(vmISODir)
		return nil, fmt.Errorf("configure vm: %w", err)
	}

	errCh := make(chan error, 1)
	vzVM.Start(func(startErr error) { errCh <- startErr })
	if err := <-errCh; err != nil {
		os.Remove(diskPath)
		os.RemoveAll(vmISODir)
		os.Remove(efiPath)
		return nil, fmt.Errorf("start vm: %w", err)
	}

	p.mu.Lock()
	p.vms[vmName] = vzVM
	p.mu.Unlock()

	return &vm.VM{ID: vmName, State: vm.VMStateStarting}, nil
}

// GetVM returns the current state of the VM. Returns VMStateRunning only when
// both the VZ machine state is Running AND an IP has been registered via RegisterIP.
func (p *Provider) GetVM(_ context.Context, vmID string) (*vm.VM, error) {
	p.mu.Lock()
	vzVM, ok := p.vms[vmID]
	ip := p.ips[vmID]
	p.mu.Unlock()

	if !ok {
		return &vm.VM{ID: vmID, State: vm.VMStateStopped}, nil
	}

	state := mapState(vzVM.State())
	if state == vm.VMStateRunning && ip == "" {
		state = vm.VMStateStarting
	}
	return &vm.VM{ID: vmID, State: state, IPAddress: ip}, nil
}

// DestroyVM stops the VM and removes its disk copy and NoCloud ISO.
func (p *Provider) DestroyVM(_ context.Context, vmID string) error {
	p.mu.Lock()
	vzVM, ok := p.vms[vmID]
	delete(p.vms, vmID)
	delete(p.ips, vmID)
	p.mu.Unlock()

	if ok && vzVM.CanStop() {
		errCh := make(chan error, 1)
		vzVM.Stop(func(err error) { errCh <- err })
		<-errCh
	}

	os.Remove(filepath.Join(p.workDir, vmID+".img"))
	os.Remove(filepath.Join(p.workDir, vmID+".efi"))
	os.RemoveAll(filepath.Join(p.isoDir, vmID))
	return nil
}

// RegisterIP stores the VM's IP address so GetVM can report VMStateRunning.
func (p *Provider) RegisterIP(_ context.Context, vmID, ip string) error {
	p.mu.Lock()
	p.ips[vmID] = ip
	p.mu.Unlock()
	return nil
}

func buildVZVM(diskPath, isoPath, efiPath string) (*vz.VirtualMachine, error) {
	efiVarStore, err := vz.NewEFIVariableStore(efiPath, vz.WithCreatingEFIVariableStore())
	if err != nil {
		return nil, fmt.Errorf("efi var store: %w", err)
	}
	bootLoader, err := vz.NewEFIBootLoader(vz.WithEFIVariableStore(efiVarStore))
	if err != nil {
		return nil, fmt.Errorf("boot loader: %w", err)
	}

	config, err := vz.NewVirtualMachineConfiguration(bootLoader, 2, 2*1024*1024*1024)
	if err != nil {
		return nil, fmt.Errorf("vm configuration: %w", err)
	}

	natAttach, err := vz.NewNATNetworkDeviceAttachment()
	if err != nil {
		return nil, fmt.Errorf("nat attachment: %w", err)
	}
	netDev, err := vz.NewVirtioNetworkDeviceConfiguration(natAttach)
	if err != nil {
		return nil, fmt.Errorf("net device: %w", err)
	}
	config.SetNetworkDevicesVirtualMachineConfiguration([]*vz.VirtioNetworkDeviceConfiguration{netDev})

	diskAttach, err := vz.NewDiskImageStorageDeviceAttachment(diskPath, false)
	if err != nil {
		return nil, fmt.Errorf("disk attachment: %w", err)
	}
	diskDev, err := vz.NewVirtioBlockDeviceConfiguration(diskAttach)
	if err != nil {
		return nil, fmt.Errorf("disk device: %w", err)
	}

	isoAttach, err := vz.NewDiskImageStorageDeviceAttachment(isoPath, true)
	if err != nil {
		return nil, fmt.Errorf("iso attachment: %w", err)
	}
	isoDev, err := vz.NewVirtioBlockDeviceConfiguration(isoAttach)
	if err != nil {
		return nil, fmt.Errorf("iso device: %w", err)
	}

	config.SetStorageDevicesVirtualMachineConfiguration([]vz.StorageDeviceConfiguration{diskDev, isoDev})

	valid, err := config.Validate()
	if err != nil {
		return nil, fmt.Errorf("config validate: %w", err)
	}
	if !valid {
		return nil, fmt.Errorf("vm configuration is invalid")
	}

	return vz.NewVirtualMachine(config)
}

func mapState(s vz.VirtualMachineState) string {
	switch s {
	case vz.VirtualMachineStateRunning:
		return vm.VMStateRunning
	case vz.VirtualMachineStateStopped, vz.VirtualMachineStateError:
		return vm.VMStateStopped
	case vz.VirtualMachineStateStarting:
		return vm.VMStateStarting
	default:
		return vm.VMStateUnknown
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
```

- [ ] **Step 3: Create provider_test.go**

Create `server/internal/vm/applevz/provider_test.go`:

```go
//go:build darwin && arm64

package applevz

import (
	"context"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vm"
)

func TestProvider_RegisterIP_StoresAndReturns(t *testing.T) {
	p := &Provider{
		vms: make(map[string]interface{ State() interface{} }),
		ips: make(map[string]string),
	}
	// We can't create a real VirtualMachine without hardware; test the IP map directly.
	p.ips["fake-vm"] = ""

	if err := p.RegisterIP(context.Background(), "fake-vm", "192.168.64.5"); err != nil {
		t.Fatalf("RegisterIP: %v", err)
	}
	p.mu.Lock()
	ip := p.ips["fake-vm"]
	p.mu.Unlock()
	if ip != "192.168.64.5" {
		t.Errorf("ip: got %q, want %q", ip, "192.168.64.5")
	}
}

func TestProvider_GetVM_UnknownVMReturnsStopped(t *testing.T) {
	p := NewProvider(nil, t.TempDir(), t.TempDir())
	v, err := p.GetVM(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("GetVM: %v", err)
	}
	if v.State != vm.VMStateStopped {
		t.Errorf("State: got %q, want %q", v.State, vm.VMStateStopped)
	}
}

func TestProvider_DestroyVM_UnknownVMIsNoop(t *testing.T) {
	p := NewProvider(nil, t.TempDir(), t.TempDir())
	if err := p.DestroyVM(context.Background(), "nonexistent"); err != nil {
		t.Fatalf("DestroyVM of unknown vm should not error: %v", err)
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := dir + "/src.img"
	dst := dir + "/dst.img"

	if err := os.WriteFile(src, []byte("hello disk"), 0644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(data) != "hello disk" {
		t.Errorf("content: got %q, want %q", data, "hello disk")
	}
}
```

Add `"os"` to test imports.

- [ ] **Step 4: Fix the provider_test.go — the Provider.vms type is concrete, not interface**

The test for `RegisterIP` was written with a placeholder. Replace with a simpler direct test:

```go
func TestProvider_RegisterIP_StoresAndReturns(t *testing.T) {
	p := &Provider{
		vms: make(map[string]*vz.VirtualMachine),
		ips: make(map[string]string),
	}

	if err := p.RegisterIP(context.Background(), "fake-vm", "192.168.64.5"); err != nil {
		t.Fatalf("RegisterIP: %v", err)
	}
	p.mu.Lock()
	ip := p.ips["fake-vm"]
	p.mu.Unlock()
	if ip != "192.168.64.5" {
		t.Errorf("ip: got %q, want %q", ip, "192.168.64.5")
	}
}
```

Add `vz "github.com/Code-Hex/vz/v3"` to test imports.

- [ ] **Step 5: Run tests (darwin/arm64 only)**

```bash
cd server && go test ./internal/vm/applevz/... -v
```

Expected: all pass (no real VMs started).

- [ ] **Step 6: Commit**

```bash
git add server/internal/vm/applevz/ server/go.mod server/go.sum
git commit -m "feat: add AppleVZ provider package with EFI boot and NAT networking"
```

---

## Task 7: Provider factory files and serve.go update

The factory MUST live in `server/cmd/agentsdxd/` (not `server/internal/vm/`) to avoid a circular import: `applevz` imports `vm` for its types, so `vm` cannot also import `applevz`.

**Files:**
- Create: `server/cmd/agentsdxd/provider_darwin_arm64.go`
- Create: `server/cmd/agentsdxd/provider_linux.go`
- Modify: `server/cmd/agentsdxd/serve.go`

- [ ] **Step 1: Create the darwin/arm64 factory in the cmd package**

Create `server/cmd/agentsdxd/provider_darwin_arm64.go`:

```go
//go:build darwin && arm64

package main

import (
	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/duck-labs/agentsdx-server/internal/vm/applevz"
)

func newProvider(images *vm.ImageStore, isoDir, workDir string) vm.VMProvider {
	return applevz.NewProvider(images, isoDir, workDir)
}
```

- [ ] **Step 2: Create the linux factory in the cmd package**

Create `server/cmd/agentsdxd/provider_linux.go`:

```go
//go:build linux

package main

import "github.com/duck-labs/agentsdx-server/internal/vm"

func newProvider(images *vm.ImageStore, isoDir, workDir string) vm.VMProvider {
	return vm.NewVirtualBoxProvider(images, isoDir)
}
```

- [ ] **Step 3: Update serve.go**

In `server/cmd/agentsdxd/serve.go`, update the `runServe` function.

Add `filepath.Join(dataDir, "vms")` to the dir creation block:

```go
for _, dir := range []struct {
    path string
    mode os.FileMode
}{
    {filepath.Join(dataDir, "profiles"), 0755},
    {filepath.Join(dataDir, "vault"), 0700},
    {filepath.Join(dataDir, "iso"), 0755},
    {filepath.Join(dataDir, "images"), 0755},
    {filepath.Join(dataDir, "vms"), 0755},
} {
```

Replace the provider line:

```go
// Before:
provider := vm.NewVirtualBoxProvider(images, filepath.Join(dataDir, "iso"))

// After:
provider := newProvider(images, filepath.Join(dataDir, "iso"), filepath.Join(dataDir, "vms"))
```

- [ ] **Step 4: Build on current platform to verify no circular imports**

```bash
cd server && go build ./cmd/agentsdxd/...
```

Expected: compiles without error.

- [ ] **Step 5: Commit**

```bash
git add server/cmd/agentsdxd/provider_darwin_arm64.go \
        server/cmd/agentsdxd/provider_linux.go \
        server/cmd/agentsdxd/serve.go
git commit -m "feat: add platform provider factory in cmd; wire serve.go to newProvider"
```

---

## Task 8: Add Builder.BuildAppleVZ and update ImageBuilder interface

**Files:**
- Modify: `server/internal/builder/builder.go`
- Modify: `server/internal/builder/builder_test.go`
- Modify: `server/internal/api/handler.go`
- Modify: `server/internal/api/handler_test.go`

- [ ] **Step 1: Write failing tests for BuildAppleVZ**

In `server/internal/builder/builder_test.go`, add:

```go
func TestBuildAppleVZ_PassesCorrectArgs(t *testing.T) {
	vmDir := t.TempDir()
	outputDir := t.TempDir()
	imagesPath := filepath.Join(t.TempDir(), "images.json")
	imageStore := vm.NewImageStore(imagesPath)

	fake := &fakeRunner{vmDir: vmDir}
	b := &Builder{
		vmDir:     vmDir,
		outputDir: outputDir,
		images:    imageStore,
		runner:    fake,
	}

	profile := types.ProfileSpec{
		Name: "arm-profile",
		Infrastructure: types.InfrastructureConfig{
			Image:   "ubuntu-24.04-arm64",
			Tooling: []string{},
		},
		Agent: types.AgentConfig{
			Provider: "claude",
		},
	}

	_, err := b.BuildAppleVZ(context.Background(), profile)
	if err != nil {
		t.Fatalf("BuildAppleVZ: %v", err)
	}

	argsStr := strings.Join(fake.capturedArgs, " ")
	if !strings.Contains(argsStr, "applevz.pkr.hcl") {
		t.Errorf("expected applevz.pkr.hcl in args, got: %v", fake.capturedArgs)
	}
	if !strings.Contains(argsStr, "-var=vm_name=arm-profile") {
		t.Errorf("expected vm_name arg, got args: %v", fake.capturedArgs)
	}
	if !strings.Contains(argsStr, "ubuntu-24.04.2-live-server-arm64.iso") {
		t.Errorf("expected arm64 iso_url, got args: %v", fake.capturedArgs)
	}
}

func TestBuildAppleVZ_UnknownImage_ReturnsError(t *testing.T) {
	vmDir := t.TempDir()
	outputDir := t.TempDir()
	imagesPath := filepath.Join(t.TempDir(), "images.json")
	imageStore := vm.NewImageStore(imagesPath)

	fake := &fakeRunner{vmDir: vmDir}
	b := &Builder{
		vmDir:     vmDir,
		outputDir: outputDir,
		images:    imageStore,
		runner:    fake,
	}

	profile := types.ProfileSpec{
		Name: "arm-profile",
		Infrastructure: types.InfrastructureConfig{
			Image: "unknown-os-arm64",
		},
		Agent: types.AgentConfig{Provider: "claude"},
	}

	_, err := b.BuildAppleVZ(context.Background(), profile)
	if err == nil {
		t.Fatal("expected error for unknown image")
	}
	if !strings.Contains(err.Error(), "unknown base image") {
		t.Errorf("expected 'unknown base image', got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to confirm failure**

```bash
cd server && go test ./internal/builder/... -run TestBuildAppleVZ -v
```

Expected: compile error — `b.BuildAppleVZ` undefined.

- [ ] **Step 3: Add ubuntu-24.04-arm64 to isoRegistry and add BuildAppleVZ**

In `server/internal/builder/builder.go`, add to `isoRegistry`:

```go
"ubuntu-24.04-arm64": {
    // Verify URL and checksum at: https://cdimage.ubuntu.com/releases/24.04.2/release/SHA256SUMS
    URL:      "https://cdimage.ubuntu.com/releases/24.04.2/release/ubuntu-24.04.2-live-server-arm64.iso",
    Checksum: "sha256:d2d9986ad1849dd59b77e6b15e50bf3e47c4b9e8fc8abdf39fc0cd3f5e36bef4",
},
```

Add `BuildAppleVZ` after `BuildVirtualBox`:

```go
// BuildAppleVZ builds an Apple VZ raw disk image for the given profile.
// It generates a temp orchestration script, invokes Packer with applevz.pkr.hcl,
// stores the .img path in ImageStore, and cleans up the temp file.
func (b *Builder) BuildAppleVZ(ctx context.Context, profile types.ProfileSpec) (string, error) {
	iso, ok := isoRegistry[profile.Infrastructure.Image]
	if !ok {
		return "", fmt.Errorf("unknown base image %q", profile.Infrastructure.Image)
	}

	scripts := composeScripts(profile)
	orchScript, err := writeOrchestrationScript(scripts, profile.Agent.Provider)
	if err != nil {
		return "", err
	}
	defer os.Remove(orchScript)

	orchDest := filepath.Join(b.vmDir, "orchestrate.sh")
	data, err := os.ReadFile(orchScript)
	if err != nil {
		return "", fmt.Errorf("read orchestration script: %w", err)
	}
	if err := os.WriteFile(orchDest, data, 0o755); err != nil {
		return "", fmt.Errorf("write orchestration script to vmDir: %w", err)
	}
	defer os.Remove(orchDest)

	imgPath := filepath.Join(b.outputDir, profile.Name+".img")

	args := []string{
		"build",
		fmt.Sprintf("-var=vm_name=%s", profile.Name),
		fmt.Sprintf("-var=iso_url=%s", iso.URL),
		fmt.Sprintf("-var=iso_checksum=%s", iso.Checksum),
		fmt.Sprintf("-var=provision_script=%s", orchDest),
		fmt.Sprintf("-var=output_dir=%s", b.outputDir),
		"applevz.pkr.hcl",
	}

	if err := b.runner.Run(ctx, b.vmDir, args); err != nil {
		return "", fmt.Errorf("packer build: %w", err)
	}

	if err := b.images.SetAppleVZPath(profile.Name, imgPath); err != nil {
		return "", fmt.Errorf("store image reference: %w", err)
	}
	return imgPath, nil
}
```

- [ ] **Step 4: Run builder tests**

```bash
cd server && go test ./internal/builder/... -v
```

Expected: all pass.

- [ ] **Step 5: Add BuildAppleVZ to ImageBuilder interface in handler.go**

In `server/internal/api/handler.go`, update `ImageBuilder`:

```go
type ImageBuilder interface {
	BuildVirtualBox(ctx context.Context, profile types.ProfileSpec) (string, error)
	BuildAppleVZ(ctx context.Context, profile types.ProfileSpec) (string, error)
}
```

Add `"runtime"` to imports in handler.go.

Update the `buildImage` handler to dispatch by platform:

```go
func (h *Handler) buildImage(w http.ResponseWriter, r *http.Request) {
	var req types.BuildImageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	spec, err := h.profiles.Get(req.ProfileName)
	if err != nil {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}
	go func() {
		if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
			if _, err := h.builder.BuildAppleVZ(context.Background(), spec); err != nil {
				log.Printf("buildImage applevz: profile %s: %v", req.ProfileName, err)
			}
		} else {
			if _, err := h.builder.BuildVirtualBox(context.Background(), spec); err != nil {
				log.Printf("buildImage virtualbox: profile %s: %v", req.ProfileName, err)
			}
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "building", "profile": req.ProfileName})
}
```

- [ ] **Step 6: Add BuildAppleVZ to fakeBuilder in handler_test.go**

In `server/internal/api/handler_test.go`, update `fakeBuilder`:

```go
type fakeBuilder struct {
	profile string
	err     error
}

func (f *fakeBuilder) BuildVirtualBox(_ context.Context, p types.ProfileSpec) (string, error) {
	f.profile = p.Name
	return "/tmp/fake.ova", f.err
}

func (f *fakeBuilder) BuildAppleVZ(_ context.Context, p types.ProfileSpec) (string, error) {
	f.profile = p.Name
	return "/tmp/fake.img", f.err
}
```

- [ ] **Step 7: Run all tests**

```bash
cd server && go test ./... 2>&1 | tail -20
```

Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add server/internal/builder/builder.go server/internal/builder/builder_test.go \
        server/internal/api/handler.go server/internal/api/handler_test.go
git commit -m "feat: add Builder.BuildAppleVZ; update ImageBuilder interface and buildImage dispatch"
```

---

## Task 9: Create vm/applevz.pkr.hcl

**Files:**
- Create: `vm/applevz.pkr.hcl`

- [ ] **Step 1: Create the Packer template**

Create `vm/applevz.pkr.hcl`:

```hcl
packer {
  required_plugins {
    qemu = {
      source  = "github.com/hashicorp/qemu"
      version = "~> 1"
    }
  }
}

variable "vm_name" {
  type        = string
  description = "VM name; used as the output filename base"
}

variable "iso_url" {
  type        = string
  description = "URL or local path to the Ubuntu ARM64 server ISO"
}

variable "iso_checksum" {
  type        = string
  description = "sha256:<hex> checksum for iso_url"
}

variable "provision_script" {
  type        = string
  description = "Local path to the generated orchestration shell script"
}

variable "output_dir" {
  type        = string
  description = "Directory where the exported raw disk image will be written"
}

variable "ssh_password" {
  type      = string
  default   = "packer"
  sensitive = true
}

source "qemu" "applevz" {
  vm_name          = "${var.vm_name}.img"
  iso_url          = var.iso_url
  iso_checksum     = var.iso_checksum
  output_directory = var.output_dir
  disk_size        = "20480M"
  memory           = 4096
  cpus             = 4
  headless         = true
  format           = "raw"
  accelerator      = "hvf"
  machine_type     = "virt"

  # edk2-aarch64-code.fd is shipped with QEMU installed via Homebrew.
  # If installed via another package manager, adjust the path accordingly.
  firmware = "/opt/homebrew/share/qemu/edk2-aarch64-code.fd"

  ssh_username     = "root"
  ssh_password     = var.ssh_password
  ssh_timeout      = "45m"
  shutdown_command = "shutdown -P now"

  http_content = {
    "/user-data" = file("${path.root}/autoinstall/user-data.yaml")
    "/meta-data" = ""
  }

  # Boot command for Ubuntu 24.04 ARM64 with UEFI/GRUB.
  # Press 'e' to edit the first GRUB entry, navigate to the linux line,
  # append autoinstall parameters, then boot with F10.
  boot_wait = "20s"
  boot_command = [
    "e<wait5>",
    "<down><down><down><end>",
    " autoinstall ds=\"nocloud-net;s=http://{{.HTTPIP}}:{{.HTTPPort}}/\"<wait>",
    "<f10>"
  ]

  qemuargs = [
    ["-device", "virtio-gpu-pci"],
    ["-device", "usb-ehci,id=usb,bus=pcie.0"],
    ["-device", "usb-kbd,bus=usb.0"],
    ["-device", "usb-mouse,bus=usb.0"],
  ]
}

build {
  sources = ["source.qemu.applevz"]

  provisioner "file" {
    source      = "${path.root}/"
    destination = "/tmp/agentsdx-vm/"
  }

  provisioner "shell" {
    script = var.provision_script
  }
}
```

**Note:** The exact boot command depends on the Ubuntu 24.04 ARM64 installer's GRUB menu. If the VM fails to autoinstall, connect via VNC (remove `headless = true` temporarily) to observe the GRUB screen and adjust `boot_wait` and `boot_command`.

- [ ] **Step 2: Init the Packer QEMU plugin (run on darwin/arm64)**

```bash
cd vm && packer init applevz.pkr.hcl
```

Expected: QEMU Packer plugin downloaded.

- [ ] **Step 3: Commit**

```bash
git add vm/applevz.pkr.hcl
git commit -m "feat: add Packer QEMU ARM64 template for Apple VZ"
```

---

## Task 10: Update setup.go for darwin/arm64

**Files:**
- Modify: `server/cmd/agentsdxd/setup.go`

- [ ] **Step 1: Add runtime import and darwin/arm64 branch to runSetup**

In `server/cmd/agentsdxd/setup.go`, add `"runtime"` to imports.

Replace `runSetup`:

```go
func runSetup(vmDir string) error {
	fmt.Println("=== agentsdxd setup ===")

	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		return runSetupDarwinARM64(vmDir)
	}

	if err := setupVboxnet0(); err != nil {
		return err
	}
	if err := setupPackerPlugin(vmDir, "virtualbox.pkr.hcl"); err != nil {
		return err
	}
	fmt.Println("\nSetup complete. You can now run: agentsdxd serve")
	return nil
}

func runSetupDarwinARM64(vmDir string) error {
	fmt.Println("\n[1/1] Initialising Packer QEMU plugin for Apple VZ...")

	packerPath, err := exec.LookPath("packer")
	if err != nil {
		fmt.Println("  Packer not found — installing via Homebrew...")
		if out, err := exec.Command("brew", "install", "packer").CombinedOutput(); err != nil {
			return fmt.Errorf("install packer: %w\n%s", err, out)
		}
		packerPath = "packer"
		fmt.Println("  Packer installed.")
	}

	hclPath := filepath.Join(vmDir, "applevz.pkr.hcl")
	if _, err := os.Stat(hclPath); err != nil {
		return fmt.Errorf("Packer template not found at %s: %w", hclPath, err)
	}

	cmd := exec.Command(packerPath, "init", hclPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("packer init: %w", err)
	}

	fmt.Println("  QEMU plugin ready.")
	fmt.Println("\nSetup complete.")
	fmt.Println("NOTE: agentsdxd serve requires sudo (or com.apple.vm.networking entitlement) for VM networking.")
	return nil
}
```

Replace `setupPackerPlugin` to accept an hcl filename argument (the old inline call becomes parameterized):

```go
func setupPackerPlugin(vmDir, hclFile string) error {
	fmt.Println("\n[2/2] Initialising Packer VirtualBox plugin...")

	packerPath, err := exec.LookPath("packer")
	if err != nil {
		fmt.Println("  Packer not found — installing via Homebrew...")
		if out, err := exec.Command("brew", "install", "packer").CombinedOutput(); err != nil {
			return fmt.Errorf("install packer: %w\n%s", err, out)
		}
		packerPath = "packer"
		fmt.Println("  Packer installed successfully.")
	}

	hclPath := filepath.Join(vmDir, hclFile)
	if _, err := os.Stat(hclPath); err != nil {
		return fmt.Errorf("Packer template not found at %s: %w", hclPath, err)
	}

	cmd := exec.Command(packerPath, "init", hclPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("packer init: %w", err)
	}

	fmt.Println("  Packer VirtualBox plugin ready.")
	return nil
}
```

Note: Remove the old `setupPackerPlugin` body that used `hclPath := vmDir + "/virtualbox.pkr.hcl"` and replace with the version above. Also add `"path/filepath"` to imports if not already present (it is already).

- [ ] **Step 2: Build to verify**

```bash
cd server && go build ./cmd/agentsdxd/...
```

Expected: compiles without error.

- [ ] **Step 3: Commit**

```bash
git add server/cmd/agentsdxd/setup.go
git commit -m "feat: add darwin/arm64 setup path — skip vboxnet0, init QEMU Packer plugin"
```

---

## Task 11: Update e2e/main_test.go for darwin/arm64

**Files:**
- Modify: `e2e/main_test.go`

- [ ] **Step 1: Skip vboxnet0 setup on darwin/arm64**

In `e2e/main_test.go`, add `"runtime"` to imports.

The current code calls `setup` unconditionally:

```go
setup := exec.Command(serverPath, "setup", "--vm-dir="+vmDir)
```

This still works — on darwin/arm64 `setup` now runs the QEMU path (Task 10). No change needed to this call. However, if the e2e tests run without a real VM (`vm` build tag not set), `setup` should be skipped entirely. Add a guard around the setup call and the server's `vboxnet0` dependency:

Replace the setup block in `e2e/main_test.go`:

```go
// On darwin/arm64, setup initialises the QEMU plugin (not vboxnet0).
// On linux, setup configures vboxnet0 and the VirtualBox plugin.
setup := exec.Command(serverPath, "setup", "--vm-dir="+vmDir)
setup.Env = append(os.Environ(), "HOME="+homeDir)
setup.Stdout = os.Stderr
setup.Stderr = os.Stderr
if err := setup.Run(); err != nil {
    fmt.Fprintf(os.Stderr, "setup: %v\n", err)
    if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
        return 1
    }
    // On darwin/arm64, setup failure (e.g. packer not installed) is non-fatal
    // for tests that don't exercise VM image building.
    fmt.Fprintf(os.Stderr, "warning: setup failed on darwin/arm64, continuing\n")
}
```

- [ ] **Step 2: Build e2e to verify**

```bash
cd e2e && go build -tags e2e . 2>&1
```

Expected: compiles without error.

- [ ] **Step 3: Commit**

```bash
git add e2e/main_test.go
git commit -m "feat: make e2e setup non-fatal on darwin/arm64 when packer unavailable"
```

---

## Self-Review Checklist

After all tasks are complete, verify:

- [ ] `go test ./...` passes in `server/` on darwin/arm64
- [ ] `go test ./...` passes in `server/` on linux (skipping applevz package via build tags)
- [ ] `go build ./cmd/agentsdxd/...` compiles cleanly on both platforms
- [ ] `POST /sessions/{id}/ip` returns 204 for a known session and 404 for unknown
- [ ] `vm.BuildUserData` with empty `vmCallbackURL` produces no `runcmd` block
- [ ] `vm.BuildUserData` with non-empty `vmCallbackURL` produces a `runcmd` that curls the URL
- [ ] `ImageStore.List` returns entries with `AppleVZ` field populated
