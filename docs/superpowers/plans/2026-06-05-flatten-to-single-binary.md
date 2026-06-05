# Flatten to Single Binary Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Collapse three Go modules (`cli`, `server`, `shared`) into one standalone `agentsdx` binary that calls all business logic directly — no daemon, no HTTP client layer, no SQLite.

**Architecture:** A new Go module lives at the repo root. All packages from `server/internal/` and `shared/types/` are migrated into `internal/`. The CLI commands call them directly. `LocalProvider` drops its SQLite dependency in favour of an in-memory map (VM lifetime = process lifetime). Profile state moves to YAML files in `~/.agentsdx/profiles/`.

**Tech Stack:** Go 1.26.3, cobra, survey/v2, hcloud-go/v2, golang.org/x/crypto, gopkg.in/yaml.v3, github.com/google/uuid

---

## File Map

**Create (new module root):**
- `go.mod` — single module `github.com/duck-labs/agentsdx`
- `go.sum` — generated

**Create (types):**
- `internal/types/profile.go` — ProfileSpec, InfrastructureConfig, ProjectConfig, AgentConfig
- `internal/types/vault.go` — VaultData, DefaultVaultData

**Create (data directory helpers):**
- `internal/datadir/datadir.go` — path helpers for `~/.agentsdx/`

**Create (vault — migrated from server/internal/vault):**
- `internal/vault/vault.go` — DeriveKey, Encrypt, Decrypt, GenerateKeyPair, StoreVaultData, LoadVaultData, VaultExists
- `internal/vault/vault_test.go`

**Create (profile store — new YAML implementation):**
- `internal/profile/store.go` — YAML-backed store reading from `~/.agentsdx/profiles/`
- `internal/profile/store_test.go`

**Create (vm — migrated from server/internal/vm):**
- `internal/vm/provider.go` — VMProvider, ImageProvider interfaces and VM struct (unchanged)
- `internal/vm/images.go` — ImageStore (unchanged logic)
- `internal/vm/userdata.go` — BuildUserData, drop sessionID and serverURL params
- `internal/vm/hetzner.go` — HetznerProvider (unchanged logic)
- `internal/vm/local.go` — LocalProvider, replace *sql.DB with in-memory map
- `internal/vm/hetzner_test.go`
- `internal/vm/local_test.go`
- `internal/vm/userdata_test.go`

**Create (builder — migrated from server/internal/builder):**
- `internal/builder/builder.go` — Builder.Build (unchanged logic)
- `internal/builder/ssh.go` — SSH helpers (unchanged)
- `internal/builder/builder_test.go`

**Create (session runner):**
- `internal/session/runner.go` — Run function: create VM → poll SSH → run SSH as child → destroy on exit

**Create (CLI commands):**
- `cmd/agentsdx/main.go` — init providers from env, wire commands
- `cmd/agentsdx/profiles.go` — profiles list/create/run/build/repo add
- `cmd/agentsdx/secrets.go` — secrets set/delete/list

**Modify:**
- `mise.toml` — replace three-module tasks with single-module tasks

**Delete (after all tests pass):**
- `cli/` — entire directory
- `server/` — entire directory
- `shared/` — entire directory

---

## Task 1: Create the new go.mod at repo root

**Files:**
- Create: `go.mod`

- [ ] **Step 1: Write go.mod**

```
module github.com/duck-labs/agentsdx

go 1.26.3

require (
	github.com/AlecAivazis/survey/v2 v2.3.7
	github.com/google/uuid v1.6.0
	github.com/hetznercloud/hcloud-go/v2 v2.41.1
	github.com/spf13/cobra v1.10.2
	golang.org/x/crypto v0.51.0
	gopkg.in/yaml.v3 v3.0.1
)
```

Save to `go.mod`. Do not run `go mod tidy` yet — dependencies will be resolved after all files exist.

- [ ] **Step 2: Commit**

```bash
git add go.mod
git commit -m "feat: add root go.mod for single agentsdx module"
```

---

## Task 2: Create `internal/types/` package

**Files:**
- Create: `internal/types/profile.go`
- Create: `internal/types/vault.go`

- [ ] **Step 1: Write profile.go**

```go
package types

type ProfileSpec struct {
	Name           string               `yaml:"name"           json:"name"`
	Infrastructure InfrastructureConfig `yaml:"infrastructure" json:"infrastructure"`
	Projects       []ProjectConfig      `yaml:"projects"       json:"projects"`
	Agent          AgentConfig          `yaml:"agent"          json:"agent"`
}

type InfrastructureConfig struct {
	Provider string   `yaml:"provider" json:"provider"`
	Image    string   `yaml:"image"    json:"image"`
	Tooling  []string `yaml:"tooling"  json:"tooling"`
}

type ProjectConfig struct {
	Repo         string `yaml:"repo"           json:"repo"`
	Path         string `yaml:"path"           json:"path"`
	AuthTokenEnv string `yaml:"auth_token_env" json:"auth_token_env,omitempty"`
}

type AgentConfig struct {
	Provider string   `yaml:"provider" json:"provider"`
	Skills   []string `yaml:"skills"   json:"skills"`
}
```

- [ ] **Step 2: Write vault.go**

```go
package types

type VaultData struct {
	GitPrivateKey      string            `json:"git_private_key"`
	GitPublicKey       string            `json:"git_public_key"`
	VMAccessPrivateKey string            `json:"vm_access_private_key"`
	VMAccessPublicKey  string            `json:"vm_access_public_key"`
	Secrets            map[string]string `json:"secrets,omitempty"`
}

func DefaultVaultData() VaultData {
	return VaultData{}
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/types/
git commit -m "feat: add internal/types package"
```

---

## Task 3: Create `internal/datadir/` package

**Files:**
- Create: `internal/datadir/datadir.go`

- [ ] **Step 1: Write datadir.go**

```go
package datadir

import (
	"os"
	"path/filepath"
)

func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agentsdx")
}

func ProfilesDir() string      { return filepath.Join(Dir(), "profiles") }
func VaultDir() string         { return filepath.Join(Dir(), "vault") }
func ImagesFile() string       { return filepath.Join(Dir(), "images.json") }
func QEMUDataDir() string      { return filepath.Join(Dir(), "qemu") }
```

- [ ] **Step 2: Commit**

```bash
git add internal/datadir/
git commit -m "feat: add internal/datadir package"
```

---

## Task 4: Migrate vault package

**Files:**
- Create: `internal/vault/vault.go`
- Create: `internal/vault/vault_test.go`

- [ ] **Step 1: Write vault.go**

Copy `server/internal/vault/vault.go` and update the module path and types import:

```go
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/ssh"

	"github.com/duck-labs/agentsdx/internal/types"
)

var hkdfSalt = []byte("agentsdx-vault-v1")

func DeriveKey(secret, profileName string) ([]byte, error) {
	if secret == "" {
		return nil, fmt.Errorf("vault secret must not be empty")
	}
	r := hkdf.New(sha256.New, []byte(secret), hkdfSalt, []byte(profileName))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, fmt.Errorf("hkdf derive key: %w", err)
	}
	return key, nil
}

func Encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func Decrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("gcm open: %w", err)
	}
	return plaintext, nil
}

func vaultPath(dir, profileName string) string {
	return filepath.Join(dir, profileName+".vault.enc")
}

func VaultExists(dir, profileName string) bool {
	_, err := os.Stat(vaultPath(dir, profileName))
	return err == nil
}

func GenerateKeyPair() (privateKeyPEM, publicKeyOpenSSH string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate ed25519 key: %w", err)
	}
	privBlock, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return "", "", fmt.Errorf("marshal private key: %w", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", fmt.Errorf("new ssh public key: %w", err)
	}
	return string(pem.EncodeToMemory(privBlock)), string(ssh.MarshalAuthorizedKey(sshPub)), nil
}

func StoreVaultData(dir, profileName, secret string, data types.VaultData) error {
	plaintext, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("store vault data: marshal: %w", err)
	}
	key, err := DeriveKey(secret, profileName)
	if err != nil {
		return fmt.Errorf("store vault data: derive key: %w", err)
	}
	encrypted, err := Encrypt(key, plaintext)
	if err != nil {
		return fmt.Errorf("store vault data: encrypt: %w", err)
	}
	if err := os.WriteFile(vaultPath(dir, profileName), encrypted, 0600); err != nil {
		return fmt.Errorf("store vault data: write file: %w", err)
	}
	return nil
}

func LoadVaultData(dir, profileName, secret string) (types.VaultData, error) {
	encrypted, err := os.ReadFile(vaultPath(dir, profileName))
	if err != nil {
		return types.VaultData{}, fmt.Errorf("load vault data: read file: %w", err)
	}
	key, err := DeriveKey(secret, profileName)
	if err != nil {
		return types.VaultData{}, fmt.Errorf("load vault data: derive key: %w", err)
	}
	plaintext, err := Decrypt(key, encrypted)
	if err != nil {
		return types.VaultData{}, fmt.Errorf("load vault data: decrypt: %w", err)
	}
	var data types.VaultData
	if err := json.Unmarshal(plaintext, &data); err != nil {
		return types.VaultData{}, fmt.Errorf("load vault data: unmarshal: %w", err)
	}
	return data, nil
}
```

