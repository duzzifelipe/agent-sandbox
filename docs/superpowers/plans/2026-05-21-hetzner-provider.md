# Hetzner Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace QEMU/Packer with Hetzner Cloud — sessions and image builds both run as `cx22` cloud servers managed via the hcloud Go SDK.

**Architecture:** `HetznerProvider` implements the existing `VMProvider` interface for session lifecycle and a new `ImageProvider` interface for image builds. The `Builder` SSH-provisions a temp Hetzner server, snapshots it, and stores the snapshot ID; sessions boot from that snapshot. `agentsdxd` runs locally and authenticates to Hetzner via `AGENTSDX_HCLOUD_TOKEN`.

**Tech Stack:** Go 1.21+, `github.com/hetznercloud/hcloud-go/v2/hcloud`, `golang.org/x/crypto/ssh` (already in go.mod), standard library `archive/tar` + `compress/gzip`

---

## File Map

| File | Action | Purpose |
|---|---|---|
| `server/go.mod` | modify | add hcloud-go/v2 |
| `server/internal/vm/provider.go` | modify | add `ImageProvider` interface; add `ImageID` to `CreateVMRequest` |
| `server/internal/vm/images.go` | modify | add `GetHetznerSnapshotID` / `SetHetznerSnapshotID` |
| `server/internal/vm/images_test.go` | modify | add tests for new methods |
| `server/internal/vm/userdata.go` | create | `BuildUserData` (moved from `nocloud.go`) |
| `server/internal/vm/userdata_test.go` | create | tests for `BuildUserData` (from `nocloud_test.go`) |
| `server/internal/vm/nocloud.go` | delete | replaced by `userdata.go` |
| `server/internal/vm/nocloud_test.go` | delete | replaced by `userdata_test.go` |
| `server/internal/vm/qemu.go` | delete | replaced by hetzner.go |
| `server/internal/vm/hetzner.go` | create | `HetznerProvider` implementing `VMProvider` + `ImageProvider` |
| `server/internal/vm/hetzner_test.go` | create | unit tests with fake hcloud clients |
| `server/internal/builder/builder.go` | rewrite | `Build` method; drop all Packer/QEMU code |
| `server/internal/builder/ssh.go` | create | SSH helpers: dial, uploadDir, uploadFile, runRemoteCommand |
| `server/internal/builder/builder_test.go` | rewrite | tests using fake `ImageProvider` and no-op provision |
| `server/internal/session/manager.go` | modify | add `*vm.ImageStore`; remove `ReportReady`; simplify `pollUntilRunning` |
| `server/internal/session/manager_test.go` | modify | pass fake image store to `NewManager` |
| `server/internal/api/handler.go` | modify | rename `ImageBuilder.BuildQEMU` → `Build`; remove `/ready` route |
| `server/internal/api/handler_test.go` | modify | update `fakeBuilder`; update `NewManager` call |
| `server/cmd/agentsdxd/serve.go` | modify | hcloud client + `HetznerProvider`; new env vars |
| `server/cmd/agentsdxd/setup.go` | rewrite | replace QEMU/Packer checks with hcloud token validation |
| `vm/qemu.pkr.hcl` | delete | no longer needed |

---

## Task 1 — Add hcloud-go dependency + extend `provider.go`

**Files:**
- Modify: `server/go.mod`
- Modify: `server/internal/vm/provider.go`

- [ ] **Step 1: Add hcloud-go/v2 dependency**

```bash
cd server && go get github.com/hetznercloud/hcloud-go/v2@latest
```

Expected: `go.mod` and `go.sum` updated with `github.com/hetznercloud/hcloud-go/v2`.

- [ ] **Step 2: Add `ImageProvider` interface and `ImageID` to `CreateVMRequest`**

Replace the entire contents of `server/internal/vm/provider.go`:

```go
package vm

import "context"

// VMProvider manages session VM lifecycle.
type VMProvider interface {
	CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error)
	DestroyVM(ctx context.Context, vmID string) error
	GetVM(ctx context.Context, vmID string) (*VM, error)
}

// ImageProvider manages image build VM lifecycle.
// CreateBuildVM blocks until the server is running and ready for SSH.
type ImageProvider interface {
	CreateBuildVM(ctx context.Context, baseImage, authorizedKey string) (*VM, error)
	SnapshotVM(ctx context.Context, vmID, snapshotName string) (string, error)
	DestroyBuildVM(ctx context.Context, vmID string) error
}

// CreateVMRequest is passed to VMProvider.CreateVM.
type CreateVMRequest struct {
	ProfileName   string
	ImageID       string // snapshot ID or base image name
	AuthorizedKey string // public key placed in authorized_keys
	UserData      string // cloud-init user-data
}

// VM is returned by VMProvider methods.
type VM struct {
	ID        string
	IPAddress string
	State     string
}

const (
	VMStateStarting = "starting"
	VMStateRunning  = "running"
	VMStateStopped  = "stopped"
	VMStateUnknown  = "unknown"
)
```

- [ ] **Step 3: Verify it compiles**

```bash
cd server && go build ./...
```

Expected: compile errors only from callers of `CreateVMRequest` that now need `ImageID` (will be fixed in later tasks). No errors inside `vm` package itself.

- [ ] **Step 4: Commit**

```bash
cd server && git add go.mod go.sum internal/vm/provider.go
git commit -m "feat: add ImageProvider interface and ImageID to CreateVMRequest"
```

---

## Task 2 — ImageStore Hetzner snapshot methods

**Files:**
- Modify: `server/internal/vm/images.go`
- Modify: `server/internal/vm/images_test.go`

- [ ] **Step 1: Write failing tests**

Add to the bottom of `server/internal/vm/images_test.go`:

```go
func TestImageStore_SetAndGetHetznerSnapshotID(t *testing.T) {
	dir := t.TempDir()
	store := vm.NewImageStore(filepath.Join(dir, "images.json"))

	if err := store.SetHetznerSnapshotID("my-profile", "98765"); err != nil {
		t.Fatalf("SetHetznerSnapshotID: %v", err)
	}

	got, err := store.GetHetznerSnapshotID("my-profile")
	if err != nil {
		t.Fatalf("GetHetznerSnapshotID: %v", err)
	}
	if got != "98765" {
		t.Errorf("got %q, want %q", got, "98765")
	}
}

func TestImageStore_GetHetznerSnapshotID_NotFound(t *testing.T) {
	dir := t.TempDir()
	store := vm.NewImageStore(filepath.Join(dir, "images.json"))

	_, err := store.GetHetznerSnapshotID("missing")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
}

func TestImageStore_GetHetznerSnapshotID_Empty(t *testing.T) {
	dir := t.TempDir()
	writeImagesJSON(t, dir, map[string]vm.ImageRecord{
		"no-snapshot": {vm.ProviderHetzner: ""},
	})
	store := vm.NewImageStore(filepath.Join(dir, "images.json"))

	_, err := store.GetHetznerSnapshotID("no-snapshot")
	if err == nil {
		t.Fatal("expected error for empty snapshot id")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd server && go test ./internal/vm/ -run TestImageStore_.*Hetzner -v
```

