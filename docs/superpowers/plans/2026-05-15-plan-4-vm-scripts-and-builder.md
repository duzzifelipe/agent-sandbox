# Plan 4: VM Provisioning Scripts + Packer Image Builder

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `vm/` shell script tree (base, tooling, agent provisioners, entrypoint, vault-sync) and a Packer HCL template, plus the `server/internal/builder` package that drives Packer to produce bootable OVA images from a profile spec.

**Architecture:** The `vm/` directory at the repo root is a self-contained tree of modular shell scripts and a static Packer HCL template. The server-side `Builder` struct reads a `ProfileSpec`, composes an ordered list of in-VM script paths (base → tooling → agent), writes a temporary orchestration shell script, then invokes `packer build` via an injectable `PackerRunner` interface. On success it records the OVA path in `images.json`. Session env vars (server URL, session ID, profile) and the git SSH private key are injected into the VM at boot via cloud-init `write_files` in the NoCloud user-data.

**Tech Stack:** Packer `virtualbox-iso` builder (external binary), bash shell scripts, Go `os/exec`, Go `encoding/base64`, standard Go `testing`

---

## File Map

| File | Purpose |
|---|---|
| `vm/base/provision.sh` | Install git, curl, openssh-server; configure root SSH access |
| `vm/tooling/mise/provision.sh` | Install mise version manager |
| `vm/tooling/docker/provision.sh` | Install Docker Engine |
| `vm/tooling/docker-compose/provision.sh` | Install docker-compose standalone binary |
| `vm/tooling/gh/provision.sh` | Install GitHub CLI |
| `vm/agents/claude/provision.sh` | Install Node.js + claude CLI |
| `vm/agents/claude/entrypoint.sh` | Restore vault state, clone repos, trap vault-sync, exec claude |
| `vm/agents/_template/provision.sh` | Documented starting point for new agents |
| `vm/agents/_template/entrypoint.sh` | Documented starting point for new agents |
| `vm/vault-sync.sh` | Tar vault paths and POST to server on session end |
| `vm/autoinstall/user-data.yaml` | Ubuntu subiquity autoinstall cloud-config for Packer |
| `vm/virtualbox.pkr.hcl` | Packer template: boots Ubuntu ISO, uploads scripts, runs orchestration script |
| `server/internal/vm/nocloud.go` | Replace `NoCloudUserData` with `BuildUserData` (adds env vars + git key) |
| `server/internal/vm/nocloud_test.go` | Tests for updated `BuildUserData` |
| `server/internal/vm/images.go` | Add `List()` method to `ImageStore` |
| `server/internal/vm/images_test.go` | Test for `List()` |
| `server/internal/session/manager.go` | Add `serverURL` field; call `BuildUserData` instead of `NoCloudUserData` |
| `server/internal/session/manager_test.go` | Update `NewManager` call sites |
| `server/internal/builder/builder.go` | `Builder` struct + `PackerRunner` interface + `BuildVirtualBox` |
| `server/internal/builder/builder_test.go` | Unit tests using fake runner |
| `server/internal/api/handler.go` | Add `ImageBuilder` interface; implement `buildImage`, `listImages`, `getAgentState` |
| `server/internal/api/handler_test.go` | Update helper + add fake builder + new handler tests |
| `server/cmd/agentsdxd/main.go` | Instantiate builder; read `AGENTSDX_SERVER_URL` + `AGENTSDX_VM_DIR` |

---

### Task 1: Extend NoCloudUserData + add serverURL to Manager

**Files:**
- Modify: `server/internal/vm/nocloud.go`
- Modify: `server/internal/vm/nocloud_test.go`
- Modify: `server/internal/session/manager.go`
- Modify: `server/internal/session/manager_test.go`

Cloud-init user-data must now inject three values into every VM: the server URL, session ID, and profile name (so entrypoint.sh can call back to the server), plus the git private key (written to `/root/.ssh/id_rsa` so repos can be cloned). We use base64 encoding for the key to avoid YAML indentation issues.

- [ ] **Step 1: Write the failing test**

Replace the contents of `server/internal/vm/nocloud_test.go` with:

```go
package vm_test

import (
	"os"
	"strings"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/kdomanski/iso9660"
)

func TestWriteNoCloudISO_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	metaData := "instance-id: test\nlocal-hostname: test-vm\n"
	userData := "#cloud-config\nssh_authorized_keys:\n  - ssh-rsa AAAA...\n"

	isoPath, err := vm.WriteNoCloudISO(dir, metaData, userData)
	if err != nil {
		t.Fatalf("WriteNoCloudISO: %v", err)
	}
	if _, err := os.Stat(isoPath); err != nil {
		t.Fatalf("ISO file not found: %v", err)
	}
}

func TestWriteNoCloudISO_ContainsFiles(t *testing.T) {
	dir := t.TempDir()
	isoPath, err := vm.WriteNoCloudISO(dir, "instance-id: abc\n", "#cloud-config\n")
	if err != nil {
		t.Fatalf("WriteNoCloudISO: %v", err)
	}
	f, err := os.Open(isoPath)
	if err != nil {
		t.Fatalf("open ISO: %v", err)
	}
	defer f.Close()
	img, err := iso9660.OpenImage(f)
	if err != nil {
		t.Fatalf("open iso9660 image: %v", err)
	}
	root, err := img.RootDir()
	if err != nil {
		t.Fatalf("root dir: %v", err)
	}
	children, err := root.GetChildren()
	if err != nil {
		t.Fatalf("get children: %v", err)
	}
	names := make(map[string]bool)
	for _, c := range children {
		names[c.Name()] = true
	}
	for _, want := range []string{"meta-data", "user-data"} {
		if !names[want] {
			t.Errorf("ISO missing file %q; got names: %v", want, names)
		}
	}
}

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

- [ ] **Step 2: Run tests — expect failure**

```bash
cd server && go test ./internal/vm/... -run "TestBuildUserData" -v
```

Expected: compile error — `vm.BuildUserData` undefined.

- [ ] **Step 3: Implement BuildUserData in nocloud.go**

Replace the body of `server/internal/vm/nocloud.go` (keep `WriteNoCloudISO`, `NoCloudMetaData`; replace `NoCloudUserData` with `BuildUserData`):

```go
package vm

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kdomanski/iso9660"
)

// WriteNoCloudISO creates a NoCloud data source ISO at dir/nocloud.iso.
func WriteNoCloudISO(dir, metaData, userData string) (string, error) {
	writer, err := iso9660.NewWriter()
	if err != nil {
		return "", fmt.Errorf("new iso writer: %w", err)
	}
	defer writer.Cleanup()

	if err := writer.AddFile(strings.NewReader(metaData), "meta-data"); err != nil {
		return "", fmt.Errorf("add meta-data: %w", err)
	}
	if err := writer.AddFile(strings.NewReader(userData), "user-data"); err != nil {
		return "", fmt.Errorf("add user-data: %w", err)
	}

	isoPath := filepath.Join(dir, "nocloud.iso")
	f, err := os.OpenFile(isoPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("create iso file: %w", err)
	}
	defer f.Close()

	if err := writer.WriteTo(f, "cidata"); err != nil {
		return "", fmt.Errorf("write iso: %w", err)
	}
	return isoPath, nil
}