- [ ] **Step 2: Write vault_test.go**

```go
package vault_test

import (
	"os"
	"testing"

	"github.com/duck-labs/agentsdx/internal/types"
	"github.com/duck-labs/agentsdx/internal/vault"
)

func TestStoreAndLoad(t *testing.T) {
	dir := t.TempDir()
	secret := "test-secret"
	profile := "test-profile"

	original := types.VaultData{
		GitPrivateKey:      "git-priv",
		GitPublicKey:       "git-pub",
		VMAccessPrivateKey: "vm-priv",
		VMAccessPublicKey:  "vm-pub",
		Secrets:            map[string]string{"KEY": "VALUE"},
	}

	if err := vault.StoreVaultData(dir, profile, secret, original); err != nil {
		t.Fatalf("StoreVaultData: %v", err)
	}

	loaded, err := vault.LoadVaultData(dir, profile, secret)
	if err != nil {
		t.Fatalf("LoadVaultData: %v", err)
	}

	if loaded.GitPrivateKey != original.GitPrivateKey {
		t.Errorf("GitPrivateKey mismatch: got %q want %q", loaded.GitPrivateKey, original.GitPrivateKey)
	}
	if loaded.Secrets["KEY"] != "VALUE" {
		t.Errorf("Secrets mismatch: got %q want %q", loaded.Secrets["KEY"], "VALUE")
	}
}

func TestVaultExists(t *testing.T) {
	dir := t.TempDir()
	if vault.VaultExists(dir, "missing") {
		t.Error("VaultExists returned true for missing vault")
	}
	data := types.DefaultVaultData()
	_ = vault.StoreVaultData(dir, "present", "secret", data)
	if !vault.VaultExists(dir, "present") {
		t.Error("VaultExists returned false for existing vault")
	}
}

func TestGenerateKeyPair(t *testing.T) {
	priv, pub, err := vault.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if priv == "" || pub == "" {
		t.Error("empty key pair returned")
	}
}

func TestWrongSecret(t *testing.T) {
	dir := t.TempDir()
	_ = vault.StoreVaultData(dir, "p", "right", types.DefaultVaultData())
	_, err := vault.LoadVaultData(dir, "p", "wrong")
	if err == nil {
		t.Error("expected error with wrong secret, got nil")
	}
}

func TestDeriveKeyEmptySecret(t *testing.T) {
	_, err := vault.DeriveKey("", "profile")
	if err == nil {
		t.Error("expected error for empty secret")
	}
}

var _ = os.TempDir // ensure os is used
```

- [ ] **Step 3: Commit**

```bash
git add internal/vault/
git commit -m "feat: migrate vault package to single module"
```

---

## Task 5: Create `internal/profile/` YAML store

**Files:**
- Create: `internal/profile/store.go`
- Create: `internal/profile/store_test.go`

- [ ] **Step 1: Write the failing test**

```go
package profile_test

import (
	"testing"

	"github.com/duck-labs/agentsdx/internal/profile"
	"github.com/duck-labs/agentsdx/internal/types"
)

func TestCreateAndGet(t *testing.T) {
	dir := t.TempDir()
	s := profile.NewStore(dir)

	spec := types.ProfileSpec{
		Name: "test",
		Infrastructure: types.InfrastructureConfig{
			Provider: "local",
			Image:    "ubuntu-24.04",
			Tooling:  []string{"mise"},
		},
		Agent: types.AgentConfig{Provider: "claude"},
	}

	if err := s.Create(spec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Get("test")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "test" || got.Infrastructure.Provider != "local" {
		t.Errorf("unexpected spec: %+v", got)
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	s := profile.NewStore(dir)

	_ = s.Create(types.ProfileSpec{Name: "a", Infrastructure: types.InfrastructureConfig{Provider: "local"}, Agent: types.AgentConfig{Provider: "claude"}})
	_ = s.Create(types.ProfileSpec{Name: "b", Infrastructure: types.InfrastructureConfig{Provider: "hetzner"}, Agent: types.AgentConfig{Provider: "claude"}})

	specs, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(specs))
	}
}

func TestCreateDuplicate(t *testing.T) {
	dir := t.TempDir()
	s := profile.NewStore(dir)
	spec := types.ProfileSpec{Name: "dup", Infrastructure: types.InfrastructureConfig{Provider: "local"}, Agent: types.AgentConfig{Provider: "claude"}}
	_ = s.Create(spec)
	if err := s.Create(spec); err == nil {
		t.Error("expected error on duplicate create")
	}
}

func TestGetMissing(t *testing.T) {
	dir := t.TempDir()
	s := profile.NewStore(dir)
	if _, err := s.Get("missing"); err == nil {
		t.Error("expected error for missing profile")
	}
}

func TestAddProject(t *testing.T) {
	dir := t.TempDir()
	s := profile.NewStore(dir)
	_ = s.Create(types.ProfileSpec{Name: "p", Infrastructure: types.InfrastructureConfig{Provider: "local"}, Agent: types.AgentConfig{Provider: "claude"}})

	proj := types.ProjectConfig{Repo: "https://github.com/example/repo", Path: "~/repo"}
	if err := s.AddProject("p", proj); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	got, _ := s.Get("p")
	if len(got.Projects) != 1 || got.Projects[0].Repo != proj.Repo {
		t.Errorf("unexpected projects: %+v", got.Projects)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/duzzifelipe/Development/duck-labs/agent-sandbox && go test ./internal/profile/... 2>&1 | head -5
```

Expected: `cannot find package` or `no Go files`

- [ ] **Step 3: Write store.go**

```go
package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/duck-labs/agentsdx/internal/types"
)

// Store manages profiles as YAML files in a directory.
type Store struct {
	dir string
}

// NewStore creates a Store backed by dir.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) profilePath(name string) string {
	return filepath.Join(s.dir, name+".yaml")
}

func (s *Store) Create(spec types.ProfileSpec) error {
	if _, err := os.Stat(s.profilePath(spec.Name)); err == nil {
		return fmt.Errorf("profile %q already exists", spec.Name)
	}
	return s.write(spec)
}

func (s *Store) Get(name string) (types.ProfileSpec, error) {
	data, err := os.ReadFile(s.profilePath(name))
	if err != nil {
		if os.IsNotExist(err) {
			return types.ProfileSpec{}, fmt.Errorf("profile %q not found", name)
		}
		return types.ProfileSpec{}, fmt.Errorf("read profile: %w", err)
	}
	var spec types.ProfileSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return types.ProfileSpec{}, fmt.Errorf("unmarshal profile: %w", err)
	}
	return spec, nil
}

func (s *Store) List() ([]types.ProfileSpec, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read profiles dir: %w", err)
	}
	var specs []types.ProfileSpec
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		spec, err := s.Get(name)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func (s *Store) AddProject(name string, proj types.ProjectConfig) error {
	spec, err := s.Get(name)
	if err != nil {
		return err
	}
	spec.Projects = append(spec.Projects, proj)
	return s.write(spec)
}

func (s *Store) write(spec types.ProfileSpec) error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("create profiles dir: %w", err)
	}
	data, err := yaml.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}
	return os.WriteFile(s.profilePath(spec.Name), data, 0600)
}
```

