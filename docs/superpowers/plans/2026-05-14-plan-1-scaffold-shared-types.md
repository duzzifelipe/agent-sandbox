# Plan 1: Repo Scaffold + Shared Types

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Set up the three-module Go monorepo structure and define all shared types used by both the server and CLI.

**Architecture:** Three Go modules (`shared`, `cli`, `server`) in one git repo. `shared` holds pure data types — ProfileSpec, VaultData, and API request/response structs. `cli` and `server` depend on `shared` via `replace` directives during local development. No business logic lives in `shared`.

**Tech Stack:** Go 1.23, `gopkg.in/yaml.v3` (profile YAML tags + tests), standard `encoding/json` (vault + API types), standard `testing` package.

---

## File Map

| File | Purpose |
|---|---|
| `shared/go.mod` | Module: `github.com/duck-labs/agentsdx-shared` |
| `shared/types/profile.go` | `ProfileSpec`, `InfrastructureConfig`, `ProjectConfig`, `AgentConfig` |
| `shared/types/profile_test.go` | YAML roundtrip tests for profile types |
| `shared/types/vault.go` | `VaultData` |
| `shared/types/vault_test.go` | JSON roundtrip tests for vault types |
| `shared/types/api.go` | Session state constants, all HTTP request/response structs |
| `shared/types/api_test.go` | JSON roundtrip tests for API types |
| `cli/go.mod` | Module: `github.com/duck-labs/agentsdx-cli`, replace → `../shared` |
| `cli/cmd/agentsdx/main.go` | Minimal entry point (`fmt.Println("agentsdx")`) |
| `server/go.mod` | Module: `github.com/duck-labs/agentsdx-server`, replace → `../shared` |
| `server/cmd/agentsdxd/main.go` | Minimal entry point (`fmt.Println("agentsdxd")`) |
| `docker-compose.yml` | Server self-hosting setup |

---

### Task 1: Initialize the shared module

**Files:**
- Create: `shared/go.mod`
- Create: `shared/types/.gitkeep`

- [ ] **Step 1: Create directory structure**

```bash
mkdir -p shared/types cli/cmd/agentsdx server/cmd/agentsdxd
```

- [ ] **Step 2: Initialize shared Go module**

```bash
cd shared && go mod init github.com/duck-labs/agentsdx-shared && go get gopkg.in/yaml.v3
```

Expected: `shared/go.mod` created with `module github.com/duck-labs/agentsdx-shared` and `gopkg.in/yaml.v3` in require.

- [ ] **Step 3: Verify**

```bash
cd shared && cat go.mod
```

Expected output contains:
```
module github.com/duck-labs/agentsdx-shared

go 1.23
```

- [ ] **Step 4: Commit**

```bash
git add shared/go.mod shared/go.sum
git commit -m "feat: initialize shared Go module"
```

---

### Task 2: Define profile types

**Files:**
- Create: `shared/types/profile.go`
- Create: `shared/types/profile_test.go`

- [ ] **Step 1: Write the failing test**

Create `shared/types/profile_test.go`:

```go
package types_test

import (
	"testing"

	"github.com/duck-labs/agentsdx-shared/types"
	"gopkg.in/yaml.v3"
)

func TestProfileSpec_YAMLRoundtrip(t *testing.T) {
	input := `
name: work-backend
infrastructure:
  provider: virtualbox
  image: ubuntu-24.04
  tooling:
    - mise
    - docker
    - gh
projects:
  - repo: git@github.com:org/api.git
    path: ~/projects/api
agent:
  provider: claude
  skills:
    - superpowers/brainstorming
`
	var got types.ProfileSpec
	if err := yaml.Unmarshal([]byte(input), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Name != "work-backend" {
		t.Errorf("Name: got %q, want %q", got.Name, "work-backend")
	}
	if got.Infrastructure.Provider != "virtualbox" {
		t.Errorf("Infrastructure.Provider: got %q, want %q", got.Infrastructure.Provider, "virtualbox")
	}
	if got.Infrastructure.Image != "ubuntu-24.04" {
		t.Errorf("Infrastructure.Image: got %q, want %q", got.Infrastructure.Image, "ubuntu-24.04")
	}
	if len(got.Infrastructure.Tooling) != 3 {
		t.Errorf("Infrastructure.Tooling: got %d items, want 3", len(got.Infrastructure.Tooling))
	}
	if len(got.Projects) != 1 || got.Projects[0].Repo != "git@github.com:org/api.git" {
		t.Errorf("Projects: got %+v", got.Projects)
	}
	if got.Agent.Provider != "claude" {
		t.Errorf("Agent.Provider: got %q, want %q", got.Agent.Provider, "claude")
	}
	if len(got.Agent.Skills) != 1 {
		t.Errorf("Agent.Skills: got %d items, want 1", len(got.Agent.Skills))
	}

	// Marshal back and unmarshal again to verify roundtrip
	out, err := yaml.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got2 types.ProfileSpec
	if err := yaml.Unmarshal(out, &got2); err != nil {
		t.Fatalf("second unmarshal: %v", err)
	}
	if got2.Name != got.Name {
		t.Errorf("roundtrip Name mismatch: %q vs %q", got2.Name, got.Name)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd shared && go test ./types/... -run TestProfileSpec_YAMLRoundtrip -v
```