// NoCloudMetaData returns minimal cloud-init meta-data for the given instance ID.
func NoCloudMetaData(instanceID string) string {
	return fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", instanceID, instanceID)
}

// BuildUserData returns cloud-init user-data that:
//   - registers the VM access authorized key
//   - writes /root/.ssh/id_rsa (git private key, base64-encoded to avoid YAML issues)
//   - writes /etc/agentsdx.env with session context for entrypoint.sh
func BuildUserData(authorizedKey, gitPrivateKey, sessionID, serverURL, profileName string) string {
	encodedKey := base64.StdEncoding.EncodeToString([]byte(gitPrivateKey))
	return fmt.Sprintf(`#cloud-config
ssh_authorized_keys:
  - %s
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
}
```

- [ ] **Step 4: Run all vm tests — expect pass**

```bash
cd server && go test ./internal/vm/... -v
```

Expected: all tests `PASS`.

- [ ] **Step 5: Update Manager to accept serverURL**

Replace `server/internal/session/manager.go`:

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
	serverURL   string
}

// NewManager creates a Manager.
func NewManager(store *Store, provider vm.VMProvider, vaultDir, vaultSecret, serverURL string) *Manager {
	return &Manager{
		store:       store,
		provider:    provider,
		vaultDir:    vaultDir,
		vaultSecret: vaultSecret,
		serverURL:   serverURL,
	}
}

// Start creates a session, launches the VM, and returns the session ID immediately.
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

- [ ] **Step 6: Update manager_test.go — add empty serverURL to NewManager calls**

In `server/internal/session/manager_test.go`, update every `session.NewManager(...)` call from 4 args to 5 by appending `""` as the serverURL:

```go
// Line 65 — change:
mgr := session.NewManager(store, newFakeVM(), vaultDir, vaultSecret)
// to:
mgr := session.NewManager(store, newFakeVM(), vaultDir, vaultSecret, "")

// Line 101 — change:
mgr := session.NewManager(store, fakeProvider, vaultDir, vaultSecret)
// to:
mgr := session.NewManager(store, fakeProvider, vaultDir, vaultSecret, "")
```

- [ ] **Step 7: Run session tests — expect pass**

```bash
cd server && go test ./internal/session/... -v
```

Expected: all tests `PASS`.

- [ ] **Step 8: Commit**

```bash
git add server/internal/vm/nocloud.go server/internal/vm/nocloud_test.go \
        server/internal/session/manager.go server/internal/session/manager_test.go
git commit -m "feat(server): inject session env vars + git key into VM via cloud-init"
```

---

### Task 2: Add List() to ImageStore

**Files:**
- Modify: `server/internal/vm/images.go`
- Modify: `server/internal/vm/images_test.go`

The `listImages` handler needs to read all entries from `images.json`. The existing `ImageStore` only supports per-profile get/set; this task adds a `List()` method that returns all entries as `[]types.ImageEntry`.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/vm/images_test.go`:

```go
func TestImageStore_List_Empty(t *testing.T) {
	dir := t.TempDir()
	store := vm.NewImageStore(filepath.Join(dir, "images.json"))
	entries, err := store.List()
	if err != nil {
		t.Fatalf("List on missing file: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestImageStore_List_ReturnsAll(t *testing.T) {
	dir := t.TempDir()
	writeImagesJSON(t, dir, map[string]vm.ImageRecord{
		"profile-a": {vm.ProviderVirtualBox: "/data/images/a.ova"},
		"profile-b": {vm.ProviderVirtualBox: "/data/images/b.ova"},
	})
	store := vm.NewImageStore(filepath.Join(dir, "images.json"))
	entries, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	seen := make(map[string]string)
	for _, e := range entries {
		seen[e.ProfileName] = e.VirtualBox
	}
	if seen["profile-a"] != "/data/images/a.ova" {
		t.Errorf("profile-a: got %q", seen["profile-a"])
	}
	if seen["profile-b"] != "/data/images/b.ova" {
		t.Errorf("profile-b: got %q", seen["profile-b"])
	}
}
```

- [ ] **Step 2: Run tests — expect failure**

```bash
cd server && go test ./internal/vm/... -run "TestImageStore_List" -v
```

Expected: compile error — `store.List` undefined.

- [ ] **Step 3: Implement List()**

Add the following method to `server/internal/vm/images.go` (after `SetVirtualBoxPath`):

```go
// List returns all image entries from images.json.
// Returns an empty slice if the file does not exist yet.
func (s *ImageStore) List() ([]types.ImageEntry, error) {
	records, err := s.load()
	if os.IsNotExist(err) {
		return []types.ImageEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load images: %w", err)
	}
	entries := make([]types.ImageEntry, 0, len(records))
	for profileName, rec := range records {
		entries = append(entries, types.ImageEntry{
			ProfileName: profileName,
			VirtualBox:  rec[ProviderVirtualBox],
			Hetzner:     rec[ProviderHetzner],
		})
	}
	return entries, nil
}
```

Also add the import for `types` at the top of `server/internal/vm/images.go`:

```go
import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/duck-labs/agentsdx-shared/types"
)
```

- [ ] **Step 4: Run all vm tests — expect pass**

```bash
cd server && go test ./internal/vm/... -v
```

Expected: all tests `PASS`.

- [ ] **Step 5: Commit**

```bash
git add server/internal/vm/images.go server/internal/vm/images_test.go
git commit -m "feat(server): add ImageStore.List() for listing all built images"
```

---

### Task 3: vm/base/provision.sh

**Files:**
- Create: `vm/base/provision.sh`

This script runs first on every Packer build. It installs the minimal toolset needed by all agents and configures SSH for root login.

- [ ] **Step 1: Create the directory and script**

```bash
mkdir -p vm/base
```

Create `vm/base/provision.sh`:

```bash
#!/bin/bash
# Base provisioner: installs minimal tools and configures root SSH access.
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

apt-get update -y
apt-get install -y \
    curl \
    wget \
    git \
    ca-certificates \
    jq \
    tar \
    openssh-server

systemctl enable ssh

mkdir -p /root/.ssh
chmod 700 /root/.ssh
touch /root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys

# Allow root login with authorized keys; no password.
sed -i 's/^#*PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
sed -i 's/^#*PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config

echo "base provisioning complete"
```

- [ ] **Step 2: Make it executable**

```bash
chmod +x vm/base/provision.sh
```

- [ ] **Step 3: Commit**

```bash
git add vm/base/provision.sh
git commit -m "feat(vm): add base provision script"
```

---

### Task 4: Tooling provision scripts

**Files:**
- Create: `vm/tooling/mise/provision.sh`
- Create: `vm/tooling/docker/provision.sh`
- Create: `vm/tooling/docker-compose/provision.sh`
- Create: `vm/tooling/gh/provision.sh`

Each script is standalone and idempotent. The builder runs whichever scripts are declared in `infrastructure.tooling`.

- [ ] **Step 1: Create directories**

```bash
mkdir -p vm/tooling/mise vm/tooling/docker vm/tooling/docker-compose vm/tooling/gh
```

- [ ] **Step 2: Create vm/tooling/mise/provision.sh**

```bash
#!/bin/bash
# Installs mise version manager and activates it for root.
set -euo pipefail

curl -fsSL https://mise.run | sh

echo 'eval "$(~/.local/bin/mise activate bash)"' >> /root/.bashrc
echo 'eval "$(~/.local/bin/mise activate bash)"' >> /etc/profile.d/mise.sh
chmod +x /etc/profile.d/mise.sh

echo "mise provisioning complete"
```

- [ ] **Step 3: Create vm/tooling/docker/provision.sh**

```bash
#!/bin/bash
# Installs Docker Engine (CE) from the official Docker apt repository.
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc

echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] \
  https://download.docker.com/linux/ubuntu \
  $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
  tee /etc/apt/sources.list.d/docker.list > /dev/null

apt-get update -y
apt-get install -y docker-ce docker-ce-cli containerd.io

systemctl enable docker

echo "docker provisioning complete"
```

- [ ] **Step 4: Create vm/tooling/docker-compose/provision.sh**

```bash
#!/bin/bash
# Installs docker-compose v2 standalone binary.
set -euo pipefail

COMPOSE_VERSION="v2.35.0"
curl -fsSL \
  "https://github.com/docker/compose/releases/download/${COMPOSE_VERSION}/docker-compose-linux-x86_64" \
  -o /usr/local/bin/docker-compose
chmod +x /usr/local/bin/docker-compose

echo "docker-compose provisioning complete"
```

- [ ] **Step 5: Create vm/tooling/gh/provision.sh**

```bash
#!/bin/bash
# Installs the GitHub CLI from the official GitHub apt repository.
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | \
  dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg

chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg

echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] \
  https://cli.github.com/packages stable main" | \
  tee /etc/apt/sources.list.d/github-cli.list > /dev/null