- [ ] **Step 4: Run go mod tidy and tests**

```bash
cd /Users/duzzifelipe/Development/duck-labs/agent-sandbox && go mod tidy && go test ./internal/profile/... -v
```

Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/profile/ go.sum
git commit -m "feat: add YAML-backed profile store"
```

---

## Task 6: Migrate `internal/vm/` package

**Files:**
- Create: `internal/vm/provider.go`
- Create: `internal/vm/images.go`
- Create: `internal/vm/userdata.go`
- Create: `internal/vm/hetzner.go`
- Create: `internal/vm/local.go`
- Create: `internal/vm/hetzner_test.go`
- Create: `internal/vm/userdata_test.go`
- Create: `internal/vm/local_test.go`

- [ ] **Step 1: Write provider.go** (unchanged from server — just update package path)

```go
package vm

import "context"

type VMProvider interface {
	CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error)
	DestroyVM(ctx context.Context, vmID string) error
	GetVM(ctx context.Context, vmID string) (*VM, error)
}

type ImageProvider interface {
	CreateBuildVM(ctx context.Context, baseImage, authorizedKey string) (*VM, error)
	SnapshotVM(ctx context.Context, vmID, snapshotName string) (string, error)
	DestroyBuildVM(ctx context.Context, vmID string) error
}

type CreateVMRequest struct {
	ProfileName   string
	ImageID       string
	AuthorizedKey string
	UserData      string
}

type VM struct {
	ID        string
	IPAddress string
	SSHPort   int
	State     string
}

const (
	VMStateStarting = "starting"
	VMStateRunning  = "running"
	VMStateStopped  = "stopped"
	VMStateUnknown  = "unknown"
)
```

- [ ] **Step 2: Write images.go** (unchanged logic — update import path)

```go
package vm

import (
	"encoding/json"
	"fmt"
	"os"
)

type Provider string

const (
	ProviderHetzner Provider = "hetzner"
	ProviderLocal   Provider = "local"
)

type ImageRecord map[Provider]string

type ImageStore struct {
	path string
}

func NewImageStore(path string) *ImageStore {
	return &ImageStore{path: path}
}

func (s *ImageStore) GetImageID(provider Provider, profileName string) (string, error) {
	records, err := s.load()
	if os.IsNotExist(err) {
		return "", fmt.Errorf("no image built for profile %q: run 'profiles build' first", profileName)
	}
	if err != nil {
		return "", fmt.Errorf("load images: %w", err)
	}
	rec, ok := records[profileName]
	if !ok {
		return "", fmt.Errorf("no image record for profile %q", profileName)
	}
	id := rec[provider]
	if id == "" {
		return "", fmt.Errorf("no %s image built for profile %q", provider, profileName)
	}
	return id, nil
}

func (s *ImageStore) SetImageID(provider Provider, profileName, id string) error {
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
	rec[provider] = id
	records[profileName] = rec
	return s.save(records)
}

func (s *ImageStore) load() (map[string]ImageRecord, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var records map[string]ImageRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parse images.json: %w", err)
	}
	return records, nil
}

func (s *ImageStore) save(records map[string]ImageRecord) error {
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal images: %w", err)
	}
	return os.WriteFile(s.path, data, 0644)
}
```

- [ ] **Step 3: Write userdata.go** (drop sessionID and serverURL)

```go
package vm

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"github.com/duck-labs/agentsdx/internal/types"
)

func BuildUserData(authorizedKey, gitPrivateKey, profileName string, secrets map[string]string, projects []types.ProjectConfig) string {
	encodedKey := base64.StdEncoding.EncodeToString([]byte(gitPrivateKey))

	var extraEnv strings.Builder
	for k, v := range secrets {
		fmt.Fprintf(&extraEnv, "      %s=%s\n", k, v)
	}

	var cloneCmds strings.Builder
	for _, proj := range projects {
		cloneURL := proj.Repo
		if proj.AuthTokenEnv != "" {
			if token, ok := secrets[proj.AuthTokenEnv]; ok && token != "" {
				cloneURL = injectToken(proj.Repo, token)
			}
		}
		fmt.Fprintf(&cloneCmds, "  - su - ubuntu -c \"git clone %s %s\"\n", cloneURL, proj.Path)
	}

	return fmt.Sprintf(`#cloud-config
ssh_authorized_keys:
  - %s
write_files:
  - path: /home/ubuntu/.ssh/id_rsa
    permissions: '0600'
    encoding: b64
    content: %s
  - path: /etc/agentsdx.env
    owner: 'ubuntu:ubuntu'
    permissions: '0600'
    content: |
      AGENTSDX_PROFILE=%s
%sruncmd:
  - mkdir -p /home/ubuntu/.ssh && chmod 700 /home/ubuntu/.ssh && chown -R ubuntu:ubuntu /home/ubuntu/.ssh
%s`, authorizedKey, encodedKey, profileName, extraEnv.String(), cloneCmds.String())
}

func injectToken(repoURL, token string) string {
	u, err := url.Parse(repoURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return repoURL
	}
	u.User = url.User(token)
	return u.String()
}
```

- [ ] **Step 4: Write userdata_test.go**

```go
package vm_test

import (
	"strings"
	"testing"

	"github.com/duck-labs/agentsdx/internal/types"
	"github.com/duck-labs/agentsdx/internal/vm"
)

func TestBuildUserData_basic(t *testing.T) {
	ud := vm.BuildUserData("ssh-ed25519 AAAA", "git-private-key", "myprofile", nil, nil)
	if !strings.Contains(ud, "#cloud-config") {
		t.Error("missing cloud-config header")
	}
	if !strings.Contains(ud, "AGENTSDX_PROFILE=myprofile") {
		t.Error("missing AGENTSDX_PROFILE")
	}
	if strings.Contains(ud, "AGENTSDX_SERVER_URL") {
		t.Error("should not contain AGENTSDX_SERVER_URL")
	}
	if strings.Contains(ud, "AGENTSDX_SESSION_ID") {
		t.Error("should not contain AGENTSDX_SESSION_ID")
	}
}

func TestBuildUserData_secrets(t *testing.T) {
	secrets := map[string]string{"MY_TOKEN": "abc123"}
	ud := vm.BuildUserData("key", "priv", "p", secrets, nil)
	if !strings.Contains(ud, "MY_TOKEN=abc123") {
		t.Errorf("expected MY_TOKEN in userdata, got:\n%s", ud)
	}
}

func TestBuildUserData_project(t *testing.T) {
	projects := []types.ProjectConfig{{Repo: "https://github.com/foo/bar", Path: "~/bar"}}
	ud := vm.BuildUserData("key", "priv", "p", nil, projects)
	if !strings.Contains(ud, "git clone https://github.com/foo/bar ~/bar") {
		t.Errorf("expected git clone in userdata, got:\n%s", ud)
	}
}
```

- [ ] **Step 5: Write hetzner.go** (copy from server/internal/vm/hetzner.go, update import path)

Replace the import:
```go
// old: (no external type imports in hetzner.go)
// new package declaration:
package vm
```

The rest of the file is identical to `server/internal/vm/hetzner.go`. Copy it verbatim and update only:
- Remove any reference to `github.com/duck-labs/agentsdx-server` (there are none in hetzner.go)
- No changes needed to logic

- [ ] **Step 6: Write hetzner_test.go** (copy from server/internal/vm/hetzner_test.go, update import path)

Read `server/internal/vm/hetzner_test.go` and copy it, replacing:
- `github.com/duck-labs/agentsdx-server/internal/vm` → `github.com/duck-labs/agentsdx/internal/vm`

- [ ] **Step 7: Write local.go** (replace *sql.DB with in-memory map)

```go
package vm

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

var knownImages = map[string]string{
	"ubuntu-24.04": "https://cloud-images.ubuntu.com/releases/24.04/release/ubuntu-24.04-server-cloudimg-arm64.img",
	"ubuntu-22.04": "https://cloud-images.ubuntu.com/releases/22.04/release/ubuntu-22.04-server-cloudimg-arm64.img",
	"ubuntu-20.04": "https://cloud-images.ubuntu.com/releases/20.04/release/focal-server-cloudimg-arm64.img",
}