Expected: FAIL — `GetHetznerSnapshotID` and `SetHetznerSnapshotID` undefined.

- [ ] **Step 3: Implement the new methods**

Add to `server/internal/vm/images.go` (before the `load` method):

```go
// GetHetznerSnapshotID returns the Hetzner snapshot ID for profileName.
func (s *ImageStore) GetHetznerSnapshotID(profileName string) (string, error) {
	records, err := s.load()
	if os.IsNotExist(err) {
		return "", fmt.Errorf("no image built for profile %q: run 'images build' first", profileName)
	}
	if err != nil {
		return "", fmt.Errorf("load images: %w", err)
	}
	rec, ok := records[profileName]
	if !ok {
		return "", fmt.Errorf("no image record for profile %q", profileName)
	}
	id := rec[ProviderHetzner]
	if id == "" {
		return "", fmt.Errorf("no hetzner snapshot built for profile %q", profileName)
	}
	return id, nil
}

// SetHetznerSnapshotID writes or updates the Hetzner snapshot ID for profileName.
func (s *ImageStore) SetHetznerSnapshotID(profileName, snapshotID string) error {
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
	rec[ProviderHetzner] = snapshotID
	records[profileName] = rec
	return s.save(records)
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
cd server && go test ./internal/vm/ -run TestImageStore -v
```

Expected: all `TestImageStore_*` tests PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/vm/images.go server/internal/vm/images_test.go
git commit -m "feat: add GetHetznerSnapshotID and SetHetznerSnapshotID to ImageStore"
```

---

## Task 3 — Migrate `nocloud.go` → `userdata.go`

**Files:**
- Create: `server/internal/vm/userdata.go`
- Create: `server/internal/vm/userdata_test.go`
- Delete: `server/internal/vm/nocloud.go`
- Delete: `server/internal/vm/nocloud_test.go`

- [ ] **Step 1: Create `userdata.go`**

Create `server/internal/vm/userdata.go` with the contents of the `BuildUserData` function (the only thing we need from `nocloud.go`):

```go
package vm

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// BuildUserData returns cloud-init user-data that injects SSH keys, agent env,
// and a callback to report the VM IP to the server on first boot.
func BuildUserData(authorizedKey, gitPrivateKey, sessionID, serverURL, profileName string) string {
	encodedKey := base64.StdEncoding.EncodeToString([]byte(gitPrivateKey))
	return fmt.Sprintf(`#cloud-config
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
runcmd:
  - IP=$(ip route get 1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src") print $(i+1)}' | head -1)
  - curl -sf -X POST "%s/sessions/%s/ready" -H "Content-Type: application/json" -d "{\"ip_address\":\"$IP\"}" || true
`, authorizedKey, encodedKey, serverURL, sessionID, profileName, serverURL, sessionID)
}
```

- [ ] **Step 2: Create `userdata_test.go`**

Create `server/internal/vm/userdata_test.go`:

```go
package vm_test

import (
	"strings"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vm"
)

func TestBuildUserData_ContainsSSHKey(t *testing.T) {
	ud := vm.BuildUserData("ssh-rsa AAAA...", "-----BEGIN OPENSSH PRIVATE KEY-----\nABC\n-----END OPENSSH PRIVATE KEY-----", "sess-1", "http://server:8080", "myprofile")
	if !strings.Contains(ud, "ssh-rsa AAAA...") {
		t.Errorf("user-data missing authorized key")
	}
	if !strings.Contains(ud, "/root/.ssh/id_rsa") {
		t.Errorf("user-data missing id_rsa write_files entry")
	}
}

func TestBuildUserData_ContainsEnvFile(t *testing.T) {
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-42", "http://server:8080", "work-backend")
	if !strings.Contains(ud, "/etc/agentsdx.env") {
		t.Errorf("user-data missing agentsdx.env write_files entry")
	}
	if !strings.Contains(ud, "AGENTSDX_SERVER_URL=http://server:8080") {
		t.Errorf("user-data missing AGENTSDX_SERVER_URL")
	}
	if !strings.Contains(ud, "AGENTSDX_SESSION_ID=sess-42") {
		t.Errorf("user-data missing AGENTSDX_SESSION_ID")
	}
	if !strings.Contains(ud, "AGENTSDX_PROFILE=work-backend") {
		t.Errorf("user-data missing AGENTSDX_PROFILE")
	}
}
```

- [ ] **Step 3: Run new tests**

```bash
cd server && go test ./internal/vm/ -run TestBuildUserData -v
```

Expected: PASS.

- [ ] **Step 4: Delete old files**

```bash
rm server/internal/vm/nocloud.go server/internal/vm/nocloud_test.go
```

- [ ] **Step 5: Drop `iso9660` from go.mod**

```bash
cd server && go mod tidy
```

Expected: `github.com/kdomanski/iso9660` removed from `go.mod` and `go.sum`.

- [ ] **Step 6: Verify compile**

```bash
cd server && go build ./...
```

Expected: no errors from `vm` package. (Other packages may fail if they referenced `nocloud.go` exports — `WriteNoCloudISO` was only used internally in `qemu.go` which is still present, so this should be clean.)

- [ ] **Step 7: Commit**

```bash
git add server/internal/vm/userdata.go server/internal/vm/userdata_test.go
git rm server/internal/vm/nocloud.go server/internal/vm/nocloud_test.go
git add server/go.mod server/go.sum
git commit -m "refactor: migrate BuildUserData to userdata.go, drop iso9660"
```

---

## Task 4 — Implement `HetznerProvider`

**Files:**
- Create: `server/internal/vm/hetzner.go`
- Create: `server/internal/vm/hetzner_test.go`

- [ ] **Step 1: Write failing tests**

Create `server/internal/vm/hetzner_test.go`:

```go
package vm_test