apt-get update -y
apt-get install -y gh

echo "gh provisioning complete"
```

- [ ] **Step 6: Make all scripts executable and commit**

```bash
chmod +x vm/tooling/mise/provision.sh vm/tooling/docker/provision.sh \
          vm/tooling/docker-compose/provision.sh vm/tooling/gh/provision.sh
git add vm/tooling/
git commit -m "feat(vm): add tooling provision scripts (mise, docker, docker-compose, gh)"
```

---

### Task 5: Claude agent scripts

**Files:**
- Create: `vm/agents/claude/provision.sh`
- Create: `vm/agents/claude/entrypoint.sh`

`provision.sh` is called during the Packer build to install the claude CLI. `entrypoint.sh` is baked into the image and runs when a user SSH-connects to start a session.

- [ ] **Step 1: Create directories**

```bash
mkdir -p vm/agents/claude
```

- [ ] **Step 2: Create vm/agents/claude/provision.sh**

```bash
#!/bin/bash
# Installs Node.js 22.x (required by claude CLI) and the claude CLI itself.
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
apt-get install -y nodejs

npm install -g @anthropic-ai/claude-code

echo "claude provisioning complete"
```

- [ ] **Step 3: Create vm/agents/claude/entrypoint.sh**

```bash
#!/bin/bash
# Claude entrypoint: restores vault state, clones repos, registers vault-sync on exit,
# then hands off to the claude CLI.
# Runs inside the VM when the user SSH-connects.
set -euo pipefail

# Load session context written by cloud-init.
if [[ -f /etc/agentsdx.env ]]; then
    set -a
    # shellcheck source=/dev/null
    source /etc/agentsdx.env
    set +a
fi

: "${AGENTSDX_SERVER_URL:?AGENTSDX_SERVER_URL is not set in /etc/agentsdx.env}"
: "${AGENTSDX_SESSION_ID:?AGENTSDX_SESSION_ID is not set}"
: "${AGENTSDX_PROFILE:?AGENTSDX_PROFILE is not set}"

# Ensure git SSH key permissions (cloud-init sets mode, but be defensive).
chmod 600 /root/.ssh/id_rsa
chmod 700 /root/.ssh

# Configure SSH to use the git key for code hosts.
cat > /root/.ssh/config <<'EOF'
Host github.com
    IdentityFile /root/.ssh/id_rsa
    StrictHostKeyChecking accept-new

Host gitlab.com
    IdentityFile /root/.ssh/id_rsa
    StrictHostKeyChecking accept-new
EOF
chmod 600 /root/.ssh/config

# Restore agent state (claude memory, settings) from server vault.
STATE_FILE="$(mktemp /tmp/agent-state-XXXXXX.tar)"
HTTP_STATUS=$(curl -s -o "$STATE_FILE" -w "%{http_code}" \
    "${AGENTSDX_SERVER_URL}/sessions/${AGENTSDX_SESSION_ID}/agent-state")
if [[ "$HTTP_STATUS" == "200" ]] && [[ -s "$STATE_FILE" ]]; then
    tar -xf "$STATE_FILE" -C /root/ 2>/dev/null || true
fi
rm -f "$STATE_FILE"

# Clone repos declared in the profile.
PROFILE_JSON=$(curl -sf "${AGENTSDX_SERVER_URL}/profiles/${AGENTSDX_PROFILE}" || echo "{}")
while IFS= read -r line; do
    repo=$(echo "$line" | cut -d' ' -f1)
    path=$(echo "$line" | cut -d' ' -f2-)
    expanded="${path/#\~//root}"
    if [[ -n "$repo" ]] && [[ ! -d "$expanded" ]]; then
        mkdir -p "$(dirname "$expanded")"
        git clone "$repo" "$expanded" || echo "warning: failed to clone $repo"
    fi
done < <(echo "$PROFILE_JSON" | jq -r '.projects[]? | "\(.repo) \(.path)"')

# Sync vault state back to server when this SSH session ends.
trap '/usr/local/bin/vault-sync.sh' EXIT