type cmdExecutor interface {
	RunCmd(ctx context.Context, name string, args ...string) error
	StartDetached(logPath, name string, args ...string) error
}

type realCmdExecutor struct{}

func (r *realCmdExecutor) RunCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (r *realCmdExecutor) StartDetached(logPath, name string, args ...string) error {
	f, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("open qemu log: %w", err)
	}
	cmd := exec.Command(name, args...)
	cmd.Stdout = f
	cmd.Stderr = f
	if err := cmd.Start(); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return nil
}

type localVMRecord struct {
	pid         int
	sshPort     int
	overlayPath string
	seedISOPath string
}

// LocalProvider implements VMProvider and ImageProvider using local QEMU VMs.
type LocalProvider struct {
	dataDir string
	exec    cmdExecutor
	mu      sync.Mutex
	vms     map[string]*localVMRecord
}

// NewLocalProvider creates a LocalProvider backed by dataDir.
func NewLocalProvider(dataDir string) *LocalProvider {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		abs = dataDir
	}
	return &LocalProvider{
		dataDir: abs,
		exec:    &realCmdExecutor{},
		vms:     make(map[string]*localVMRecord),
	}
}

func (p *LocalProvider) CreateBuildVM(ctx context.Context, baseImage, authorizedKey string) (*VM, error) {
	tmpDir, err := os.MkdirTemp("", "agentsdx-build-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup := func() { os.RemoveAll(tmpDir) }

	userData := fmt.Sprintf("#cloud-config\nusers:\n  - name: root\n    ssh_authorized_keys:\n      - %s\n", authorizedKey)
	if err := os.WriteFile(filepath.Join(tmpDir, "user-data"), []byte(userData), 0644); err != nil {
		cleanup()
		return nil, fmt.Errorf("write user-data: %w", err)
	}

	metaData := "instance-id: agentsdx-build\nlocal-hostname: agentsdx-build\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "meta-data"), []byte(metaData), 0644); err != nil {
		cleanup()
		return nil, fmt.Errorf("write meta-data: %w", err)
	}

	seedISO := filepath.Join(tmpDir, "seed.iso")
	if err := p.exec.RunCmd(ctx, "hdiutil", "makehybrid", "-o", seedISO, "-joliet", "-iso", "-default-volume-name", "cidata", tmpDir); err != nil {
		cleanup()
		return nil, fmt.Errorf("create seed iso: %w", err)
	}

	resolvedImage, err := p.resolveBaseImage(ctx, baseImage)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("resolve base image: %w", err)
	}

	overlayPath := filepath.Join(tmpDir, "build-overlay.qcow2")
	if err := p.exec.RunCmd(ctx, "qemu-img", "create", "-f", "qcow2", "-b", resolvedImage, "-F", "qcow2", overlayPath); err != nil {
		cleanup()
		return nil, fmt.Errorf("create overlay: %w", err)
	}
	if err := p.exec.RunCmd(ctx, "qemu-img", "resize", overlayPath, "20G"); err != nil {
		cleanup()
		return nil, fmt.Errorf("resize overlay: %w", err)
	}

	port, err := findFreePort()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("find free port: %w", err)
	}

	vmID := uuid.New().String()
	qemuLog := filepath.Join(tmpDir, "qemu.log")
	pidFile := filepath.Join(tmpDir, "qemu.pid")
	log.Printf("local provider: starting build VM %s (logs: %s)", vmID, qemuLog)

	if err := p.exec.StartDetached(qemuLog, "qemu-system-aarch64",
		"-nographic", "-machine", "virt", "-accel", "hvf", "-cpu", "host",
		"-m", "2048", "-smp", "2",
		"-bios", "/opt/homebrew/share/qemu/edk2-aarch64-code.fd",
		"-drive", fmt.Sprintf("if=virtio,format=qcow2,file=%s", overlayPath),
		"-drive", fmt.Sprintf("if=virtio,format=raw,file=%s", seedISO),
		"-device", "virtio-net-pci,netdev=net0",
		"-netdev", fmt.Sprintf("user,id=net0,hostfwd=tcp::%d-:22", port),
		"-pidfile", pidFile,
	); err != nil {
		cleanup()
		return nil, fmt.Errorf("start qemu: %w", err)
	}

	if err := dialSSHPortReady(ctx, port); err != nil {
		cleanup()
		return nil, fmt.Errorf("wait for ssh: %w", err)
	}

	pid, err := readPidFile(pidFile)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("read pid file: %w", err)
	}

	p.mu.Lock()
	p.vms[vmID] = &localVMRecord{pid: pid, sshPort: port, overlayPath: overlayPath, seedISOPath: seedISO}
	p.mu.Unlock()

	return &VM{ID: vmID, IPAddress: "127.0.0.1", SSHPort: port, State: VMStateRunning}, nil
}

func (p *LocalProvider) SnapshotVM(ctx context.Context, vmID, snapshotName string) (string, error) {
	p.mu.Lock()
	rec, ok := p.vms[vmID]
	p.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("vm %q not found", vmID)
	}

	killProcess(rec.pid)

	snapshotsDir := filepath.Join(p.dataDir, "qemu", "snapshots")
	if err := os.MkdirAll(snapshotsDir, 0755); err != nil {
		return "", fmt.Errorf("create snapshots dir: %w", err)
	}

	snapshotPath := filepath.Join(snapshotsDir, snapshotName+".qcow2")
	if err := p.exec.RunCmd(ctx, "qemu-img", "convert", "-O", "qcow2", rec.overlayPath, snapshotPath); err != nil {
		return "", fmt.Errorf("convert overlay: %w", err)
	}

	os.Remove(rec.overlayPath)
	os.Remove(rec.seedISOPath)
	os.Remove(filepath.Dir(rec.overlayPath)) //nolint:errcheck

	p.mu.Lock()
	delete(p.vms, vmID)
	p.mu.Unlock()

	return snapshotPath, nil
}

func (p *LocalProvider) DestroyBuildVM(ctx context.Context, vmID string) error {
	return p.destroyQemuVM(vmID)
}