import (
	"context"
	"net"
	"strconv"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// --- fake hcloud clients ---

type fakeServerClient struct {
	createResult hcloud.ServerCreateResult
	createErr    error
	getServer    *hcloud.Server
	getErr       error
	deleted      []*hcloud.Server
	imageResult  hcloud.ServerCreateImageResult
	imageErr     error
}

func (f *fakeServerClient) Create(_ context.Context, _ hcloud.ServerCreateOpts) (hcloud.ServerCreateResult, *hcloud.Response, error) {
	return f.createResult, nil, f.createErr
}

func (f *fakeServerClient) GetByID(_ context.Context, _ int64) (*hcloud.Server, *hcloud.Response, error) {
	return f.getServer, nil, f.getErr
}

func (f *fakeServerClient) Delete(_ context.Context, s *hcloud.Server) ([]*hcloud.Action, *hcloud.Response, error) {
	f.deleted = append(f.deleted, s)
	return nil, nil, nil
}

func (f *fakeServerClient) CreateImage(_ context.Context, _ *hcloud.Server, _ *hcloud.ServerCreateImageOpts) (hcloud.ServerCreateImageResult, *hcloud.Response, error) {
	return f.imageResult, nil, f.imageErr
}

type fakeSSHKeyClient struct {
	created  *hcloud.SSHKey
	createErr error
	byName   *hcloud.SSHKey
	deleted  []*hcloud.SSHKey
}

func (f *fakeSSHKeyClient) Create(_ context.Context, opts hcloud.SSHKeyCreateOpts) (*hcloud.SSHKey, *hcloud.Response, error) {
	if f.createErr != nil {
		return nil, nil, f.createErr
	}
	f.created = &hcloud.SSHKey{ID: 1, Name: opts.Name}
	return f.created, nil, nil
}

func (f *fakeSSHKeyClient) Delete(_ context.Context, k *hcloud.SSHKey) (*hcloud.Response, error) {
	f.deleted = append(f.deleted, k)
	return nil, nil
}

func (f *fakeSSHKeyClient) GetByName(_ context.Context, name string) (*hcloud.SSHKey, *hcloud.Response, error) {
	if f.byName != nil && f.byName.Name == name {
		return f.byName, nil, nil
	}
	return nil, nil, nil
}

type fakeActionClient struct{ err error }

func (f *fakeActionClient) WaitFor(_ context.Context, _ ...*hcloud.Action) error {
	return f.err
}

func newTestProvider(servers *fakeServerClient, keys *fakeSSHKeyClient, actions *fakeActionClient) *vm.HetznerProvider {
	return vm.NewHetznerProviderFromClients(servers, keys, actions, "nbg1")
}

// --- tests ---

func TestHetznerProvider_CreateVM_ReturnsStartingWithIP(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	servers := &fakeServerClient{
		createResult: hcloud.ServerCreateResult{
			Server: &hcloud.Server{
				ID:     42,
				Status: hcloud.ServerStatusStarting,
				PublicNet: hcloud.ServerPublicNet{
					IPv4: hcloud.ServerPublicNetIPv4{IP: ip},
				},
				Labels: map[string]string{},
			},
			Action: &hcloud.Action{},
		},
	}
	p := newTestProvider(servers, &fakeSSHKeyClient{}, &fakeActionClient{})

	v, err := p.CreateVM(context.Background(), vm.CreateVMRequest{
		ProfileName:   "dev",
		ImageID:       "99",
		AuthorizedKey: "ssh-ed25519 AAAA",
		UserData:      "#cloud-config\n",
	})

	if err != nil {
		t.Fatalf("CreateVM: %v", err)
	}
	if v.ID != "42" {
		t.Errorf("ID: got %q, want %q", v.ID, "42")
	}
	if v.IPAddress != "1.2.3.4" {
		t.Errorf("IPAddress: got %q, want %q", v.IPAddress, "1.2.3.4")
	}
	if v.State != vm.VMStateStarting {
		t.Errorf("State: got %q, want %q", v.State, vm.VMStateStarting)
	}
}

func TestHetznerProvider_GetVM_MapsStatus(t *testing.T) {
	ip := net.ParseIP("5.6.7.8")
	servers := &fakeServerClient{
		getServer: &hcloud.Server{
			ID:     10,
			Status: hcloud.ServerStatusRunning,
			PublicNet: hcloud.ServerPublicNet{
				IPv4: hcloud.ServerPublicNetIPv4{IP: ip},
			},
		},
	}
	p := newTestProvider(servers, &fakeSSHKeyClient{}, &fakeActionClient{})

	v, err := p.GetVM(context.Background(), "10")
	if err != nil {
		t.Fatalf("GetVM: %v", err)
	}
	if v.State != vm.VMStateRunning {
		t.Errorf("State: got %q, want %q", v.State, vm.VMStateRunning)
	}
	if v.IPAddress != "5.6.7.8" {
		t.Errorf("IPAddress: got %q, want %q", v.IPAddress, "5.6.7.8")
	}
}

func TestHetznerProvider_GetVM_UnknownID_ReturnsUnknown(t *testing.T) {
	p := newTestProvider(&fakeServerClient{}, &fakeSSHKeyClient{}, &fakeActionClient{})

	v, err := p.GetVM(context.Background(), "not-a-number")
	if err != nil {
		t.Fatalf("GetVM: %v", err)
	}
	if v.State != vm.VMStateUnknown {
		t.Errorf("State: got %q, want %q", v.State, vm.VMStateUnknown)
	}
}

func TestHetznerProvider_DestroyVM_DeletesServerAndKey(t *testing.T) {
	servers := &fakeServerClient{
		getServer: &hcloud.Server{
			ID:     7,
			Labels: map[string]string{"agentsdx-sshkey": "agentsdx-session-7"},
		},
	}
	keys := &fakeSSHKeyClient{
		byName: &hcloud.SSHKey{ID: 3, Name: "agentsdx-session-7"},
	}
	p := newTestProvider(servers, keys, &fakeActionClient{})

	if err := p.DestroyVM(context.Background(), "7"); err != nil {
		t.Fatalf("DestroyVM: %v", err)
	}
	if len(servers.deleted) != 1 {
		t.Errorf("expected 1 server deleted, got %d", len(servers.deleted))
	}
	if len(keys.deleted) != 1 {
		t.Errorf("expected 1 ssh key deleted, got %d", len(keys.deleted))
	}
}

func TestHetznerProvider_CreateBuildVM_ReturnsRunningVM(t *testing.T) {
	ip := net.ParseIP("9.9.9.9")
	servers := &fakeServerClient{
		createResult: hcloud.ServerCreateResult{
			Server: &hcloud.Server{
				ID:     55,
				Status: hcloud.ServerStatusRunning,
				PublicNet: hcloud.ServerPublicNet{
					IPv4: hcloud.ServerPublicNetIPv4{IP: ip},
				},
				Labels: map[string]string{},
			},
			Action: &hcloud.Action{},
		},
	}
	p := newTestProvider(servers, &fakeSSHKeyClient{}, &fakeActionClient{})

	v, err := p.CreateBuildVM(context.Background(), "ubuntu-24.04", "ssh-ed25519 AAAA")
	if err != nil {
		t.Fatalf("CreateBuildVM: %v", err)
	}
	if v.IPAddress != "9.9.9.9" {
		t.Errorf("IPAddress: got %q, want %q", v.IPAddress, "9.9.9.9")
	}
	if v.State != vm.VMStateRunning {
		t.Errorf("State: got %q, want %q", v.State, vm.VMStateRunning)
	}
}

func TestHetznerProvider_SnapshotVM_ReturnsImageID(t *testing.T) {
	imageID := int64(777)
	servers := &fakeServerClient{
		imageResult: hcloud.ServerCreateImageResult{
			Image:  &hcloud.Image{ID: imageID},
			Action: &hcloud.Action{},
		},
	}
	p := newTestProvider(servers, &fakeSSHKeyClient{}, &fakeActionClient{})

	got, err := p.SnapshotVM(context.Background(), "55", "my-profile")
	if err != nil {
		t.Fatalf("SnapshotVM: %v", err)
	}
	if got != strconv.FormatInt(imageID, 10) {
		t.Errorf("snapshotID: got %q, want %q", got, strconv.FormatInt(imageID, 10))
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd server && go test ./internal/vm/ -run TestHetznerProvider -v
```

Expected: FAIL — `HetznerProvider`, `NewHetznerProviderFromClients` undefined.

- [ ] **Step 3: Implement `hetzner.go`**

Create `server/internal/vm/hetzner.go`:

```go
package vm

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

const hetznerServerType = "cx22"

// hcloudServerOps is a narrow interface over hcloud.ServerClient for testing.
type hcloudServerOps interface {
	Create(ctx context.Context, opts hcloud.ServerCreateOpts) (hcloud.ServerCreateResult, *hcloud.Response, error)
	GetByID(ctx context.Context, id int64) (*hcloud.Server, *hcloud.Response, error)
	Delete(ctx context.Context, server *hcloud.Server) ([]*hcloud.Action, *hcloud.Response, error)
	CreateImage(ctx context.Context, server *hcloud.Server, opts *hcloud.ServerCreateImageOpts) (hcloud.ServerCreateImageResult, *hcloud.Response, error)
}

// hcloudSSHKeyOps is a narrow interface over hcloud.SSHKeyClient for testing.
type hcloudSSHKeyOps interface {
	Create(ctx context.Context, opts hcloud.SSHKeyCreateOpts) (*hcloud.SSHKey, *hcloud.Response, error)
	Delete(ctx context.Context, sshKey *hcloud.SSHKey) (*hcloud.Response, error)
	GetByName(ctx context.Context, name string) (*hcloud.SSHKey, *hcloud.Response, error)
}

// hcloudActionOps is a narrow interface over hcloud.ActionClient for testing.
type hcloudActionOps interface {
	WaitFor(ctx context.Context, actions ...*hcloud.Action) error
}

// HetznerProvider implements VMProvider and ImageProvider using Hetzner Cloud.
type HetznerProvider struct {
	servers  hcloudServerOps
	sshKeys  hcloudSSHKeyOps
	actions  hcloudActionOps
	location string
}

// NewHetznerProvider creates a HetznerProvider from a hcloud.Client.
func NewHetznerProvider(client *hcloud.Client, location string) *HetznerProvider {
	if location == "" {
		location = "nbg1"
	}
	return &HetznerProvider{
		servers:  client.Server,
		sshKeys:  client.SSHKey,
		actions:  client.Action,
		location: location,
	}
}

// NewHetznerProviderFromClients creates a HetznerProvider from narrow interfaces (for testing).
func NewHetznerProviderFromClients(servers hcloudServerOps, sshKeys hcloudSSHKeyOps, actions hcloudActionOps, location string) *HetznerProvider {
	return &HetznerProvider{servers: servers, sshKeys: sshKeys, actions: actions, location: location}
}

// CreateVM creates a session server from the snapshot identified by req.ImageID.
func (p *HetznerProvider) CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error) {
	keyName := fmt.Sprintf("agentsdx-session-%d", time.Now().UnixMilli())
	sshKey, _, err := p.sshKeys.Create(ctx, hcloud.SSHKeyCreateOpts{
		Name:      keyName,
		PublicKey: req.AuthorizedKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create ssh key: %w", err)
	}

	result, _, err := p.servers.Create(ctx, hcloud.ServerCreateOpts{
		Name:       fmt.Sprintf("agentsdx-session-%d", time.Now().UnixMilli()),
		ServerType: &hcloud.ServerType{Name: hetznerServerType},
		Image:      imageRef(req.ImageID),
		Location:   &hcloud.Location{Name: p.location},
		SSHKeys:    []*hcloud.SSHKey{sshKey},
		UserData:   req.UserData,
		Labels:     map[string]string{"agentsdx-type": "session", "agentsdx-sshkey": keyName},
	})
	if err != nil {
		_, _ = p.sshKeys.Delete(ctx, sshKey)
		return nil, fmt.Errorf("create server: %w", err)
	}

	return &VM{
		ID:        strconv.FormatInt(result.Server.ID, 10),
		IPAddress: result.Server.PublicNet.IPv4.IP.String(),
		State:     VMStateStarting,
	}, nil
}

// GetVM fetches server status and maps it to VM state.
func (p *HetznerProvider) GetVM(ctx context.Context, vmID string) (*VM, error) {
	id, err := strconv.ParseInt(vmID, 10, 64)
	if err != nil {
		return &VM{ID: vmID, State: VMStateUnknown}, nil
	}
	server, _, err := p.servers.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get server: %w", err)
	}
	if server == nil {
		return &VM{ID: vmID, State: VMStateUnknown}, nil
	}

	state := VMStateStarting
	switch server.Status {
	case hcloud.ServerStatusRunning:
		state = VMStateRunning
	case hcloud.ServerStatusOff, hcloud.ServerStatusDeleting:
		state = VMStateStopped
	}

	ip := ""
	if server.PublicNet.IPv4.IP != nil {
		ip = server.PublicNet.IPv4.IP.String()
	}
	return &VM{ID: vmID, IPAddress: ip, State: state}, nil
}

// DestroyVM deletes the server and its associated SSH key.
func (p *HetznerProvider) DestroyVM(ctx context.Context, vmID string) error {
	return p.deleteServerAndKey(ctx, vmID)
}

// CreateBuildVM creates a temporary server for image provisioning.
// Blocks until hcloud reports the server as started.
func (p *HetznerProvider) CreateBuildVM(ctx context.Context, baseImage, authorizedKey string) (*VM, error) {
	keyName := fmt.Sprintf("agentsdx-build-%d", time.Now().UnixMilli())
	sshKey, _, err := p.sshKeys.Create(ctx, hcloud.SSHKeyCreateOpts{
		Name:      keyName,
		PublicKey: authorizedKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create ssh key: %w", err)
	}

	result, _, err := p.servers.Create(ctx, hcloud.ServerCreateOpts{
		Name:       fmt.Sprintf("agentsdx-build-%d", time.Now().UnixMilli()),
		ServerType: &hcloud.ServerType{Name: hetznerServerType},
		Image:      imageRef(baseImage),
		Location:   &hcloud.Location{Name: p.location},
		SSHKeys:    []*hcloud.SSHKey{sshKey},
		Labels:     map[string]string{"agentsdx-type": "build", "agentsdx-sshkey": keyName},
	})
	if err != nil {
		_, _ = p.sshKeys.Delete(ctx, sshKey)
		return nil, fmt.Errorf("create build server: %w", err)
	}

	allActions := []*hcloud.Action{result.Action}
	allActions = append(allActions, result.NextActions...)
	if err := p.actions.WaitFor(ctx, allActions...); err != nil {
		_, _ = p.servers.Delete(ctx, result.Server)
		_, _ = p.sshKeys.Delete(ctx, sshKey)
		return nil, fmt.Errorf("wait for build server: %w", err)
	}

	return &VM{
		ID:        strconv.FormatInt(result.Server.ID, 10),
		IPAddress: result.Server.PublicNet.IPv4.IP.String(),
		State:     VMStateRunning,
	}, nil
}

// SnapshotVM takes a snapshot of the server and returns the snapshot image ID.
func (p *HetznerProvider) SnapshotVM(ctx context.Context, vmID, snapshotName string) (string, error) {
	id, err := strconv.ParseInt(vmID, 10, 64)
	if err != nil {
		return "", fmt.Errorf("parse vm id: %w", err)
	}
	desc := snapshotName
	result, _, err := p.servers.CreateImage(ctx, &hcloud.Server{ID: id}, &hcloud.ServerCreateImageOpts{
		Type:        hcloud.ImageTypeSnapshot,
		Description: &desc,
		Labels:      map[string]string{"agentsdx-profile": snapshotName},
	})
	if err != nil {
		return "", fmt.Errorf("create snapshot: %w", err)
	}
	if err := p.actions.WaitFor(ctx, result.Action); err != nil {
		return "", fmt.Errorf("wait for snapshot: %w", err)
	}
	return strconv.FormatInt(result.Image.ID, 10), nil
}

// DestroyBuildVM deletes a build server and its SSH key.
func (p *HetznerProvider) DestroyBuildVM(ctx context.Context, vmID string) error {
	return p.deleteServerAndKey(ctx, vmID)
}

func (p *HetznerProvider) deleteServerAndKey(ctx context.Context, vmID string) error {
	id, err := strconv.ParseInt(vmID, 10, 64)
	if err != nil {
		return nil
	}
	server, _, err := p.servers.GetByID(ctx, id)
	if err != nil || server == nil {
		return nil
	}
	sshKeyName := server.Labels["agentsdx-sshkey"]
	_, _ = p.servers.Delete(ctx, server)
	if sshKeyName != "" {
		if key, _, _ := p.sshKeys.GetByName(ctx, sshKeyName); key != nil {
			_, _ = p.sshKeys.Delete(ctx, key)
		}
	}
	return nil
}

// imageRef builds an hcloud.Image reference from an ID string (numeric = snapshot)
// or name string (e.g. "ubuntu-24.04" = public image).
func imageRef(imageID string) *hcloud.Image {
	if id, err := strconv.ParseInt(imageID, 10, 64); err == nil {
		return &hcloud.Image{ID: id}
	}
	return &hcloud.Image{Name: imageID}
}
```

- [ ] **Step 4: Run tests**

```bash
cd server && go test ./internal/vm/ -run TestHetznerProvider -v
```

Expected: all `TestHetznerProvider_*` tests PASS.

- [ ] **Step 5: Delete `qemu.go`**

```bash
rm server/internal/vm/qemu.go
```

- [ ] **Step 6: Verify compile**

```bash
cd server && go build ./...
```

Expected: errors only in `builder/` (still references `BuildQEMU`) and `cmd/agentsdxd/` — fixed in upcoming tasks.

- [ ] **Step 7: Commit**

```bash
git add server/internal/vm/hetzner.go server/internal/vm/hetzner_test.go
git rm server/internal/vm/qemu.go
git commit -m "feat: implement HetznerProvider (VMProvider + ImageProvider)"
```

---

## Task 5 — Rewrite `builder.go` with SSH provisioning

**Files:**
- Create: `server/internal/builder/ssh.go`
- Rewrite: `server/internal/builder/builder.go`
- Rewrite: `server/internal/builder/builder_test.go`

- [ ] **Step 1: Create `ssh.go`**

Create `server/internal/builder/ssh.go`:

```go
package builder

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshConn is a narrow interface over *ssh.Client used by provisioning helpers.
type sshConn interface {
	NewSession() (*ssh.Session, error)
	Close() error
}

// dialSSHWithRetry dials addr (host:port) retrying every 5s until ctx is cancelled.
func dialSSHWithRetry(ctx context.Context, addr, privKey string) (sshConn, error) {
	signer, err := ssh.ParsePrivateKey([]byte(privKey))
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	cfg := &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec — ephemeral build VM
		Timeout:         10 * time.Second,
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var lastErr error
	for {
		c, err := ssh.Dial("tcp", addr, cfg)
		if err == nil {
			return c, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out connecting to %s: %v", addr, lastErr)
		case <-ticker.C:
		}
	}
}

// uploadDir tars localDir and extracts it into /tmp/agentsdx-vm/ on the remote.
func uploadDir(conn sshConn, localDir string) error {
	session, err := conn.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	pr, pw := io.Pipe()
	session.Stdin = pr
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	errCh := make(chan error, 1)
	go func() {
		gw := gzip.NewWriter(pw)
		tw := tar.NewWriter(gw)
		walkErr := filepath.WalkDir(localDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(localDir, path)
			if rel == "." {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			hdr, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			hdr.Name = rel
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			if !d.IsDir() {
				f, err := os.Open(path)
				if err != nil {
					return err
				}
				defer f.Close()
				_, err = io.Copy(tw, f)
				return err
			}
			return nil
		})
		tw.Close()
		gw.Close()
		pw.CloseWithError(walkErr)
		errCh <- walkErr
	}()

	if err := session.Run("mkdir -p /tmp/agentsdx-vm && tar -xzf - -C /tmp/agentsdx-vm"); err != nil {
		return fmt.Errorf("extract dir on remote: %w", err)
	}
	return <-errCh
}

// uploadFile uploads the contents of localPath to remotePath and marks it executable.
func uploadFile(conn sshConn, localPath, remotePath string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read local file: %w", err)
	}
	session, err := conn.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer session.Close()
	session.Stdin = bytes.NewReader(data)
	return session.Run(fmt.Sprintf("cat > %s && chmod +x %s", remotePath, remotePath))
}

// runRemoteCommand runs cmd on the remote, streaming stdout/stderr to the local process.
func runRemoteCommand(conn sshConn, cmd string) error {
	session, err := conn.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer session.Close()
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr
	return session.Run(cmd)
}
```

- [ ] **Step 2: Rewrite `builder.go`**

Replace the entire contents of `server/internal/builder/builder.go`:

```go
package builder

import (
	"context"
	"fmt"
	"log"
	"os"
	"text/template"

	"github.com/duck-labs/agentsdx-server/internal/vault"
	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/duck-labs/agentsdx-shared/types"
)

// Builder orchestrates Hetzner image builds.
type Builder struct {
	vmDir    string
	images   *vm.ImageStore
	provider vm.ImageProvider
	// provision is injectable for testing; defaults to sshProvision.
	provision func(ctx context.Context, ip, privKey, vmDir, orchScriptPath string) error
}

// New creates a Builder.
func New(vmDir string, images *vm.ImageStore, provider vm.ImageProvider) *Builder {
	b := &Builder{vmDir: vmDir, images: images, provider: provider}
	b.provision = b.sshProvision
	return b
}

// Build provisions a Hetzner snapshot for the given profile and returns the snapshot ID.
func (b *Builder) Build(ctx context.Context, profile types.ProfileSpec) (string, error) {
	privKey, pubKey, err := vault.GenerateKeyPair()
	if err != nil {
		return "", fmt.Errorf("generate key pair: %w", err)
	}

	buildVM, err := b.provider.CreateBuildVM(ctx, profile.Infrastructure.Image, pubKey)
	if err != nil {
		return "", fmt.Errorf("create build vm: %w", err)
	}

	scripts := composeScripts(profile)
	orchScript, err := writeOrchestrationScript(scripts, profile.Agent.Provider)
	if err != nil {
		_ = b.provider.DestroyBuildVM(ctx, buildVM.ID)
		return "", fmt.Errorf("write orchestration script: %w", err)
	}
	defer os.Remove(orchScript)

	log.Printf("provisioning profile %s on %s", profile.Name, buildVM.IPAddress)
	if err := b.provision(ctx, buildVM.IPAddress, privKey, b.vmDir, orchScript); err != nil {
		_ = b.provider.DestroyBuildVM(ctx, buildVM.ID)
		return "", fmt.Errorf("provision: %w", err)
	}

	snapshotID, err := b.provider.SnapshotVM(ctx, buildVM.ID, profile.Name)
	if err != nil {
		_ = b.provider.DestroyBuildVM(ctx, buildVM.ID)
		return "", fmt.Errorf("snapshot vm: %w", err)
	}

	if err := b.images.SetHetznerSnapshotID(profile.Name, snapshotID); err != nil {
		_ = b.provider.DestroyBuildVM(ctx, buildVM.ID)
		return "", fmt.Errorf("store snapshot id: %w", err)
	}

	_ = b.provider.DestroyBuildVM(ctx, buildVM.ID)
	log.Printf("build complete for profile %s: snapshot %s", profile.Name, snapshotID)
	return snapshotID, nil
}

func (b *Builder) sshProvision(ctx context.Context, ip, privKey, vmDir, orchScriptPath string) error {
	conn, err := dialSSHWithRetry(ctx, ip+":22", privKey)
	if err != nil {
		return fmt.Errorf("dial ssh: %w", err)
	}
	defer conn.Close()
	if err := uploadDir(conn, vmDir); err != nil {
		return fmt.Errorf("upload vm dir: %w", err)
	}
	if err := uploadFile(conn, orchScriptPath, "/tmp/agentsdx-orchestrate.sh"); err != nil {
		return fmt.Errorf("upload orchestration script: %w", err)
	}
	return runRemoteCommand(conn, "/tmp/agentsdx-orchestrate.sh")
}

// composeScripts returns the ordered list of in-VM provisioning script paths for a profile.
func composeScripts(profile types.ProfileSpec) []string {
	scripts := []string{"/tmp/agentsdx-vm/base/provision.sh"}
	for _, tool := range profile.Infrastructure.Tooling {
		scripts = append(scripts, fmt.Sprintf("/tmp/agentsdx-vm/tooling/%s/provision.sh", tool))
	}
	scripts = append(scripts, fmt.Sprintf("/tmp/agentsdx-vm/agents/%s/provision.sh", profile.Agent.Provider))
	return scripts
}

const orchestrationTpl = `#!/bin/bash
set -euo pipefail
{{range .Scripts}}
bash "{{.}}"
{{end}}
cp "/tmp/agentsdx-vm/agents/{{.Agent}}/entrypoint.sh" /usr/local/bin/entrypoint.sh
cp /tmp/agentsdx-vm/vault-sync.sh /usr/local/bin/vault-sync.sh
chmod +x /usr/local/bin/entrypoint.sh /usr/local/bin/vault-sync.sh
`

// writeOrchestrationScript writes a temp bash script that runs provision scripts in order.
// Caller must delete the returned file path.
func writeOrchestrationScript(scripts []string, agentProvider string) (string, error) {
	f, err := os.CreateTemp("", "agentsdx-orchestrate-*.sh")
	if err != nil {
		return "", fmt.Errorf("create temp script: %w", err)
	}
	defer f.Close()

	tmpl, err := template.New("orch").Parse(orchestrationTpl)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	data := struct {
		Scripts []string
		Agent   string
	}{Scripts: scripts, Agent: agentProvider}
	if err := tmpl.Execute(f, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	if err := os.Chmod(f.Name(), 0o755); err != nil {
		return "", fmt.Errorf("chmod script: %w", err)
	}
	return f.Name(), nil
}
```

- [ ] **Step 3: Rewrite `builder_test.go`**

Replace the entire contents of `server/internal/builder/builder_test.go`:

```go
package builder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/duck-labs/agentsdx-shared/types"
)

// fakeImageProvider is a test double for vm.ImageProvider.
type fakeImageProvider struct {
	buildVM      *vm.VM
	buildErr     error
	snapshotID   string
	snapshotErr  error
	destroyErr   error
	destroyedIDs []string
}

func (f *fakeImageProvider) CreateBuildVM(_ context.Context, _, _ string) (*vm.VM, error) {
	if f.buildErr != nil {
		return nil, f.buildErr
	}
	return f.buildVM, nil
}

func (f *fakeImageProvider) SnapshotVM(_ context.Context, vmID, _ string) (string, error) {
	if f.snapshotErr != nil {
		return "", f.snapshotErr
	}
	return f.snapshotID, nil
}

func (f *fakeImageProvider) DestroyBuildVM(_ context.Context, vmID string) error {
	f.destroyedIDs = append(f.destroyedIDs, vmID)
	return f.destroyErr
}

func testBuilder(t *testing.T, provider *fakeImageProvider) *Builder {
	t.Helper()
	vmDir := t.TempDir()
	imagesPath := filepath.Join(t.TempDir(), "images.json")
	b := New(vmDir, vm.NewImageStore(imagesPath), provider)
	b.provision = func(_ context.Context, _, _, _, _ string) error { return nil }
	return b
}

func TestBuild_StoresSnapshotID(t *testing.T) {
	provider := &fakeImageProvider{
		buildVM:    &vm.VM{ID: "build-1", IPAddress: "1.2.3.4", State: vm.VMStateRunning},
		snapshotID: "snap-42",
	}
	b := testBuilder(t, provider)

	profile := types.ProfileSpec{
		Name:           "my-profile",
		Infrastructure: types.InfrastructureConfig{Image: "ubuntu-24.04"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}

	got, err := b.Build(context.Background(), profile)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got != "snap-42" {
		t.Errorf("returned snapshotID: got %q, want %q", got, "snap-42")
	}

	stored, err := b.images.GetHetznerSnapshotID("my-profile")
	if err != nil {
		t.Fatalf("GetHetznerSnapshotID: %v", err)
	}
	if stored != "snap-42" {
		t.Errorf("stored snapshotID: got %q, want %q", stored, "snap-42")
	}
}

func TestBuild_DestroysBuildVMOnSuccess(t *testing.T) {
	provider := &fakeImageProvider{
		buildVM:    &vm.VM{ID: "build-1", IPAddress: "1.2.3.4", State: vm.VMStateRunning},
		snapshotID: "snap-42",
	}
	b := testBuilder(t, provider)

	profile := types.ProfileSpec{
		Name:           "my-profile",
		Infrastructure: types.InfrastructureConfig{Image: "ubuntu-24.04"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}

	if _, err := b.Build(context.Background(), profile); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(provider.destroyedIDs) != 1 || provider.destroyedIDs[0] != "build-1" {
		t.Errorf("expected DestroyBuildVM(build-1), got: %v", provider.destroyedIDs)
	}
}

func TestBuild_CreateBuildVMFailure_ReturnsError(t *testing.T) {
	provider := &fakeImageProvider{buildErr: errors.New("quota exceeded")}
	b := testBuilder(t, provider)

	profile := types.ProfileSpec{
		Name:           "my-profile",
		Infrastructure: types.InfrastructureConfig{Image: "ubuntu-24.04"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}

	_, err := b.Build(context.Background(), profile)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "create build vm") {
		t.Errorf("expected 'create build vm' in error, got: %v", err)
	}
}

func TestBuild_ProvisionFailure_DestroysBuildVM(t *testing.T) {
	provider := &fakeImageProvider{
		buildVM: &vm.VM{ID: "build-1", IPAddress: "1.2.3.4", State: vm.VMStateRunning},
	}
	imagesPath := filepath.Join(t.TempDir(), "images.json")
	b := New(t.TempDir(), vm.NewImageStore(imagesPath), provider)
	b.provision = func(_ context.Context, _, _, _, _ string) error {
		return errors.New("ssh failed")
	}

	profile := types.ProfileSpec{
		Name:           "my-profile",
		Infrastructure: types.InfrastructureConfig{Image: "ubuntu-24.04"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}

	_, err := b.Build(context.Background(), profile)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(provider.destroyedIDs) != 1 {
		t.Errorf("expected DestroyBuildVM called once, got: %v", provider.destroyedIDs)
	}
}

func TestComposeScripts_BaseOnly(t *testing.T) {
	profile := types.ProfileSpec{
		Infrastructure: types.InfrastructureConfig{Tooling: nil},
		Agent:          types.AgentConfig{Provider: "claude"},
	}
	scripts := composeScripts(profile)
	expected := []string{
		"/tmp/agentsdx-vm/base/provision.sh",
		"/tmp/agentsdx-vm/agents/claude/provision.sh",
	}
	if len(scripts) != len(expected) {
		t.Fatalf("expected %d scripts, got %d: %v", len(expected), len(scripts), scripts)
	}
	for i, s := range scripts {
		if s != expected[i] {
			t.Errorf("script[%d]: expected %q, got %q", i, expected[i], s)
		}
	}
}

func TestComposeScripts_WithTooling(t *testing.T) {
	profile := types.ProfileSpec{
		Infrastructure: types.InfrastructureConfig{Tooling: []string{"mise", "docker"}},
		Agent:          types.AgentConfig{Provider: "claude"},
	}
	scripts := composeScripts(profile)
	expected := []string{
		"/tmp/agentsdx-vm/base/provision.sh",
		"/tmp/agentsdx-vm/tooling/mise/provision.sh",
		"/tmp/agentsdx-vm/tooling/docker/provision.sh",
		"/tmp/agentsdx-vm/agents/claude/provision.sh",
	}
	if len(scripts) != len(expected) {
		t.Fatalf("expected %d scripts, got %d: %v", len(expected), len(scripts), scripts)
	}
	for i, s := range scripts {
		if s != expected[i] {
			t.Errorf("script[%d]: expected %q, got %q", i, expected[i], s)
		}
	}
}

func TestWriteOrchestrationScript_ContainsBashCalls(t *testing.T) {
	scriptPath, err := writeOrchestrationScript([]string{"/path/one", "/path/two"}, "claude")
	if err != nil {
		t.Fatalf("writeOrchestrationScript: %v", err)
	}
	defer os.Remove(scriptPath)

	data, _ := os.ReadFile(scriptPath)
	content := string(data)

	for _, want := range []string{
		"#!/bin/bash",
		`bash "/path/one"`,
		`bash "/path/two"`,
		"/usr/local/bin/entrypoint.sh",
		"/usr/local/bin/vault-sync.sh",
		"agents/claude/entrypoint.sh",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("script missing %q", want)
		}
	}
}
```

- [ ] **Step 4: Run builder tests**

```bash
cd server && go test ./internal/builder/ -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/builder/
git commit -m "feat: rewrite builder with SSH provisioning, drop Packer"
```

---

## Task 6 — Update session manager

**Files:**
- Modify: `server/internal/session/manager.go`
- Modify: `server/internal/session/manager_test.go`

- [ ] **Step 1: Update `manager.go`**

Replace the contents of `server/internal/session/manager.go`:

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
```

- [ ] **Step 2: Update `manager_test.go`**

The tests call `session.NewManager` — add `images *vm.ImageStore` as the third argument. Add a helper to build an image store with a pre-seeded snapshot:

```go
package session_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/duck-labs/agentsdx-server/internal/session"
	"github.com/duck-labs/agentsdx-server/internal/vault"
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

func fakeImages(t *testing.T, profileName, snapshotID string) *vm.ImageStore {
	t.Helper()
	store := vm.NewImageStore(filepath.Join(t.TempDir(), "images.json"))
	if err := store.SetHetznerSnapshotID(profileName, snapshotID); err != nil {
		t.Fatalf("seed images: %v", err)
	}
	return store
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

	mgr := session.NewManager(store, newFakeVM(), fakeImages(t, "dev", "snap-1"), vaultDir, vaultSecret, "")
	id, err := mgr.Start(context.Background(), "dev")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty session ID")
	}

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
	mgr := session.NewManager(store, fakeProvider, fakeImages(t, "dev", "snap-1"), vaultDir, vaultSecret, "")

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
```

- [ ] **Step 3: Run session tests**

```bash
cd server && go test ./internal/session/ -v
```

Expected: all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add server/internal/session/manager.go server/internal/session/manager_test.go
git commit -m "feat: add ImageStore to session manager, remove ReportReady"
```

---

## Task 7 — Update API handler

**Files:**
- Modify: `server/internal/api/handler.go`
- Modify: `server/internal/api/handler_test.go`

- [ ] **Step 1: Update `handler.go`**

Make the following changes to `server/internal/api/handler.go`:

1. Rename the `ImageBuilder` interface method:

```go
// ImageBuilder is the interface for building VM images.
type ImageBuilder interface {
	Build(ctx context.Context, profile types.ProfileSpec) (string, error)
}
```

2. Remove `POST /sessions/{id}/ready` from `Router()` — delete this line:

```go
r.Post("/sessions/{id}/ready", h.sessionReady)
```

3. In `buildImage` handler, change `h.builder.BuildQEMU` to `h.builder.Build`:

```go
go func() {
    if _, err := h.builder.Build(context.Background(), spec); err != nil {
        log.Printf("buildImage: profile %s: %v", req.ProfileName, err)
    }
}()
```

4. Delete the entire `sessionReady` method (lines 283–297 in the original).

- [ ] **Step 2: Update `handler_test.go`**

Make two changes:

1. Update `fakeBuilder` to implement the new interface:

```go
type fakeBuilder struct {
	profile string
	err     error
}

func (f *fakeBuilder) Build(_ context.Context, p types.ProfileSpec) (string, error) {
	f.profile = p.Name
	return "snap-42", f.err
}
```

2. Update the `newHandler` helper — add `images` argument to `session.NewManager`:

```go
imagesPath := filepath.Join(dir, "images.json")
images := vm.NewImageStore(imagesPath)
// seed a snapshot so manager.Start doesn't fail on image lookup in tests
_ = images.SetHetznerSnapshotID("dev", "snap-1")

mgr := session.NewManager(sessionStore, fakeProvider, images, dir, "test-secret", "")
```

Also add `"path/filepath"` to imports if not already present.

- [ ] **Step 3: Run handler tests**

```bash
cd server && go test ./internal/api/ -v
```

Expected: all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add server/internal/api/handler.go server/internal/api/handler_test.go
git commit -m "feat: rename ImageBuilder.Build, remove /ready endpoint"
```

---

## Task 8 — Update `serve.go` and `setup.go`

**Files:**
- Modify: `server/cmd/agentsdxd/serve.go`
- Rewrite: `server/cmd/agentsdxd/setup.go`

- [ ] **Step 1: Rewrite `serve.go`**

Replace the entire contents of `server/cmd/agentsdxd/serve.go`:

```go
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"

	"github.com/duck-labs/agentsdx-server/internal/api"
	"github.com/duck-labs/agentsdx-server/internal/builder"
	"github.com/duck-labs/agentsdx-server/internal/db"
	"github.com/duck-labs/agentsdx-server/internal/profile"
	"github.com/duck-labs/agentsdx-server/internal/session"
	"github.com/duck-labs/agentsdx-server/internal/vm"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP server",
		Run: func(cmd *cobra.Command, args []string) {
			runServe()
		},
	}
}

func runServe() {
	secret := mustEnv("AGENTSDX_VAULT_SECRET")
	hcloudToken := mustEnv("AGENTSDX_HCLOUD_TOKEN")
	dataDir := envOrDefault("AGENTSDX_DATA_DIR", "./data")
	addr := envOrDefault("AGENTSDX_ADDR", ":8080")
	serverURL := envOrDefault("AGENTSDX_SERVER_URL", "http://localhost"+addr)
	vmDir := envOrDefault("AGENTSDX_VM_DIR", "./vm")
	hcloudLocation := envOrDefault("AGENTSDX_HCLOUD_LOCATION", "nbg1")

	for _, dir := range []struct {
		path string
		mode os.FileMode
	}{
		{filepath.Join(dataDir, "profiles"), 0755},
		{filepath.Join(dataDir, "vault"), 0700},
		{filepath.Join(dataDir, "images"), 0755},
	} {
		if err := os.MkdirAll(dir.path, dir.mode); err != nil {
			log.Fatalf("create data dir %s: %v", dir.path, err)
		}
	}

	conn, err := db.Open(filepath.Join(dataDir, "agentsdx.db"))
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer conn.Close()

	client := hcloud.NewClient(hcloud.WithToken(hcloudToken))
	hetznerProvider := vm.NewHetznerProvider(client, hcloudLocation)

	images := vm.NewImageStore(filepath.Join(dataDir, "images.json"))
	profileStore := profile.NewStore(conn, filepath.Join(dataDir, "profiles"))
	sessionStore := session.NewStore(conn)
	mgr := session.NewManager(sessionStore, hetznerProvider, images, filepath.Join(dataDir, "vault"), secret, serverURL)
	imageBuilder := builder.New(vmDir, images, hetznerProvider)

	h := api.NewHandler(profileStore, mgr, images, imageBuilder, filepath.Join(dataDir, "vault"), secret)

	log.Printf("agentsdxd listening on %s", addr)
	if err := http.ListenAndServe(addr, h.Router()); err != nil {
		log.Fatalf("listen: %v", err)
	}
}
```

- [ ] **Step 2: Rewrite `setup.go`**

Replace the entire contents of `server/cmd/agentsdxd/setup.go`:

```go
package main

import (
	"context"
	"fmt"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Verify Hetzner Cloud credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup()
		},
	}
}

func runSetup() error {
	fmt.Println("=== agentsdxd setup ===")

	token := mustEnv("AGENTSDX_HCLOUD_TOKEN")
	fmt.Println("\n[1/1] Verifying Hetzner Cloud token...")

	client := hcloud.NewClient(hcloud.WithToken(token))
	opts := hcloud.ServerListOpts{ListOpts: hcloud.ListOpts{PerPage: 1}}
	if _, _, err := client.Server.List(context.Background(), opts); err != nil {
		return fmt.Errorf("hcloud token invalid or API unreachable: %w", err)
	}

	fmt.Println("  Token valid. You can now run: agentsdxd serve")
	return nil
}
```

- [ ] **Step 3: Build everything**

```bash
cd server && go build ./...
```

Expected: clean build with no errors.

- [ ] **Step 4: Run all tests**

```bash
cd server && go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add server/cmd/agentsdxd/serve.go server/cmd/agentsdxd/setup.go
git commit -m "feat: wire HetznerProvider in serve.go, replace setup with hcloud token check"
```

---

## Task 9 — Delete QEMU artifacts and final cleanup

**Files:**
- Delete: `vm/qemu.pkr.hcl`

- [ ] **Step 1: Delete the Packer template**

```bash
git rm vm/qemu.pkr.hcl
```

- [ ] **Step 2: Final build and test**

```bash
cd server && go build ./... && go test ./...
```

Expected: clean build, all tests pass.

- [ ] **Step 3: Final commit**

```bash
git add -A
git commit -m "chore: delete qemu.pkr.hcl, complete Hetzner migration"
```

---

## Self-Review

**Spec coverage check:**

| Spec requirement | Task |
|---|---|
| HetznerProvider implements VMProvider | Task 4 |
| HetznerProvider implements ImageProvider | Task 4 |
| ImageProvider interface in provider.go | Task 1 |
| ImageID in CreateVMRequest | Task 1 |
| GetHetznerSnapshotID / SetHetznerSnapshotID | Task 2 |
| BuildUserData moved to userdata.go | Task 3 |
| iso9660 dropped | Task 3 |
| qemu.go deleted | Task 4 |
| Builder.Build with SSH provisioning | Task 5 |
| ssh.go helpers | Task 5 |
| Session manager gets ImageStore | Task 6 |
| ReportReady removed | Task 6 |
| pollUntilRunning simplified | Task 6 |
| /ready endpoint removed | Task 7 |
| ImageBuilder.Build renamed | Task 7 |
| serve.go wired to Hetzner | Task 8 |
| setup.go validates hcloud token | Task 8 |
| qemu.pkr.hcl deleted | Task 9 |

All spec requirements covered. ✓