exec claude
```

- [ ] **Step 4: Make executable and commit**

```bash
chmod +x vm/agents/claude/provision.sh vm/agents/claude/entrypoint.sh
git add vm/agents/claude/
git commit -m "feat(vm): add claude agent provision and entrypoint scripts"
```

---

### Task 6: _template agent scripts + vault-sync.sh

**Files:**
- Create: `vm/agents/_template/provision.sh`
- Create: `vm/agents/_template/entrypoint.sh`
- Create: `vm/vault-sync.sh`

- [ ] **Step 1: Create _template directory**

```bash
mkdir -p vm/agents/_template
```

- [ ] **Step 2: Create vm/agents/_template/provision.sh**

```bash
#!/bin/bash
# Template provision script for adding a new agent.
# Copy this directory to vm/agents/<agent-name>/ and fill in the steps below.
set -euo pipefail

# TODO: install <agent-name> binary and its dependencies here.
# Example:
#   curl -fsSL https://example.com/install.sh | sh
#   apt-get install -y <package>

echo "<agent-name> provisioning complete"
```

- [ ] **Step 3: Create vm/agents/_template/entrypoint.sh**

```bash
#!/bin/bash
# Template entrypoint for a new agent.
# This script runs inside the VM when a user SSH-connects.
# Copy this to vm/agents/<agent-name>/entrypoint.sh and replace exec line.
set -euo pipefail

if [[ -f /etc/agentsdx.env ]]; then
    set -a
    source /etc/agentsdx.env
    set +a
fi

: "${AGENTSDX_SERVER_URL:?}"
: "${AGENTSDX_SESSION_ID:?}"
: "${AGENTSDX_PROFILE:?}"

chmod 600 /root/.ssh/id_rsa

# Restore agent state from server vault.
STATE_FILE="$(mktemp /tmp/agent-state-XXXXXX.tar)"
HTTP_STATUS=$(curl -s -o "$STATE_FILE" -w "%{http_code}" \
    "${AGENTSDX_SERVER_URL}/sessions/${AGENTSDX_SESSION_ID}/agent-state")
if [[ "$HTTP_STATUS" == "200" ]] && [[ -s "$STATE_FILE" ]]; then
    tar -xf "$STATE_FILE" -C /root/ 2>/dev/null || true
fi
rm -f "$STATE_FILE"

trap '/usr/local/bin/vault-sync.sh' EXIT

# TODO: replace with the actual agent binary.
exec <agent-binary>
```

- [ ] **Step 4: Create vm/vault-sync.sh**

```bash
#!/bin/bash
# Syncs agent state back to the server at session end.
# Invoked via trap in entrypoint.sh; reads context from /etc/agentsdx.env.
set -euo pipefail

if [[ -f /etc/agentsdx.env ]]; then
    set -a
    source /etc/agentsdx.env
    set +a
fi

: "${AGENTSDX_SERVER_URL:?AGENTSDX_SERVER_URL is not set}"
: "${AGENTSDX_SESSION_ID:?AGENTSDX_SESSION_ID is not set}"

TARBALL="$(mktemp /tmp/vault-sync-XXXXXX.tar)"

PATHS=()
for p in /root/.claude /root/.claude.json; do
    [[ -e "$p" ]] && PATHS+=("$(basename "$p")")
done