func (p *LocalProvider) CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error) {
	vmID := uuid.New().String()

	sessionsDir := filepath.Join(p.dataDir, "qemu", "sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		return nil, fmt.Errorf("create sessions dir: %w", err)
	}

	overlayPath := filepath.Join(sessionsDir, vmID+"-overlay.qcow2")
	if err := p.exec.RunCmd(ctx, "qemu-img", "create", "-f", "qcow2", "-b", req.ImageID, "-F", "qcow2", overlayPath); err != nil {
		return nil, fmt.Errorf("create session overlay: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "agentsdx-session-*")
	if err != nil {
		os.Remove(overlayPath)
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup := func() {
		os.Remove(overlayPath)
		os.RemoveAll(tmpDir)
	}

	log.Printf("local provider: session VM %s user-data:\n%s", vmID, req.UserData)
	if err := os.WriteFile(filepath.Join(tmpDir, "user-data"), []byte(req.UserData), 0644); err != nil {
		cleanup()
		return nil, fmt.Errorf("write user-data: %w", err)
	}

	metaData := fmt.Sprintf("instance-id: %s\nlocal-hostname: agentsdx-session\n", vmID)
	if err := os.WriteFile(filepath.Join(tmpDir, "meta-data"), []byte(metaData), 0644); err != nil {
		cleanup()
		return nil, fmt.Errorf("write meta-data: %w", err)
	}

	seedISO := filepath.Join(tmpDir, "seed.iso")
	if err := p.exec.RunCmd(ctx, "hdiutil", "makehybrid", "-o", seedISO, "-joliet", "-iso", "-default-volume-name", "cidata", tmpDir); err != nil {
		cleanup()
		return nil, fmt.Errorf("create seed iso: %w", err)
	}

	port, err := findFreePort()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("find free port: %w", err)
	}

	qemuLog := filepath.Join(tmpDir, "qemu.log")
	pidFile := filepath.Join(tmpDir, "qemu.pid")
	log.Printf("local provider: starting session VM %s (logs: %s)", vmID, qemuLog)

	if err := p.exec.StartDetached(qemuLog, "qemu-system-aarch64",
		"-nographic", "-machine", "virt", "-accel", "hvf", "-cpu", "host",
		"-m", "2048", "-smp", "2",
		"-bios", "/opt/homebrew/share/qemu/edk2-aarch64-code.fd",
		"-drive", fmt.Sprintf("if=virtio,format=qcow2,file=%s", overlayPath),
		"-drive", fmt.Sprintf("if=virtio,format=raw,file=%s", seedISO),
		"-device", "virtio-net-pci,netdev=net0",
		"-netdev", fmt.Sprintf("user,id=net0,hostfwd=tcp::%d-:22", port),
		"-pidfile", pidFile,
	); err != nil {
		cleanup()
		return nil, fmt.Errorf("start qemu: %w", err)
	}

	if err := dialSSHPortReady(ctx, port); err != nil {
		cleanup()
		return nil, fmt.Errorf("wait for ssh: %w", err)
	}

	pid, err := readPidFile(pidFile)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("read pid file: %w", err)
	}

	p.mu.Lock()
	p.vms[vmID] = &localVMRecord{pid: pid, sshPort: port, overlayPath: overlayPath, seedISOPath: seedISO}
	p.mu.Unlock()

	return &VM{ID: vmID, IPAddress: "127.0.0.1", SSHPort: port, State: VMStateStarting}, nil
}

func (p *LocalProvider) GetVM(ctx context.Context, vmID string) (*VM, error) {
	p.mu.Lock()
	rec, ok := p.vms[vmID]
	p.mu.Unlock()
	if !ok {
		return &VM{ID: vmID, State: VMStateUnknown}, nil
	}
	state := VMStateUnknown
	if syscall.Kill(rec.pid, 0) == nil {
		state = VMStateRunning
	}
	return &VM{ID: vmID, IPAddress: "127.0.0.1", SSHPort: rec.sshPort, State: state}, nil
}

func (p *LocalProvider) DestroyVM(ctx context.Context, vmID string) error {
	return p.destroyQemuVM(vmID)
}

func (p *LocalProvider) destroyQemuVM(vmID string) error {
	p.mu.Lock()
	rec, ok := p.vms[vmID]
	if ok {
		delete(p.vms, vmID)
	}
	p.mu.Unlock()
	if !ok {
		return nil
	}
	killProcess(rec.pid)
	os.Remove(rec.overlayPath)
	os.Remove(rec.seedISOPath)
	os.Remove(filepath.Dir(rec.overlayPath)) //nolint:errcheck
	return nil
}

func (p *LocalProvider) resolveBaseImage(ctx context.Context, baseImage string) (string, error) {
	if filepath.IsAbs(baseImage) {
		if _, err := os.Stat(baseImage); err != nil {
			return "", fmt.Errorf("image file not found: %s", baseImage)
		}
		return baseImage, nil
	}
	url, ok := knownImages[baseImage]
	if !ok {
		names := make([]string, 0, len(knownImages))
		for k := range knownImages {
			names = append(names, k)
		}
		sort.Strings(names)
		return "", fmt.Errorf("unknown image %q — use an absolute path or one of: %s", baseImage, strings.Join(names, ", "))
	}
	cacheDir := filepath.Join(p.dataDir, "qemu", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}
	cachePath := filepath.Join(cacheDir, baseImage+".img")
	if _, err := os.Stat(cachePath); err == nil {
		log.Printf("local provider: using cached image %s", cachePath)
		return cachePath, nil
	}
	log.Printf("local provider: downloading %s from %s", baseImage, url)
	if err := downloadToFile(ctx, url, cachePath); err != nil {
		return "", fmt.Errorf("download %s: %w", baseImage, err)
	}
	return cachePath, nil
}

func downloadToFile(ctx context.Context, url, destPath string) error {
	tmpPath := destPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		f.Close()
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.Close()
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		f.Close()
		return fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("write image: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, destPath)
}

func findFreePort() (int, error) {
	for i := 0; i < 10; i++ {
		port := 10000 + rand.Intn(10000)
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			l.Close()
			return port, nil
		}
	}
	return 0, fmt.Errorf("could not find free port after 10 attempts")
}

func dialSSHPortReady(ctx context.Context, port int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("ssh port %d not ready after 3 minutes", port)
}

func readPidFile(path string) (int, error) {
	for i := 0; i < 10; i++ {
		data, err := os.ReadFile(path)
		if err == nil {
			var pid int
			if _, err := fmt.Sscanf(string(data), "%d", &pid); err == nil && pid > 0 {
				return pid, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return 0, fmt.Errorf("could not read pid from %s after 10 attempts", path)
}

func killProcess(pid int) {
	syscall.Kill(pid, syscall.SIGTERM) //nolint:errcheck
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	syscall.Kill(pid, syscall.SIGKILL) //nolint:errcheck
}
```

- [ ] **Step 8: Write local_test.go** (rewrite for in-memory API — old tests used *sql.DB directly)

The old tests construct `&LocalProvider{db: conn, ...}` which is no longer valid. Write new tests using the exported `NewLocalProvider` constructor and the in-memory map:

```go
package vm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalProvider_FindFreePort_ReturnsPortInRange(t *testing.T) {
	port, err := findFreePort()
	if err != nil {
		t.Fatalf("findFreePort() error: %v", err)
	}
	if port < 10000 || port >= 20000 {
		t.Errorf("findFreePort() = %d, want port in [10000, 20000)", port)
	}
}

func TestLocalProvider_GetVM_ProcessAlive_ReturnsRunning(t *testing.T) {
	p := &LocalProvider{dataDir: t.TempDir(), exec: &fakeCmdExecutor{}, vms: make(map[string]*localVMRecord)}
	vmID := "test-alive-vm"
	pid := os.Getpid()
	p.vms[vmID] = &localVMRecord{pid: pid, sshPort: 12345, overlayPath: "/tmp/o.qcow2", seedISOPath: "/tmp/s.iso"}

	vm, err := p.GetVM(context.Background(), vmID)
	if err != nil {
		t.Fatalf("GetVM() error: %v", err)
	}
	if vm.State != VMStateRunning {
		t.Errorf("GetVM().State = %q, want %q", vm.State, VMStateRunning)
	}
	if vm.SSHPort != 12345 {
		t.Errorf("GetVM().SSHPort = %d, want 12345", vm.SSHPort)
	}
}

func TestLocalProvider_GetVM_ProcessDead_ReturnsUnknown(t *testing.T) {
	p := &LocalProvider{dataDir: t.TempDir(), exec: &fakeCmdExecutor{}, vms: make(map[string]*localVMRecord)}
	vmID := "test-dead-vm"
	p.vms[vmID] = &localVMRecord{pid: 99999999, sshPort: 12346, overlayPath: "/tmp/o2.qcow2", seedISOPath: "/tmp/s2.iso"}

	vm, err := p.GetVM(context.Background(), vmID)
	if err != nil {
		t.Fatalf("GetVM() error: %v", err)
	}
	if vm.State != VMStateUnknown {
		t.Errorf("GetVM().State = %q, want %q", vm.State, VMStateUnknown)
	}
}

func TestLocalProvider_GetVM_NotFound_ReturnsUnknown(t *testing.T) {
	p := &LocalProvider{dataDir: t.TempDir(), exec: &fakeCmdExecutor{}, vms: make(map[string]*localVMRecord)}
	vm, err := p.GetVM(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("GetVM() error: %v", err)
	}
	if vm.State != VMStateUnknown {
		t.Errorf("GetVM().State = %q, want %q", vm.State, VMStateUnknown)
	}
}

func TestLocalProvider_ResolveBaseImage_AbsolutePath_Exists(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "base-*.img")
	if err != nil {
		t.Fatalf("create temp img: %v", err)
	}
	f.Close()

	p := &LocalProvider{dataDir: t.TempDir(), exec: &fakeCmdExecutor{}, vms: make(map[string]*localVMRecord)}
	got, err := p.resolveBaseImage(context.Background(), f.Name())
	if err != nil {
		t.Fatalf("resolveBaseImage() error: %v", err)
	}
	if got != f.Name() {
		t.Errorf("got %q, want %q", got, f.Name())
	}
}

func TestLocalProvider_ResolveBaseImage_AbsolutePath_Missing_ReturnsError(t *testing.T) {
	p := &LocalProvider{dataDir: t.TempDir(), exec: &fakeCmdExecutor{}, vms: make(map[string]*localVMRecord)}
	_, err := p.resolveBaseImage(context.Background(), "/nonexistent/path/image.img")
	if err == nil {
		t.Fatal("expected error for missing absolute path, got nil")
	}
}

func TestLocalProvider_ResolveBaseImage_KnownName_HitsCache(t *testing.T) {
	dataDir := t.TempDir()
	cacheDir := filepath.Join(dataDir, "qemu", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	cachedPath := filepath.Join(cacheDir, "ubuntu-24.04.img")
	if err := os.WriteFile(cachedPath, []byte("fake image"), 0644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	p := &LocalProvider{dataDir: dataDir, exec: &fakeCmdExecutor{}, vms: make(map[string]*localVMRecord)}
	got, err := p.resolveBaseImage(context.Background(), "ubuntu-24.04")
	if err != nil {
		t.Fatalf("resolveBaseImage() error: %v", err)
	}
	if got != cachedPath {
		t.Errorf("got %q, want %q", got, cachedPath)
	}
}

func TestLocalProvider_ResolveBaseImage_UnknownName_ReturnsError(t *testing.T) {
	p := &LocalProvider{dataDir: t.TempDir(), exec: &fakeCmdExecutor{}, vms: make(map[string]*localVMRecord)}
	_, err := p.resolveBaseImage(context.Background(), "debian-12")
	if err == nil {
		t.Fatal("expected error for unknown name, got nil")
	}
	if !strings.Contains(err.Error(), "unknown image") {
		t.Errorf("error %q should mention 'unknown image'", err.Error())
	}
}

type fakeCmdExecutor struct {
	runCalls   [][]string
	startCalls [][]string
	runErr     error
	startErr   error
}

func (f *fakeCmdExecutor) RunCmd(_ context.Context, name string, args ...string) error {
	call := append([]string{name}, args...)
	f.runCalls = append(f.runCalls, call)
	return f.runErr
}

func (f *fakeCmdExecutor) StartDetached(logPath, name string, args ...string) error {
	call := append([]string{name}, args...)
	f.startCalls = append(f.startCalls, call)
	return f.startErr
}
```

- [ ] **Step 9: Run go mod tidy and tests**

```bash
cd /Users/duzzifelipe/Development/duck-labs/agent-sandbox && go mod tidy && go test ./internal/vm/... -v -run TestBuildUserData
```

Expected: userdata tests PASS

- [ ] **Step 10: Commit**

```bash
git add internal/vm/ go.sum
git commit -m "feat: migrate vm package, LocalProvider drops SQLite for in-memory map"
```

---

## Task 7: Migrate `internal/builder/` package

**Files:**
- Create: `internal/builder/builder.go`
- Create: `internal/builder/ssh.go`
- Create: `internal/builder/builder_test.go`

- [ ] **Step 1: Write ssh.go**

Copy `server/internal/builder/ssh.go` verbatim — no import changes needed (only uses stdlib and `golang.org/x/crypto/ssh`).

- [ ] **Step 2: Write builder.go**

Copy `server/internal/builder/builder.go` and update imports:
- `github.com/duck-labs/agentsdx-server/internal/vault` → `github.com/duck-labs/agentsdx/internal/vault`
- `github.com/duck-labs/agentsdx-server/internal/vm` → `github.com/duck-labs/agentsdx/internal/vm`
- `github.com/duck-labs/agentsdx-shared/types` → `github.com/duck-labs/agentsdx/internal/types`

All logic remains identical.

- [ ] **Step 3: Write builder_test.go**

Copy `server/internal/builder/builder_test.go` and update imports:
- `github.com/duck-labs/agentsdx-server/internal/builder` → `github.com/duck-labs/agentsdx/internal/builder`
- `github.com/duck-labs/agentsdx-server/internal/vm` → `github.com/duck-labs/agentsdx/internal/vm`
- `github.com/duck-labs/agentsdx-shared/types` → `github.com/duck-labs/agentsdx/internal/types`

- [ ] **Step 4: Run tests**

```bash
cd /Users/duzzifelipe/Development/duck-labs/agent-sandbox && go test ./internal/builder/... -v
```

Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/builder/
git commit -m "feat: migrate builder package to single module"
```

---

## Task 8: Create `internal/session/runner.go`

**Files:**
- Create: `internal/session/runner.go`

- [ ] **Step 1: Write runner.go**

```go
package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/duck-labs/agentsdx/internal/types"
	"github.com/duck-labs/agentsdx/internal/vm"
)

// Run creates a VM for the profile, polls until SSH is ready, spawns an SSH
// child process, and destroys the VM when the child exits or a signal is received.
func Run(ctx context.Context, spec types.ProfileSpec, vaultData types.VaultData, provider vm.VMProvider, images *vm.ImageStore) error {
	imageID, err := images.GetImageID(vm.Provider(spec.Infrastructure.Provider), spec.Name)
	if err != nil {
		return err
	}

	userData := vm.BuildUserData(
		vaultData.VMAccessPublicKey,
		vaultData.GitPrivateKey,
		spec.Name,
		vaultData.Secrets,
		spec.Projects,
	)

	fmt.Printf("Starting VM for profile %q...\n", spec.Name)
	v, err := provider.CreateVM(ctx, vm.CreateVMRequest{
		ProfileName:   spec.Name,
		ImageID:       imageID,
		AuthorizedKey: vaultData.VMAccessPublicKey,
		UserData:      userData,
	})
	if err != nil {
		return fmt.Errorf("create vm: %w", err)
	}

	defer func() {
		fmt.Println("\nDestroying VM...")
		_ = provider.DestroyVM(context.Background(), v.ID)
	}()

	fmt.Printf("Waiting for VM to be ready")
	if err := pollUntilRunning(ctx, provider, v); err != nil {
		return err
	}
	fmt.Println()

	keyFile, err := os.CreateTemp("", "agentsdx-key-*")
	if err != nil {
		return fmt.Errorf("create temp key file: %w", err)
	}
	defer os.Remove(keyFile.Name())

	if _, err := keyFile.WriteString(vaultData.VMAccessPrivateKey); err != nil {
		return fmt.Errorf("write key: %w", err)
	}
	keyFile.Close()
	if err := os.Chmod(keyFile.Name(), 0600); err != nil {
		return fmt.Errorf("chmod key: %w", err)
	}

	sshBin, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("ssh not found in PATH: %w", err)
	}

	sshArgs := []string{
		sshBin,
		"-i", keyFile.Name(),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-t",
	}
	if v.SSHPort != 0 {
		sshArgs = append(sshArgs, "-p", fmt.Sprintf("%d", v.SSHPort))
	}
	sshArgs = append(sshArgs, fmt.Sprintf("ubuntu@%s", v.IPAddress), "/usr/local/bin/entrypoint.sh")

	fmt.Printf("Connecting to %s...\n", v.IPAddress)
	sshCmd := exec.Command(sshArgs[0], sshArgs[1:]...)
	sshCmd.Stdin = os.Stdin
	sshCmd.Stdout = os.Stdout
	sshCmd.Stderr = os.Stderr

	if err := sshCmd.Start(); err != nil {
		return fmt.Errorf("start ssh: %w", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	doneCh := make(chan error, 1)
	go func() { doneCh <- sshCmd.Wait() }()

	select {
	case <-sigCh:
		_ = sshCmd.Process.Signal(syscall.SIGTERM)
		<-doneCh
	case <-doneCh:
	}

	return nil
}

func pollUntilRunning(ctx context.Context, provider vm.VMProvider, v *vm.VM) error {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		current, err := provider.GetVM(ctx, v.ID)
		if err == nil && current.State == vm.VMStateRunning {
			v.IPAddress = current.IPAddress
			v.SSHPort = current.SSHPort
			return nil
		}
		fmt.Print(".")
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("VM did not become ready within 2 minutes")
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/session/
git commit -m "feat: add session runner (create VM, SSH, destroy on exit)"
```

---

## Task 9: Create CLI commands

**Files:**
- Create: `cmd/agentsdx/main.go`
- Create: `cmd/agentsdx/profiles.go`
- Create: `cmd/agentsdx/secrets.go`

- [ ] **Step 1: Write main.go**

```go
package main

import (
	"fmt"
	"os"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"

	"github.com/duck-labs/agentsdx/internal/datadir"
	"github.com/duck-labs/agentsdx/internal/profile"
	"github.com/duck-labs/agentsdx/internal/vm"
)

func main() {
	vaultSecret := os.Getenv("AGENTSDX_VAULT_SECRET")
	if vaultSecret == "" {
		fmt.Fprintln(os.Stderr, "error: AGENTSDX_VAULT_SECRET is not set")
		os.Exit(1)
	}

	if err := os.MkdirAll(datadir.ProfilesDir(), 0700); err != nil {
		fmt.Fprintf(os.Stderr, "error: create profiles dir: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(datadir.VaultDir(), 0700); err != nil {
		fmt.Fprintf(os.Stderr, "error: create vault dir: %v\n", err)
		os.Exit(1)
	}

	vmProviders := map[string]vm.VMProvider{}
	imageProviders := map[string]vm.ImageProvider{}

	if token := os.Getenv("AGENTSDX_HCLOUD_TOKEN"); token != "" {
		client := hcloud.NewClient(hcloud.WithToken(token))
		location := os.Getenv("AGENTSDX_HCLOUD_LOCATION")
		hp := vm.NewHetznerProvider(client, location)
		vmProviders["hetzner"] = hp
		imageProviders["hetzner"] = hp
	}

	lp := vm.NewLocalProvider(datadir.Dir())
	vmProviders["local"] = lp
	imageProviders["local"] = lp

	profiles := profile.NewStore(datadir.ProfilesDir())
	images := vm.NewImageStore(datadir.ImagesFile())

	root := &cobra.Command{
		Use:   "agentsdx",
		Short: "Manage agent sandboxes",
	}

	root.AddCommand(
		newProfilesCmd(profiles, images, vmProviders, imageProviders, vaultSecret),
		newSecretsCmd(vaultSecret),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Write profiles.go**

```go
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/AlecAivazis/survey/v2"
	"github.com/spf13/cobra"

	"github.com/duck-labs/agentsdx/internal/profile"
	"github.com/duck-labs/agentsdx/internal/session"
	"github.com/duck-labs/agentsdx/internal/builder"
	"github.com/duck-labs/agentsdx/internal/types"
	"github.com/duck-labs/agentsdx/internal/vault"
	"github.com/duck-labs/agentsdx/internal/vm"
	"github.com/duck-labs/agentsdx/internal/datadir"
)

func newProfilesCmd(
	profiles *profile.Store,
	images *vm.ImageStore,
	vmProviders map[string]vm.VMProvider,
	imageProviders map[string]vm.ImageProvider,
	vaultSecret string,
) *cobra.Command {
	parent := &cobra.Command{
		Use:   "profiles",
		Short: "Manage sandbox profiles",
	}
	parent.AddCommand(newProfilesListCmd(profiles))
	parent.AddCommand(newProfilesCreateCmd(profiles))
	parent.AddCommand(newProfilesRunCmd(profiles, images, vmProviders, vaultSecret))
	parent.AddCommand(newProfilesBuildCmd(profiles, images, imageProviders, vaultSecret))
	parent.AddCommand(newProfilesRepoCmd(profiles))
	return parent
}

func newProfilesListCmd(profiles *profile.Store) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List sandbox profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			specs, err := profiles.List()
			if err != nil {
				return err
			}
			if len(specs) == 0 {
				fmt.Println("No profiles found. Run 'agentsdx profiles create' to create one.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tPROVIDER\tAGENT\tPROJECTS")
			for _, p := range specs {
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", p.Name, p.Infrastructure.Provider, p.Agent.Provider, len(p.Projects))
			}
			return w.Flush()
		},
	}
}

func newProfilesCreateCmd(profiles *profile.Store) *cobra.Command {
	return &cobra.Command{
		Use:   "create",
		Short: "Create a new sandbox profile interactively",
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := runWizard()
			if err != nil {
				return err
			}
			if err := profiles.Create(spec); err != nil {
				return fmt.Errorf("create profile: %w", err)
			}
			fmt.Printf("Profile %q created.\n", spec.Name)
			return nil
		},
	}
}

func newProfilesRunCmd(profiles *profile.Store, images *vm.ImageStore, vmProviders map[string]vm.VMProvider, vaultSecret string) *cobra.Command {
	return &cobra.Command{
		Use:   "run <profile>",
		Short: "Start a sandbox session and open an SSH connection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			spec, err := profiles.Get(name)
			if err != nil {
				return err
			}

			provider, ok := vmProviders[spec.Infrastructure.Provider]
			if !ok {
				return fmt.Errorf("provider %q not configured — check environment variables", spec.Infrastructure.Provider)
			}

			vaultData, err := loadOrInitVault(name, vaultSecret)
			if err != nil {
				return err
			}

			return session.Run(context.Background(), spec, vaultData, provider, images)
		},
	}
}