Expected: compile error — `types.ProfileSpec` undefined.

- [ ] **Step 3: Implement profile types**

Create `shared/types/profile.go`:

```go
package types

type ProfileSpec struct {
	Name           string              `yaml:"name"           json:"name"`
	Infrastructure InfrastructureConfig `yaml:"infrastructure" json:"infrastructure"`
	Projects       []ProjectConfig     `yaml:"projects"       json:"projects"`
	Agent          AgentConfig         `yaml:"agent"          json:"agent"`
}

type InfrastructureConfig struct {
	Provider string   `yaml:"provider" json:"provider"`
	Image    string   `yaml:"image"    json:"image"`
	Tooling  []string `yaml:"tooling"  json:"tooling"`
}

type ProjectConfig struct {
	Repo string `yaml:"repo" json:"repo"`
	Path string `yaml:"path" json:"path"`
}

type AgentConfig struct {
	Provider string   `yaml:"provider" json:"provider"`
	Skills   []string `yaml:"skills"   json:"skills"`
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd shared && go test ./types/... -run TestProfileSpec_YAMLRoundtrip -v
```

Expected: `PASS`

- [ ] **Step 5: Commit**

```bash
git add shared/types/profile.go shared/types/profile_test.go
git commit -m "feat: add shared ProfileSpec types"
```

---

### Task 3: Define vault types

**Files:**
- Create: `shared/types/vault.go`
- Create: `shared/types/vault_test.go`

- [ ] **Step 1: Write the failing test**

Create `shared/types/vault_test.go`:

```go
package types_test

import (
	"encoding/json"
	"testing"

	"github.com/duck-labs/agentsdx-shared/types"
)

func TestVaultData_JSONRoundtrip(t *testing.T) {
	original := types.VaultData{
		GitPrivateKey:      "git-priv-key",
		GitPublicKey:       "git-pub-key",
		VMAccessPrivateKey: "vm-priv-key",
		VMAccessPublicKey:  "vm-pub-key",
		AgentStatePaths:    []string{".claude/", ".claude.json"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got types.VaultData
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.GitPrivateKey != original.GitPrivateKey {
		t.Errorf("GitPrivateKey: got %q, want %q", got.GitPrivateKey, original.GitPrivateKey)
	}
	if got.VMAccessPublicKey != original.VMAccessPublicKey {
		t.Errorf("VMAccessPublicKey: got %q, want %q", got.VMAccessPublicKey, original.VMAccessPublicKey)
	}
	if len(got.AgentStatePaths) != 2 {
		t.Errorf("AgentStatePaths: got %d items, want 2", len(got.AgentStatePaths))
	}
	if got.AgentStatePaths[0] != ".claude/" || got.AgentStatePaths[1] != ".claude.json" {
		t.Errorf("AgentStatePaths: got %v", got.AgentStatePaths)
	}
}

func TestVaultData_DefaultAgentStatePaths(t *testing.T) {
	v := types.DefaultVaultData()
	if len(v.AgentStatePaths) != 2 {
		t.Fatalf("expected 2 default paths, got %d", len(v.AgentStatePaths))
	}
	if v.AgentStatePaths[0] != ".claude/" {
		t.Errorf("first path: got %q, want %q", v.AgentStatePaths[0], ".claude/")
	}
	if v.AgentStatePaths[1] != ".claude.json" {
		t.Errorf("second path: got %q, want %q", v.AgentStatePaths[1], ".claude.json")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd shared && go test ./types/... -run TestVaultData -v
```

Expected: compile error — `types.VaultData` undefined.

- [ ] **Step 3: Implement vault types**

Create `shared/types/vault.go`:

```go
package types

type VaultData struct {
	GitPrivateKey      string   `json:"git_private_key"`
	GitPublicKey       string   `json:"git_public_key"`
	VMAccessPrivateKey string   `json:"vm_access_private_key"`
	VMAccessPublicKey  string   `json:"vm_access_public_key"`
	AgentStatePaths    []string `json:"agent_state_paths"`
}

func DefaultVaultData() VaultData {
	return VaultData{
		AgentStatePaths: []string{".claude/", ".claude.json"},
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd shared && go test ./types/... -run TestVaultData -v
```

Expected: `PASS`

- [ ] **Step 5: Commit**

```bash
git add shared/types/vault.go shared/types/vault_test.go
git commit -m "feat: add shared VaultData types"
```

---

### Task 4: Define API types

**Files:**
- Create: `shared/types/api.go`
- Create: `shared/types/api_test.go`

- [ ] **Step 1: Write the failing test**

Create `shared/types/api_test.go`:

```go
package types_test

import (
	"encoding/json"
	"testing"

	"github.com/duck-labs/agentsdx-shared/types"
)

func TestCreateSessionRequest_JSON(t *testing.T) {
	req := types.CreateSessionRequest{ProfileName: "work-backend"}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got types.CreateSessionRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ProfileName != "work-backend" {
		t.Errorf("ProfileName: got %q, want %q", got.ProfileName, "work-backend")
	}
}

func TestSessionResponse_JSON(t *testing.T) {
	resp := types.SessionResponse{
		ID:        "abc123",
		Profile:   "work-backend",
		State:     types.SessionStateRunning,
		IPAddress: "192.168.56.10",
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got types.SessionResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != "abc123" {
		t.Errorf("ID: got %q, want %q", got.ID, "abc123")
	}
	if got.State != types.SessionStateRunning {
		t.Errorf("State: got %q, want %q", got.State, types.SessionStateRunning)
	}
	if got.IPAddress != "192.168.56.10" {
		t.Errorf("IPAddress: got %q, want %q", got.IPAddress, "192.168.56.10")
	}
}

func TestSessionStates_Values(t *testing.T) {
	states := []string{
		types.SessionStatePending,
		types.SessionStateStarting,
		types.SessionStateRunning,
		types.SessionStateStopping,
		types.SessionStateDestroyed,
	}
	for _, s := range states {
		if s == "" {
			t.Errorf("session state constant is empty string")
		}
	}
}

func TestBuildImageRequest_JSON(t *testing.T) {
	req := types.BuildImageRequest{ProfileName: "work-backend"}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got types.BuildImageRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ProfileName != "work-backend" {
		t.Errorf("ProfileName: got %q, want %q", got.ProfileName, "work-backend")
	}
}

func TestImageEntry_JSON(t *testing.T) {
	entry := types.ImageEntry{
		ProfileName: "work-backend",
		VirtualBox:  "/data/images/work-backend.ova",
		Hetzner:     "",
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got types.ImageEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.VirtualBox != "/data/images/work-backend.ova" {
		t.Errorf("VirtualBox: got %q, want %q", got.VirtualBox, "/data/images/work-backend.ova")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd shared && go test ./types/... -run "TestCreateSessionRequest|TestSessionResponse|TestSessionStates|TestBuildImageRequest|TestImageEntry" -v
```

Expected: compile error — `types.CreateSessionRequest` undefined.

- [ ] **Step 3: Implement API types**

Create `shared/types/api.go`:

```go
package types

const (
	SessionStatePending   = "pending"
	SessionStateStarting  = "starting"
	SessionStateRunning   = "running"
	SessionStateStopping  = "stopping"
	SessionStateDestroyed = "destroyed"
)

type CreateSessionRequest struct {
	ProfileName string `json:"profile_name"`
}

type SessionResponse struct {
	ID        string `json:"id"`
	Profile   string `json:"profile"`
	State     string `json:"state"`
	IPAddress string `json:"ip_address,omitempty"`
}

type VMKeyResponse struct {
	PrivateKey string `json:"private_key"`
}

type BuildImageRequest struct {
	ProfileName string `json:"profile_name"`
}

type ImageEntry struct {
	ProfileName string `json:"profile_name"`
	VirtualBox  string `json:"virtualbox"`
	Hetzner     string `json:"hetzner"`
}

// Credentials upload is a raw multipart tarball (no JSON struct needed).
// The HTTP handler in the server reads the request body directly as bytes.
```

- [ ] **Step 4: Run all shared tests to verify everything passes**

```bash
cd shared && go test ./... -v
```