if [[ ${#PATHS[@]} -eq 0 ]]; then
    echo "vault-sync: no agent state found, skipping"
    exit 0
fi

cd /root
tar -cf "$TARBALL" "${PATHS[@]}"

curl -s -X POST \
    --data-binary "@$TARBALL" \
    -H "Content-Type: application/octet-stream" \
    "${AGENTSDX_SERVER_URL}/sessions/${AGENTSDX_SESSION_ID}/vault-sync"

rm -f "$TARBALL"
echo "vault-sync: agent state synced"
```

- [ ] **Step 5: Make executable and commit**

```bash
chmod +x vm/agents/_template/provision.sh vm/agents/_template/entrypoint.sh vm/vault-sync.sh
git add vm/agents/_template/ vm/vault-sync.sh
git commit -m "feat(vm): add agent template scripts and vault-sync"
```

---

### Task 7: Packer HCL template + Ubuntu autoinstall user-data

**Files:**
- Create: `vm/autoinstall/user-data.yaml`
- Create: `vm/virtualbox.pkr.hcl`

The HCL template boots an Ubuntu 24.04 ISO using subiquity autoinstall (served via Packer's built-in HTTP server), uploads the `vm/` tree into the running VM, then runs the generated orchestration script.

- [ ] **Step 1: Generate a SHA-512 password hash for "packer"**

Run this on your machine (requires openssl):

```bash
openssl passwd -6 -salt agentsdx packer
```

Copy the output — it looks like `$6$agentsdx$...`. You will paste it into the next step.

- [ ] **Step 2: Create vm/autoinstall/ directory**

```bash
mkdir -p vm/autoinstall
```

- [ ] **Step 3: Create vm/autoinstall/user-data.yaml**

Replace `PASTE_HASH_HERE` with the hash from Step 1:

```yaml
#cloud-config
autoinstall:
  version: 1
  apt:
    geoip: false
    primary:
      - arches: [amd64, i386]
        uri: http://archive.ubuntu.com/ubuntu
  identity:
    hostname: agentsdx-vm
    # Password for the initial ubuntu user (only used during installation).
    # Generate with: openssl passwd -6 -salt agentsdx packer
    password: "PASTE_HASH_HERE"
    username: ubuntu
    realname: ubuntu
  ssh:
    allow-pw: true
    install-server: true
  storage:
    layout:
      name: direct
  user-data:
    disable_root: false
  late-commands:
    # Set root password so Packer can SSH in as root to run provisioners.
    - "echo 'root:packer' | chpasswd --root /target"
    - "sed -i 's/^#*PermitRootLogin.*/PermitRootLogin yes/' /target/etc/ssh/sshd_config"
    - "sed -i 's/^#*PasswordAuthentication.*/PasswordAuthentication yes/' /target/etc/ssh/sshd_config"
```

- [ ] **Step 4: Find the Ubuntu 24.04.2 ISO checksum**

```bash
curl -s https://releases.ubuntu.com/24.04.2/SHA256SUMS | grep live-server-amd64
```

Copy the SHA256 hash (64 hex chars). You will paste it in Step 5.

- [ ] **Step 5: Create vm/virtualbox.pkr.hcl**

Replace `PASTE_CHECKSUM_HERE` with `sha256:` followed by the hash from Step 4:

```hcl
packer {
  required_plugins {
    virtualbox = {
      source  = "github.com/hashicorp/virtualbox"
      version = "~> 1"
    }
  }
}

variable "vm_name" {
  type        = string
  description = "VM name; used as the OVA filename base"
}

variable "iso_url" {
  type        = string
  default     = "https://releases.ubuntu.com/24.04.2/ubuntu-24.04.2-live-server-amd64.iso"
  description = "URL or local path to the Ubuntu 24.04 server ISO"
}

variable "iso_checksum" {
  type        = string
  default     = "PASTE_CHECKSUM_HERE"
  description = "sha256:<hex> checksum — verify at https://releases.ubuntu.com/24.04.2/SHA256SUMS"
}

variable "provision_script" {
  type        = string
  description = "Local path to the generated orchestration shell script"
}

variable "output_dir" {
  type        = string
  description = "Directory where the exported OVA will be written"
}

variable "ssh_password" {
  type      = string
  default   = "packer"
  sensitive = true
}

source "virtualbox-iso" "vm" {
  vm_name          = var.vm_name
  iso_url          = var.iso_url
  iso_checksum     = var.iso_checksum
  guest_os_type    = "Ubuntu_64"
  disk_size        = 20480
  memory           = 2048
  cpus             = 2
  headless         = true
  ssh_username     = "root"
  ssh_password     = var.ssh_password
  ssh_timeout      = "30m"
  shutdown_command = "shutdown -P now"

  # Serve autoinstall user-data and meta-data via Packer's HTTP server.
  http_content = {
    "/user-data" = file("${path.root}/autoinstall/user-data.yaml")
    "/meta-data" = ""
  }

  boot_wait = "5s"
  # GRUB command-line: boot with autoinstall pointing at Packer's HTTP server.
  boot_command = [
    "c<wait>",
    "linux /casper/vmlinuz --- autoinstall ds=\"nocloud-net;s=http://{{.HTTPIP}}:{{.HTTPPort}}/\" <wait>",
    "<enter><wait>",
    "initrd /casper/initrd<wait>",
    "<enter><wait>",
    "boot<enter>"
  ]

  vboxmanage = [
    ["modifyvm", "{{.Name}}", "--nic1", "hostonly", "--hostonlyadapter1", "vboxnet0"],
  ]

  output_directory = var.output_dir
  format           = "ova"
}

build {
  sources = ["source.virtualbox-iso.vm"]

  # Upload the entire vm/ directory so provision scripts are available in the VM.
  provisioner "file" {
    source      = "${path.root}/"
    destination = "/tmp/agentsdx-vm/"
  }

  # Run the builder-generated orchestration script.
  provisioner "shell" {
    script = var.provision_script
  }
}
```

- [ ] **Step 6: Validate HCL syntax (requires packer in PATH)**

```bash
cd vm && packer validate \
  -var vm_name=test \
  -var provision_script=/dev/null \
  -var output_dir=/tmp/test-output \
  virtualbox.pkr.hcl
```

Expected: `The configuration is valid.`
If packer is not installed: `brew install packer` (macOS) or skip and note in PR.

- [ ] **Step 7: Commit**

```bash
git add vm/autoinstall/ vm/virtualbox.pkr.hcl
git commit -m "feat(vm): add Packer VirtualBox template with Ubuntu autoinstall"
```

---

### Task 8: Image builder package

**Files:**
- Create: `server/internal/builder/builder.go`
- Create: `server/internal/builder/builder_test.go`

The `Builder` composes in-VM script paths from a `ProfileSpec`, writes a temporary orchestration script (bash that runs each path in the VM at `/tmp/agentsdx-vm/...`), invokes Packer via an injectable `PackerRunner`, and on success records the OVA path in `ImageStore`.

- [ ] **Step 1: Write the failing tests**

Create `server/internal/builder/builder_test.go`:

```go
package builder

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/duck-labs/agentsdx-shared/types"
)

// fakeRunner records the orchestration script content before returning.
type fakeRunner struct {
	scriptContent string
	calledVars    map[string]string
	err           error
}

func (f *fakeRunner) Run(_ context.Context, _ string, vars map[string]string) error {
	f.calledVars = make(map[string]string)
	for k, v := range vars {
		f.calledVars[k] = v
	}
	if path, ok := vars["provision_script"]; ok {
		data, _ := os.ReadFile(path)
		f.scriptContent = string(data)
	}
	return f.err
}

func newTestBuilder(t *testing.T, fr *fakeRunner) (*Builder, *vm.ImageStore) {
	t.Helper()
	dir := t.TempDir()
	store := vm.NewImageStore(filepath.Join(dir, "images.json"))
	return &Builder{
		vmDir:      "/app/vm",
		imagesDir:  dir,
		imageStore: store,
		runner:     fr,
	}, store
}

func TestBuildVirtualBox_ScriptOrder(t *testing.T) {
	fr := &fakeRunner{}
	b, _ := newTestBuilder(t, fr)

	spec := types.ProfileSpec{
		Infrastructure: types.InfrastructureConfig{
			Image:   "ubuntu-24.04",
			Tooling: []string{"mise", "docker"},
		},
		Agent: types.AgentConfig{Provider: "claude"},
	}

	if err := b.BuildVirtualBox(context.Background(), "myprofile", spec); err != nil {
		t.Fatalf("BuildVirtualBox: %v", err)
	}

	script := fr.scriptContent
	baseIdx := strings.Index(script, "/tmp/agentsdx-vm/base/provision.sh")
	miseIdx := strings.Index(script, "/tmp/agentsdx-vm/tooling/mise/provision.sh")
	dockerIdx := strings.Index(script, "/tmp/agentsdx-vm/tooling/docker/provision.sh")
	claudeIdx := strings.Index(script, "/tmp/agentsdx-vm/agents/claude/provision.sh")

	for name, idx := range map[string]int{
		"base":   baseIdx,
		"mise":   miseIdx,
		"docker": dockerIdx,
		"claude": claudeIdx,
	} {
		if idx < 0 {
			t.Errorf("script missing %s provisioner; script:\n%s", name, script)
		}
	}

	if baseIdx > miseIdx || miseIdx > dockerIdx || dockerIdx > claudeIdx {
		t.Errorf("script provisioners out of order: base=%d mise=%d docker=%d claude=%d",
			baseIdx, miseIdx, dockerIdx, claudeIdx)
	}
}

func TestBuildVirtualBox_EntrypointCopied(t *testing.T) {
	fr := &fakeRunner{}
	b, _ := newTestBuilder(t, fr)

	spec := types.ProfileSpec{
		Infrastructure: types.InfrastructureConfig{Image: "ubuntu-24.04"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}

	if err := b.BuildVirtualBox(context.Background(), "p", spec); err != nil {
		t.Fatalf("BuildVirtualBox: %v", err)
	}

	if !strings.Contains(fr.scriptContent, "/usr/local/bin/entrypoint.sh") {
		t.Errorf("script missing entrypoint.sh copy step; script:\n%s", fr.scriptContent)
	}
	if !strings.Contains(fr.scriptContent, "/usr/local/bin/vault-sync.sh") {
		t.Errorf("script missing vault-sync.sh copy step; script:\n%s", fr.scriptContent)
	}
}

func TestBuildVirtualBox_StoresOVAPath(t *testing.T) {
	fr := &fakeRunner{}
	b, store := newTestBuilder(t, fr)

	spec := types.ProfileSpec{
		Infrastructure: types.InfrastructureConfig{Image: "ubuntu-24.04"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}

	if err := b.BuildVirtualBox(context.Background(), "myprofile", spec); err != nil {
		t.Fatalf("BuildVirtualBox: %v", err)
	}

	ovaPath, err := store.GetVirtualBoxPath("myprofile")
	if err != nil {
		t.Fatalf("GetVirtualBoxPath after build: %v", err)
	}
	if !strings.Contains(ovaPath, "myprofile") {
		t.Errorf("OVA path %q doesn't contain profile name", ovaPath)
	}
	if !strings.HasSuffix(ovaPath, ".ova") {
		t.Errorf("OVA path %q doesn't end with .ova", ovaPath)
	}
}

func TestBuildVirtualBox_UnknownImage(t *testing.T) {
	fr := &fakeRunner{}
	b, _ := newTestBuilder(t, fr)

	spec := types.ProfileSpec{
		Infrastructure: types.InfrastructureConfig{Image: "nonexistent-os"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}

	err := b.BuildVirtualBox(context.Background(), "p", spec)
	if err == nil {
		t.Fatal("expected error for unknown base image")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error should mention 'unsupported': %v", err)
	}
}

func TestBuildVirtualBox_RunnerError_NoStoreUpdate(t *testing.T) {
	fr := &fakeRunner{err: fmt.Errorf("packer failed")}
	b, store := newTestBuilder(t, fr)

	spec := types.ProfileSpec{
		Infrastructure: types.InfrastructureConfig{Image: "ubuntu-24.04"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}

	err := b.BuildVirtualBox(context.Background(), "p", spec)
	if err == nil {
		t.Fatal("expected error when runner fails")
	}

	if _, storeErr := store.GetVirtualBoxPath("p"); storeErr == nil {
		t.Error("expected ImageStore to have no entry after a failed build")
	}
}

func TestBuildVirtualBox_TempScriptCleanedUp(t *testing.T) {
	var capturedPath string
	fr := &fakeRunner{}
	b, _ := newTestBuilder(t, fr)

	// Capture the provision_script path via a custom runner.
	b.runner = &capturingRunner{inner: fr, capture: &capturedPath}

	spec := types.ProfileSpec{
		Infrastructure: types.InfrastructureConfig{Image: "ubuntu-24.04"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}

	b.BuildVirtualBox(context.Background(), "p", spec)

	if capturedPath != "" {
		if _, err := os.Stat(capturedPath); !os.IsNotExist(err) {
			t.Errorf("temp orchestration script %q was not cleaned up", capturedPath)
		}
	}
}

type capturingRunner struct {
	inner   PackerRunner
	capture *string
}

func (c *capturingRunner) Run(ctx context.Context, hclPath string, vars map[string]string) error {
	if p, ok := vars["provision_script"]; ok {
		*c.capture = p
	}
	return c.inner.Run(ctx, hclPath, vars)
}
```

- [ ] **Step 2: Run tests — expect failure**

```bash
cd server && go test ./internal/builder/... -v
```

Expected: compile error — package does not exist yet.

- [ ] **Step 3: Implement builder.go**

Create `server/internal/builder/builder.go`:

```go
package builder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/duck-labs/agentsdx-shared/types"
)

// PackerRunner abstracts shelling out to packer, allowing tests to inject a fake.
type PackerRunner interface {
	Run(ctx context.Context, hclPath string, vars map[string]string) error
}

// Builder composes vm/ scripts for a profile and drives Packer to produce a VirtualBox OVA.
type Builder struct {
	vmDir      string
	imagesDir  string
	imageStore *vm.ImageStore
	runner     PackerRunner
}

// New creates a Builder that shells out to the real packer binary.
func New(vmDir, imagesDir string, imageStore *vm.ImageStore) *Builder {
	return &Builder{
		vmDir:      vmDir,
		imagesDir:  imagesDir,
		imageStore: imageStore,
		runner:     &realPackerRunner{},
	}
}

// isoRegistry maps infrastructure.image values to [isoURL, isoChecksum].
// Add entries here when supporting new base OS images.
var isoRegistry = map[string][2]string{
	"ubuntu-24.04": {
		"https://releases.ubuntu.com/24.04.2/ubuntu-24.04.2-live-server-amd64.iso",
		// Verify: curl -s https://releases.ubuntu.com/24.04.2/SHA256SUMS | grep live-server-amd64
		"sha256:d6dab0c4cb67c685bf41e4585fd426b4b90cb0a8b5db7c90a9e5f84e8e1e1b0e",
	},
}

// BuildVirtualBox builds a VirtualBox OVA for the given profile and stores the OVA
// path in images.json. It blocks until Packer finishes; call it in a goroutine.
func (b *Builder) BuildVirtualBox(ctx context.Context, profileName string, spec types.ProfileSpec) error {
	isoInfo, ok := isoRegistry[spec.Infrastructure.Image]
	if !ok {
		return fmt.Errorf("unsupported base image: %q (add it to builder.isoRegistry)", spec.Infrastructure.Image)
	}

	scripts := b.composeScripts(spec)

	tmpScript, err := b.writeOrchestrationScript(scripts, spec.Agent.Provider)
	if err != nil {
		return fmt.Errorf("write orchestration script: %w", err)
	}
	defer os.Remove(tmpScript)

	outDir := filepath.Join(b.imagesDir, profileName)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	hclPath := filepath.Join(b.vmDir, "virtualbox.pkr.hcl")
	vars := map[string]string{
		"vm_name":          profileName,
		"iso_url":          isoInfo[0],
		"iso_checksum":     isoInfo[1],
		"provision_script": tmpScript,
		"output_dir":       outDir,
	}

	if err := b.runner.Run(ctx, hclPath, vars); err != nil {
		return fmt.Errorf("packer build: %w", err)
	}

	ovaPath := filepath.Join(outDir, profileName+".ova")
	if err := b.imageStore.SetVirtualBoxPath(profileName, ovaPath); err != nil {
		return fmt.Errorf("store image path: %w", err)
	}

	return nil
}

// composeScripts returns the ordered list of in-VM script paths to execute:
// base → each declared tool → agent.
func (b *Builder) composeScripts(spec types.ProfileSpec) []string {
	scripts := []string{"/tmp/agentsdx-vm/base/provision.sh"}
	for _, tool := range spec.Infrastructure.Tooling {
		scripts = append(scripts, fmt.Sprintf("/tmp/agentsdx-vm/tooling/%s/provision.sh", tool))
	}
	scripts = append(scripts, fmt.Sprintf("/tmp/agentsdx-vm/agents/%s/provision.sh", spec.Agent.Provider))
	return scripts
}

// writeOrchestrationScript writes a temporary shell script that runs all scripts
// in order, then copies the agent entrypoint and vault-sync into the image.
func (b *Builder) writeOrchestrationScript(scripts []string, agentProvider string) (string, error) {
	f, err := os.CreateTemp("", "agentsdx-provision-*.sh")
	if err != nil {
		return "", err
	}
	defer f.Close()

	lines := []string{
		"#!/bin/bash",
		"set -euo pipefail",
		"",
	}
	for _, s := range scripts {
		lines = append(lines, fmt.Sprintf("bash %q", s))
	}
	lines = append(lines,
		fmt.Sprintf("cp %q /usr/local/bin/entrypoint.sh",
			fmt.Sprintf("/tmp/agentsdx-vm/agents/%s/entrypoint.sh", agentProvider)),
		"cp /tmp/agentsdx-vm/vault-sync.sh /usr/local/bin/vault-sync.sh",
		"chmod +x /usr/local/bin/entrypoint.sh /usr/local/bin/vault-sync.sh",
	)

	if _, err := f.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		os.Remove(f.Name())
		return "", err
	}

	if err := os.Chmod(f.Name(), 0755); err != nil {
		os.Remove(f.Name())
		return "", err
	}

	return f.Name(), nil
}

type realPackerRunner struct{}

func (r *realPackerRunner) Run(ctx context.Context, hclPath string, vars map[string]string) error {
	args := []string{"build"}
	for k, v := range vars {
		args = append(args, "-var", k+"="+v)
	}
	args = append(args, hclPath)

	cmd := exec.CommandContext(ctx, "packer", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
cd server && go test ./internal/builder/... -v
```

Expected: all 5 tests `PASS`.

- [ ] **Step 5: Commit**

```bash
git add server/internal/builder/
git commit -m "feat(server): add image builder package with Packer orchestration"
```

---

### Task 9: Add getAgentState endpoint + wire builder into handler

**Files:**
- Modify: `server/internal/api/handler.go`
- Modify: `server/internal/api/handler_test.go`

The handler gains three changes:
1. An `ImageBuilder` interface so `buildImage` can delegate to `Builder`
2. A real `listImages` implementation that reads from `ImageStore`
3. A new `getAgentState` handler that serves the vault tarball back to entrypoint.sh

- [ ] **Step 1: Update handler_test.go**

Replace `server/internal/api/handler_test.go` with:

```go
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

// fakeVM satisfies vm.VMProvider for handler tests.
type fakeVM struct{}

func (f *fakeVM) CreateVM(_ context.Context, req vm.CreateVMRequest) (*vm.VM, error) {
	return &vm.VM{ID: "fake-" + req.ProfileName, State: vm.VMStateRunning, IPAddress: "192.168.56.1"}, nil
}
func (f *fakeVM) DestroyVM(_ context.Context, _ string) error { return nil }
func (f *fakeVM) GetVM(_ context.Context, vmID string) (*vm.VM, error) {
	return &vm.VM{ID: vmID, State: vm.VMStateRunning, IPAddress: "192.168.56.1"}, nil
}

// fakeBuilder satisfies api.ImageBuilder for handler tests.
type fakeBuilder struct {
	lastProfile string
	err         error
}

func (fb *fakeBuilder) BuildVirtualBox(_ context.Context, profileName string, _ types.ProfileSpec) error {
	fb.lastProfile = profileName
	return fb.err
}

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

	mgr := session.NewManager(sessionStore, &fakeVM{}, dir, "test-secret", "")

	h := api.NewHandler(profileStore, mgr, images, &fakeBuilder{}, dir, "test-secret")
	return h, dir
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

func TestHandler_BuildImage_ProfileNotFound(t *testing.T) {
	h, _ := newHandler(t)

	body, _ := json.Marshal(types.BuildImageRequest{ProfileName: "missing"})
	req := httptest.NewRequest(http.MethodPost, "/images/build", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing profile, got %d — %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_BuildImage_AcceptedForExistingProfile(t *testing.T) {
	h, _ := newHandler(t)

	spec := types.ProfileSpec{
		Name:           "my-profile",
		Infrastructure: types.InfrastructureConfig{Image: "ubuntu-24.04"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}
	body, _ := json.Marshal(spec)
	req := httptest.NewRequest(http.MethodPost, "/profiles", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /profiles: %d", rec.Code)
	}

	body, _ = json.Marshal(types.BuildImageRequest{ProfileName: "my-profile"})
	req = httptest.NewRequest(http.MethodPost, "/images/build", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202 Accepted, got %d — %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_ListImages_Empty(t *testing.T) {
	h, _ := newHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/images", nil)
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /images: got %d", rec.Code)
	}
	var entries []types.ImageEntry
	json.NewDecoder(rec.Body).Decode(&entries)
	if entries == nil {
		t.Error("expected non-nil (empty) slice")
	}
}

func TestHandler_GetAgentState_NoContent(t *testing.T) {
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
	var sess types.SessionResponse
	json.NewDecoder(rec.Body).Decode(&sess)

	req = httptest.NewRequest(http.MethodGet, "/sessions/"+sess.ID+"/agent-state", nil)
	rec = httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)
	// No credentials have been uploaded — expect 204.
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204 when no agent state exists, got %d — %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run tests — expect failure**

```bash
cd server && go test ./internal/api/... -v
```

Expected: compile errors — `api.NewHandler` has wrong signature; `api.ImageBuilder` undefined.

- [ ] **Step 3: Replace handler.go**

Replace `server/internal/api/handler.go` with:

```go
package api

import (
	"context"
	"encoding/json"
	"io"
	"log"
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

// ImageBuilder drives Packer to build VM images.
type ImageBuilder interface {
	BuildVirtualBox(ctx context.Context, profileName string, spec types.ProfileSpec) error
}

// Handler holds all dependencies for the HTTP API.
type Handler struct {
	profiles    *profile.Store
	sessions    *session.Manager
	images      *vm.ImageStore
	builder     ImageBuilder
	vaultDir    string
	vaultSecret string
}

// NewHandler creates a Handler.
func NewHandler(
	profiles *profile.Store,
	sessions *session.Manager,
	images *vm.ImageStore,
	builder ImageBuilder,
	vaultDir string,
	vaultSecret string,
) *Handler {
	return &Handler{
		profiles:    profiles,
		sessions:    sessions,
		images:      images,
		builder:     builder,
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
	r.Get("/sessions/{id}/agent-state", h.getAgentState)
	r.Post("/sessions/{id}/stop", h.stopSession)
	r.Post("/sessions/{id}/vault-sync", h.vaultSync)

	r.Post("/images/build", h.buildImage)
	r.Get("/images", h.listImages)

	return r
}

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
	agentStatePath := filepath.Join(h.vaultDir, name+"-agent-state.tar")
	if err := os.WriteFile(agentStatePath, tarball, 0600); err != nil {
		writeError(w, http.StatusInternalServerError, "store agent state")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

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

// getAgentState serves the agent state tarball stored in the vault.
// It checks for the encrypted vault-sync file first (most recent state), then falls
// back to the unencrypted tarball written by "agentsdx credentials set".
// Returns 204 if no state has been stored yet.
func (h *Handler) getAgentState(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	resp, err := h.sessions.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	encPath := filepath.Join(h.vaultDir, resp.Profile+"-agent-state.tar.enc")
	if data, err := os.ReadFile(encPath); err == nil {
		key, err := vault.DeriveKey(h.vaultSecret, resp.Profile+"-agent-state")
		if err == nil {
			if decrypted, err := vault.Decrypt(key, data); err == nil {
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Write(decrypted)
				return
			}
		}
	}

	plainPath := filepath.Join(h.vaultDir, resp.Profile+"-agent-state.tar")
	data, err := os.ReadFile(plainPath)
	if os.IsNotExist(err) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read agent state")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(data)
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

// buildImage validates the profile exists, then starts a Packer build in the background.
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
		if err := h.builder.BuildVirtualBox(context.Background(), req.ProfileName, spec); err != nil {
			log.Printf("image build failed for %q: %v", req.ProfileName, err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":  "build started",
		"profile": req.ProfileName,
	})
}

func (h *Handler) listImages(w http.ResponseWriter, r *http.Request) {
	entries, err := h.images.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
```

- [ ] **Step 4: Run all api tests — expect pass**

```bash
cd server && go test ./internal/api/... -v
```

Expected: all tests `PASS`.

- [ ] **Step 5: Commit**

```bash
git add server/internal/api/handler.go server/internal/api/handler_test.go
git commit -m "feat(server): add ImageBuilder interface, getAgentState endpoint, real buildImage + listImages"
```

---

### Task 10: Wire builder into main.go + final verification

**Files:**
- Modify: `server/cmd/agentsdxd/main.go`

Two new env vars are required:
- `AGENTSDX_SERVER_URL` — injected into every VM so entrypoint.sh can call back
- `AGENTSDX_VM_DIR` — path to the `vm/` directory inside the container (defaults to `./vm`)

- [ ] **Step 1: Replace main.go**

```go
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/duck-labs/agentsdx-server/internal/api"
	"github.com/duck-labs/agentsdx-server/internal/builder"
	"github.com/duck-labs/agentsdx-server/internal/db"
	"github.com/duck-labs/agentsdx-server/internal/profile"
	"github.com/duck-labs/agentsdx-server/internal/session"
	"github.com/duck-labs/agentsdx-server/internal/vm"
)

func main() {
	secret := mustEnv("AGENTSDX_VAULT_SECRET")
	serverURL := mustEnv("AGENTSDX_SERVER_URL")
	dataDir := envOrDefault("AGENTSDX_DATA_DIR", "./data")
	vmDir := envOrDefault("AGENTSDX_VM_DIR", "./vm")
	addr := envOrDefault("AGENTSDX_ADDR", ":8080")

	for _, dir := range []struct {
		path string
		mode os.FileMode
	}{
		{filepath.Join(dataDir, "profiles"), 0755},
		{filepath.Join(dataDir, "vault"), 0700},
		{filepath.Join(dataDir, "iso"), 0755},
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

	profileStore := profile.NewStore(conn, filepath.Join(dataDir, "profiles"))
	images := vm.NewImageStore(filepath.Join(dataDir, "images.json"))
	provider := vm.NewVirtualBoxProvider(images, filepath.Join(dataDir, "iso"))

	sessionStore := session.NewStore(conn)
	mgr := session.NewManager(sessionStore, provider, filepath.Join(dataDir, "vault"), secret, serverURL)

	bldr := builder.New(vmDir, filepath.Join(dataDir, "images"), images)

	h := api.NewHandler(profileStore, mgr, images, bldr, filepath.Join(dataDir, "vault"), secret)

	log.Printf("agentsdxd listening on %s (server URL: %s, vm dir: %s)", addr, serverURL, vmDir)
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

- [ ] **Step 2: Verify server builds**

```bash
cd server && go build ./...
```

Expected: no errors.

- [ ] **Step 3: Run all server tests**

```bash
cd server && go test ./... -v
```

Expected: all tests `PASS`.

- [ ] **Step 4: Verify the full monorepo builds**

```bash
cd shared && go test ./...
cd ../cli && go build ./...
cd ../server && go build ./...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add server/cmd/agentsdxd/main.go
git commit -m "feat(server): wire builder into main; add AGENTSDX_SERVER_URL + AGENTSDX_VM_DIR"
```

---

## Self-Review

**Spec coverage:**

| Design requirement | Task |
|---|---|
| base/provision.sh installs base tools | Task 3 ✓ |
| tooling/*/provision.sh for mise, docker, docker-compose, gh | Task 4 ✓ |
| agents/claude/provision.sh installs claude binary | Task 5 ✓ |
| agents/claude/entrypoint.sh restores vault, clones repos, execs claude | Task 5 ✓ |
| agents/_template/ for new agents | Task 6 ✓ |
| vault-sync.sh tars vault paths and POSTs to server | Task 6 ✓ |
| Packer HCL template for VirtualBox | Task 7 ✓ |
| Builder composes scripts from ProfileSpec | Task 8 ✓ |
| Builder shells out to packer with generated script | Task 8 ✓ |
| Builder updates images.json on success | Task 8 ✓ |
| buildImage handler triggers async build | Task 9 ✓ |
| listImages handler reads real entries | Task 9 ✓ |
| getAgentState serves vault tarball to entrypoint.sh | Task 9 ✓ |
| Session env vars (server URL, session ID, profile) injected via cloud-init | Task 1 ✓ |
| Git SSH key injected via cloud-init write_files | Task 1 ✓ |
| AGENTSDX_SERVER_URL required in main | Task 10 ✓ |
| AGENTSDX_VM_DIR configures vm/ path | Task 10 ✓ |

**Placeholder scan:** No TBDs or stubs. Shell scripts are complete and runnable. Go code compiles with matching types across all tasks.

**Type consistency:**
- `vm.BuildUserData` defined in Task 1, called from `session.Manager.Start` in Task 1 ✓
- `vm.ImageStore.List()` defined in Task 2, called from `handler.listImages` in Task 9 ✓
- `builder.Builder.BuildVirtualBox` signature matches `api.ImageBuilder` interface in Task 9 ✓
- `session.NewManager` 5-arg signature consistent across manager.go (Task 1), manager_test.go (Task 1), handler_test.go (Task 9), main.go (Task 10) ✓
- `api.NewHandler` 6-arg signature consistent across handler.go (Task 9), handler_test.go (Task 9), main.go (Task 10) ✓