func newProfilesBuildCmd(profiles *profile.Store, images *vm.ImageStore, imageProviders map[string]vm.ImageProvider, vaultSecret string) *cobra.Command {
	return &cobra.Command{
		Use:   "build <profile>",
		Short: "Build a VM image for a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			spec, err := profiles.Get(name)
			if err != nil {
				return err
			}

			provider, ok := imageProviders[spec.Infrastructure.Provider]
			if !ok {
				return fmt.Errorf("image provider %q not configured — check environment variables", spec.Infrastructure.Provider)
			}

			vmDir := "vm"
			b := builder.New(vmDir, images, map[string]vm.ImageProvider{spec.Infrastructure.Provider: provider})

			fmt.Printf("Building image for profile %q...\n", name)
			snapshotID, err := b.Build(context.Background(), spec)
			if err != nil {
				return fmt.Errorf("build: %w", err)
			}
			fmt.Printf("Build complete: %s\n", snapshotID)
			return nil
		},
	}
}

func newProfilesRepoCmd(profiles *profile.Store) *cobra.Command {
	parent := &cobra.Command{
		Use:   "repo",
		Short: "Manage repositories in a profile",
	}
	parent.AddCommand(newProfilesRepoAddCmd(profiles))
	return parent
}

func newProfilesRepoAddCmd(profiles *profile.Store) *cobra.Command {
	var authTokenEnv string
	cmd := &cobra.Command{
		Use:   "add <profile> <repo-url> [path]",
		Short: "Add a repository to a profile",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName := args[0]
			repoURL := args[1]

			var mountPath string
			if len(args) == 3 {
				mountPath = args[2]
			} else {
				name, err := repoNameFromURL(repoURL)
				if err != nil {
					return fmt.Errorf("cannot derive path from %q: %w — provide an explicit path", repoURL, err)
				}
				mountPath = "~/" + name
			}

			proj := types.ProjectConfig{
				Repo:         repoURL,
				Path:         mountPath,
				AuthTokenEnv: authTokenEnv,
			}
			if err := profiles.AddProject(profileName, proj); err != nil {
				return fmt.Errorf("add project: %w", err)
			}
			fmt.Printf("Repository %q added to profile %q (path: %s).\n", repoURL, profileName, mountPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&authTokenEnv, "auth-token-env", "", "Name of the secret whose value is used as git auth token")
	return cmd
}

func runWizard() (types.ProfileSpec, error) {
	var spec types.ProfileSpec

	if err := survey.AskOne(&survey.Input{
		Message: "Profile name:",
		Help:    "Unique name for this sandbox (e.g. work-backend)",
	}, &spec.Name, survey.WithValidator(survey.Required)); err != nil {
		return spec, err
	}

	if err := survey.AskOne(&survey.Select{
		Message: "VM provider:",
		Options: []string{"local", "hetzner"},
		Default: "local",
	}, &spec.Infrastructure.Provider); err != nil {
		return spec, err
	}

	if err := survey.AskOne(&survey.Select{
		Message: "Base OS image:",
		Options: []string{"ubuntu-24.04"},
		Default: "ubuntu-24.04",
	}, &spec.Infrastructure.Image); err != nil {
		return spec, err
	}

	if err := survey.AskOne(&survey.MultiSelect{
		Message: "Tooling to install:",
		Options: []string{"mise", "docker", "docker-compose", "gh"},
	}, &spec.Infrastructure.Tooling); err != nil {
		return spec, err
	}

	if err := survey.AskOne(&survey.Select{
		Message: "Agent:",
		Options: []string{"claude", "opencode", "hermes"},
		Default: "claude",
	}, &spec.Agent.Provider); err != nil {
		return spec, err
	}

	var skillsInput string
	if err := survey.AskOne(&survey.Input{
		Message: "Skills (comma-separated, optional):",
		Help:    "e.g. superpowers/brainstorming,superpowers/tdd",
	}, &skillsInput); err != nil {
		return spec, err
	}
	if skillsInput != "" {
		for _, s := range strings.Split(skillsInput, ",") {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				spec.Agent.Skills = append(spec.Agent.Skills, trimmed)
			}
		}
	}

	return spec, nil
}