Expected: all tests `PASS`.

- [ ] **Step 5: Commit**

```bash
git add shared/types/api.go shared/types/api_test.go
git commit -m "feat: add shared API types and session state constants"
```

---

### Task 5: Initialize CLI and server modules

**Files:**
- Create: `cli/go.mod`
- Create: `cli/cmd/agentsdx/main.go`
- Create: `server/go.mod`
- Create: `server/cmd/agentsdxd/main.go`

- [ ] **Step 1: Initialize CLI module**

```bash
cd cli && go mod init github.com/duck-labs/agentsdx-cli
```

- [ ] **Step 2: Add shared dependency with replace directive to cli/go.mod**

Edit `cli/go.mod` to add the require and replace:

```
module github.com/duck-labs/agentsdx-cli

go 1.23

require github.com/duck-labs/agentsdx-shared v0.0.0

replace github.com/duck-labs/agentsdx-shared => ../shared
```

- [ ] **Step 3: Create CLI entry point**

Create `cli/cmd/agentsdx/main.go`:

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "agentsdx: not yet implemented")
	os.Exit(1)
}
```

- [ ] **Step 4: Verify CLI builds**

```bash
cd cli && go build ./...
```

Expected: no errors.

- [ ] **Step 5: Initialize server module**

```bash
cd server && go mod init github.com/duck-labs/agentsdx-server
```

- [ ] **Step 6: Add shared dependency with replace directive to server/go.mod**

Edit `server/go.mod`:

```
module github.com/duck-labs/agentsdx-server

go 1.23

require github.com/duck-labs/agentsdx-shared v0.0.0

replace github.com/duck-labs/agentsdx-shared => ../shared
```

- [ ] **Step 7: Create server entry point**

Create `server/cmd/agentsdxd/main.go`:

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "agentsdxd: not yet implemented")
	os.Exit(1)
}
```

- [ ] **Step 8: Verify server builds**

```bash
cd server && go build ./...
```

Expected: no errors.

- [ ] **Step 9: Verify shared types are importable from CLI**

Create a temporary file `cli/cmd/agentsdx/check_test.go`:

```go
package main

import (
	"testing"

	"github.com/duck-labs/agentsdx-shared/types"
)

func TestSharedTypesImportable(t *testing.T) {
	p := types.ProfileSpec{Name: "test"}
	if p.Name != "test" {
		t.Fail()
	}
	v := types.DefaultVaultData()
	if len(v.AgentStatePaths) == 0 {
		t.Fail()
	}
}
```

```bash
cd cli && go test ./cmd/agentsdx/... -v
```

Expected: `PASS`. Then delete the temp test file:

```bash
rm cli/cmd/agentsdx/check_test.go
```

- [ ] **Step 10: Commit**

```bash
git add cli/ server/
git commit -m "feat: initialize cli and server Go modules with shared dependency"
```

---

### Task 6: Add docker-compose.yml

**Files:**
- Create: `docker-compose.yml`

- [ ] **Step 1: Create docker-compose.yml**

Create `docker-compose.yml`:

```yaml
services:
  agentsdx:
    image: ghcr.io/duck-labs/agentsdx-server:latest
    volumes:
      - ./data:/data
    environment:
      AGENTSDX_VAULT_SECRET: "${AGENTSDX_VAULT_SECRET}"
    ports:
      - "8080:8080"
```

- [ ] **Step 2: Verify it parses correctly**

```bash
docker compose config
```

Expected: composed configuration printed without errors. (If Docker is not available, skip this step.)

- [ ] **Step 3: Commit**

```bash
git add docker-compose.yml
git commit -m "feat: add docker-compose for server self-hosting"
```

---

### Task 7: Final verification

- [ ] **Step 1: Run all tests across all modules**

```bash
cd shared && go test ./... -v
cd ../cli && go build ./...
cd ../server && go build ./...
```

Expected: all `shared` tests pass, both binaries build without errors.

- [ ] **Step 2: Verify repo structure matches spec**

```bash
find . -name "*.go" -o -name "go.mod" -o -name "*.yml" | grep -v ".git" | sort
```

Expected output includes:
```
./cli/cmd/agentsdx/main.go
./cli/go.mod
./docker-compose.yml
./server/cmd/agentsdxd/main.go
./server/go.mod
./shared/go.mod
./shared/types/api.go
./shared/types/api_test.go
./shared/types/profile.go
./shared/types/profile_test.go
./shared/types/vault.go
./shared/types/vault_test.go
```