func loadOrInitVault(profileName, vaultSecret string) (types.VaultData, error) {
	vaultDir := datadir.VaultDir()
	if vault.VaultExists(vaultDir, profileName) {
		return vault.LoadVaultData(vaultDir, profileName, vaultSecret)
	}

	vmPriv, vmPub, err := vault.GenerateKeyPair()
	if err != nil {
		return types.VaultData{}, fmt.Errorf("generate vm key pair: %w", err)
	}
	gitPriv, gitPub, err := vault.GenerateKeyPair()
	if err != nil {
		return types.VaultData{}, fmt.Errorf("generate git key pair: %w", err)
	}
	vd := types.DefaultVaultData()
	vd.VMAccessPrivateKey = vmPriv
	vd.VMAccessPublicKey = vmPub
	vd.GitPrivateKey = gitPriv
	vd.GitPublicKey = gitPub
	if err := vault.StoreVaultData(vaultDir, profileName, vaultSecret, vd); err != nil {
		return types.VaultData{}, fmt.Errorf("init vault: %w", err)
	}
	return vd, nil
}

func repoNameFromURL(rawURL string) (string, error) {
	rawURL = strings.TrimSuffix(rawURL, "/")
	var segment string
	if idx := strings.LastIndex(rawURL, "/"); idx >= 0 {
		segment = rawURL[idx+1:]
	} else {
		segment = rawURL
	}
	name := strings.TrimSuffix(segment, ".git")
	if name == "" {
		return "", fmt.Errorf("could not extract repo name from URL")
	}
	return name, nil
}
```

- [ ] **Step 3: Write secrets.go**

```go
package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/duck-labs/agentsdx/internal/datadir"
	"github.com/duck-labs/agentsdx/internal/vault"
)

func newSecretsCmd(vaultSecret string) *cobra.Command {
	parent := &cobra.Command{
		Use:   "secrets",
		Short: "Manage per-profile secrets",
	}
	parent.AddCommand(newSecretsSetCmd(vaultSecret))
	parent.AddCommand(newSecretsDeleteCmd(vaultSecret))
	parent.AddCommand(newSecretsListCmd(vaultSecret))
	return parent
}

func newSecretsSetCmd(vaultSecret string) *cobra.Command {
	return &cobra.Command{
		Use:   "set <profile> <KEY> <VALUE>",
		Short: "Set or overwrite a secret for a profile",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName, key, value := args[0], args[1], args[2]
			vd, err := loadOrInitVault(profileName, vaultSecret)
			if err != nil {
				return err
			}
			if vd.Secrets == nil {
				vd.Secrets = make(map[string]string)
			}
			vd.Secrets[key] = value
			if err := vault.StoreVaultData(datadir.VaultDir(), profileName, vaultSecret, vd); err != nil {
				return fmt.Errorf("store vault: %w", err)
			}
			fmt.Printf("Secret %q set for profile %q.\n", key, profileName)
			return nil
		},
	}
}

func newSecretsDeleteCmd(vaultSecret string) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <profile> <KEY>",
		Short: "Remove a secret from a profile",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName, key := args[0], args[1]
			if !vault.VaultExists(datadir.VaultDir(), profileName) {
				fmt.Printf("Secret %q deleted from profile %q.\n", key, profileName)
				return nil
			}
			vd, err := vault.LoadVaultData(datadir.VaultDir(), profileName, vaultSecret)
			if err != nil {
				return err
			}
			delete(vd.Secrets, key)
			if err := vault.StoreVaultData(datadir.VaultDir(), profileName, vaultSecret, vd); err != nil {
				return fmt.Errorf("store vault: %w", err)
			}
			fmt.Printf("Secret %q deleted from profile %q.\n", key, profileName)
			return nil
		},
	}
}

func newSecretsListCmd(vaultSecret string) *cobra.Command {
	return &cobra.Command{
		Use:   "list <profile>",
		Short: "List secret key names for a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName := args[0]
			if !vault.VaultExists(datadir.VaultDir(), profileName) {
				fmt.Println("No secrets set.")
				return nil
			}
			vd, err := vault.LoadVaultData(datadir.VaultDir(), profileName, vaultSecret)
			if err != nil {
				return err
			}
			if len(vd.Secrets) == 0 {
				fmt.Println("No secrets set.")
				return nil
			}
			for k := range vd.Secrets {
				fmt.Println(k)
			}
			return nil
		},
	}
}

```

- [ ] **Step 4: Run go mod tidy and build**

```bash
cd /Users/duzzifelipe/Development/duck-labs/agent-sandbox && go mod tidy && go build ./cmd/agentsdx/
```

Expected: binary builds with no errors

- [ ] **Step 5: Smoke test the binary**

```bash
./agentsdx --help
./agentsdx profiles --help
./agentsdx secrets --help
```

Expected: help text prints for all commands with correct subcommand structure

- [ ] **Step 6: Commit**

```bash
git add cmd/agentsdx/ go.sum
git commit -m "feat: add single-binary CLI commands"
```

---

## Task 10: Update mise.toml and run all tests

**Files:**
- Modify: `mise.toml`

- [ ] **Step 1: Write updated mise.toml**

```toml
[tools]
go = "1.26.3"

[env]
_.file = ".env"

[tasks.build]
description = "Build agentsdx"
run = "go build -o dist/agentsdx ./cmd/agentsdx"

[tasks.test]
description = "Run all tests"
run = "go test -count=1 ./..."
```

- [ ] **Step 2: Run all tests**

```bash
cd /Users/duzzifelipe/Development/duck-labs/agent-sandbox && go test -count=1 ./...
```

Expected: all tests PASS (vm package tests that require QEMU/hdiutil may be skipped or require the build tools installed)

- [ ] **Step 3: Build the binary to dist/**

```bash
mkdir -p dist && go build -o dist/agentsdx ./cmd/agentsdx
```

Expected: `dist/agentsdx` is created

- [ ] **Step 4: Commit**

```bash
git add mise.toml dist/.gitignore
git commit -m "chore: simplify mise.toml for single-module build"
```

---

## Task 11: Delete old directories

- [ ] **Step 1: Verify the new binary works**

```bash
AGENTSDX_VAULT_SECRET=test ./dist/agentsdx profiles list
```

Expected: `No profiles found.` (not an error about AGENTSDX_URL)

- [ ] **Step 2: Delete cli/, server/, shared/**

```bash
rm -rf cli/ server/ shared/
```

- [ ] **Step 3: Verify tests still pass**

```bash
cd /Users/duzzifelipe/Development/duck-labs/agent-sandbox && go test -count=1 ./...
```

Expected: all tests PASS

- [ ] **Step 4: Build final binary**

```bash
go build -o dist/agentsdx ./cmd/agentsdx && ./dist/agentsdx --help
```

Expected: binary builds and help text prints

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: delete cli/, server/, shared/ — single agentsdx binary complete"
```
