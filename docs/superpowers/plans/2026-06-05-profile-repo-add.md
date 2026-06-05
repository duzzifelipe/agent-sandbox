# Profile Repo Add Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove repository config from the `profiles create` wizard and add `agentsdx profiles repo add <profile> <repo-url> [path] [--auth-token-env <secret>]` as a standalone command that stores the repo in the profile and clones it at VM boot via cloud-init.

**Architecture:** `ProjectConfig` gains an `AuthTokenEnv` field. A new `POST /profiles/{name}/projects` endpoint appends a project to an existing profile via a new `Store.AddProject` method. `BuildUserData` grows a `projects` parameter and emits `git clone` commands in `runcmd`, embedding auth tokens from the secrets map when `AuthTokenEnv` is set.

**Tech Stack:** Go 1.26, cobra (CLI), chi (HTTP router), SQLite (profile store), cloud-init user-data (YAML)

---

### Task 1: Add `AuthTokenEnv` to `ProjectConfig`

**Files:**
- Modify: `shared/types/profile.go`
- Modify: `shared/types/profile_test.go`

- [ ] **Step 1: Write a failing test for `AuthTokenEnv` YAML roundtrip**

Add to `shared/types/profile_test.go`:

```go
func TestProjectConfig_AuthTokenEnvRoundtrip(t *testing.T) {
	input := `
name: myprofile
infrastructure:
  provider: hetzner
  image: ubuntu-24.04
projects:
  - repo: https://github.com/org/api.git
    path: ~/api
    auth_token_env: GITHUB_TOKEN
agent:
  provider: claude
`
	var got types.ProfileSpec
	if err := yaml.Unmarshal([]byte(input), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(got.Projects))
	}
	if got.Projects[0].AuthTokenEnv != "GITHUB_TOKEN" {
		t.Errorf("AuthTokenEnv: got %q, want %q", got.Projects[0].AuthTokenEnv, "GITHUB_TOKEN")
	}
	out, err := yaml.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got2 types.ProfileSpec
	if err := yaml.Unmarshal(out, &got2); err != nil {
		t.Fatalf("second unmarshal: %v", err)
	}
	if got2.Projects[0].AuthTokenEnv != "GITHUB_TOKEN" {
		t.Errorf("roundtrip AuthTokenEnv: got %q, want %q", got2.Projects[0].AuthTokenEnv, "GITHUB_TOKEN")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd shared && go test ./types/... -run TestProjectConfig_AuthTokenEnvRoundtrip -v
```

Expected: FAIL — `AuthTokenEnv` field does not exist yet.

- [ ] **Step 3: Add `AuthTokenEnv` to `ProjectConfig`**

In `shared/types/profile.go`, replace `ProjectConfig`:

```go
type ProjectConfig struct {
	Repo         string `yaml:"repo"           json:"repo"`
	Path         string `yaml:"path"           json:"path"`
	AuthTokenEnv string `yaml:"auth_token_env" json:"auth_token_env,omitempty"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd shared && go test ./types/... -v
```

Expected: PASS — all tests including the new roundtrip test.

- [ ] **Step 5: Commit**

```bash
git add shared/types/profile.go shared/types/profile_test.go
git commit -m "feat(types): add AuthTokenEnv to ProjectConfig"
```

---

### Task 2: Add `Store.AddProject`

**Files:**
- Modify: `server/internal/profile/store.go`
- Modify: `server/internal/profile/store_test.go`

- [ ] **Step 1: Write failing tests for `AddProject`**

Add to `server/internal/profile/store_test.go`:

```go
func TestStore_AddProject_AppendsProject(t *testing.T) {
	s := newStore(t)
	spec := sampleSpec("my-profile")
	// sampleSpec already has 1 project
	if err := s.Create(spec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	newProj := types.ProjectConfig{
		Repo:         "https://github.com/org/backend.git",
		Path:         "~/backend",
		AuthTokenEnv: "GITHUB_TOKEN",
	}
	if err := s.AddProject("my-profile", newProj); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	got, err := s.Get("my-profile")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d: %+v", len(got.Projects), got.Projects)
	}
	last := got.Projects[len(got.Projects)-1]
	if last.Repo != newProj.Repo || last.Path != newProj.Path || last.AuthTokenEnv != newProj.AuthTokenEnv {
		t.Errorf("unexpected last project: %+v", last)
	}
}

func TestStore_AddProject_ProfileNotFound(t *testing.T) {
	s := newStore(t)
	proj := types.ProjectConfig{Repo: "https://github.com/org/api.git", Path: "~/api"}
	if err := s.AddProject("no-such", proj); err == nil {
		t.Error("expected error for missing profile, got nil")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd server && go test ./internal/profile/... -run "TestStore_AddProject" -v
```

Expected: FAIL — `AddProject` method does not exist.

- [ ] **Step 3: Implement `Store.AddProject`**

Add to `server/internal/profile/store.go`:

```go
func (s *Store) AddProject(name string, proj types.ProjectConfig) error {
	spec, err := s.Get(name)
	if err != nil {
		return err
	}
	spec.Projects = append(spec.Projects, proj)
	data, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshal profile spec: %w", err)
	}
	_, err = s.db.Exec(`UPDATE profiles SET spec = ? WHERE name = ?`, string(data), name)
	if err != nil {
		return fmt.Errorf("update profile: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd server && go test ./internal/profile/... -v
```

Expected: PASS — all profile store tests.

- [ ] **Step 5: Commit**

```bash
git add server/internal/profile/store.go server/internal/profile/store_test.go
git commit -m "feat(profile): add Store.AddProject method"
```

---

### Task 3: Add `POST /profiles/{name}/projects` API handler

**Files:**
- Modify: `server/internal/api/handler.go`
- Modify: `server/internal/api/handler_test.go`

- [ ] **Step 1: Write failing tests for the new endpoint**

Add to `server/internal/api/handler_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd server && go test ./internal/api/... -run "TestAddProject" -v
```

Expected: FAIL — route does not exist (404 from chi).

- [ ] **Step 3: Register the route in `Router()`**

In `server/internal/api/handler.go`, add inside `Router()` after the existing profile routes:

```go
r.Post("/profiles/{name}/projects", h.addProject)
```

- [ ] **Step 4: Implement the `addProject` handler**

Add to `server/internal/api/handler.go`:

```go
func (h *Handler) addProject(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var proj types.ProjectConfig
	if err := json.NewDecoder(r.Body).Decode(&proj); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if proj.Repo == "" {
		writeError(w, http.StatusBadRequest, "repo is required")
		return
	}
	if err := h.profiles.AddProject(name, proj); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd server && go test ./internal/api/... -v
```

Expected: PASS — all handler tests.

- [ ] **Step 6: Commit**

```bash
git add server/internal/api/handler.go server/internal/api/handler_test.go
git commit -m "feat(api): add POST /profiles/{name}/projects endpoint"
```

---

### Task 4: Extend `BuildUserData` with clone commands and update callers

**Files:**
- Modify: `server/internal/vm/userdata.go`
- Modify: `server/internal/vm/userdata_test.go`
- Modify: `server/internal/session/manager.go`

- [ ] **Step 1: Write failing tests for the new clone behaviour**

Add to `server/internal/vm/userdata_test.go`. Also update the import block to include the shared types package:

```go
import (
	"strings"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/duck-labs/agentsdx-shared/types"
)
```

Add the new test functions:

```go
func TestBuildUserData_ClonesProjects(t *testing.T) {
	projects := []types.ProjectConfig{
		{Repo: "https://github.com/org/api.git", Path: "~/api"},
	}
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-1", "http://server:8080", "myprofile", nil, projects)
	if !strings.Contains(ud, `git clone https://github.com/org/api.git ~/api`) {
		t.Errorf("user-data missing git clone command, got:\n%s", ud)
	}
}

func TestBuildUserData_InjectsTokenInCloneURL(t *testing.T) {
	projects := []types.ProjectConfig{
		{Repo: "https://github.com/org/private.git", Path: "~/private", AuthTokenEnv: "GITHUB_TOKEN"},
	}
	secrets := map[string]string{"GITHUB_TOKEN": "ghp_abc123"}
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-1", "http://server:8080", "myprofile", secrets, projects)
	if !strings.Contains(ud, "git clone https://ghp_abc123@github.com/org/private.git ~/private") {
		t.Errorf("user-data missing authenticated clone, got:\n%s", ud)
	}
}

func TestBuildUserData_MissingTokenSecretClonesWithoutAuth(t *testing.T) {
	projects := []types.ProjectConfig{
		{Repo: "https://github.com/org/private.git", Path: "~/private", AuthTokenEnv: "GITHUB_TOKEN"},
	}
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-1", "http://server:8080", "myprofile", nil, projects)
	if !strings.Contains(ud, "git clone https://github.com/org/private.git ~/private") {
		t.Errorf("expected unauthenticated clone, got:\n%s", ud)
	}
	if strings.Contains(ud, "@github.com") {
		t.Errorf("should not have token in URL when secret is missing")
	}
}

func TestBuildUserData_NoProjectsNoCloneCommands(t *testing.T) {
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-1", "http://server:8080", "myprofile", nil, nil)
	if strings.Contains(ud, "git clone") {
		t.Errorf("unexpected git clone in user-data with no projects")
	}
}
```

- [ ] **Step 2: Update the four existing test calls to pass `nil` as the new last arg**

In `server/internal/vm/userdata_test.go`, every existing call to `vm.BuildUserData` has 6 arguments. Add `nil` as a 7th argument to each:

```go
// TestBuildUserData_ContainsSSHKey
ud := vm.BuildUserData("ssh-rsa AAAA...", "-----BEGIN OPENSSH PRIVATE KEY-----\nABC\n-----END OPENSSH PRIVATE KEY-----", "sess-1", "http://server:8080", "myprofile", nil, nil)

// TestBuildUserData_ContainsEnvFile
ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-42", "http://server:8080", "work-backend", nil, nil)

// TestBuildUserData_InjectsSecrets
ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-1", "http://server:8080", "myprofile", secrets, nil)

// TestBuildUserData_NoSecretsOmitsExtraLines
ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-1", "http://server:8080", "myprofile", nil, nil)
```

- [ ] **Step 3: Rewrite `server/internal/vm/userdata.go` with the new signature**

Replace the entire file:

```go
package vm

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"github.com/duck-labs/agentsdx-shared/types"
)

// BuildUserData returns cloud-init user-data that injects SSH keys, agent env vars,
// per-profile secrets, and git clone commands for each project.
func BuildUserData(authorizedKey, gitPrivateKey, sessionID, serverURL, profileName string, secrets map[string]string, projects []types.ProjectConfig) string {
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
    permissions: '0644'
    content: |
      AGENTSDX_SERVER_URL=%s
      AGENTSDX_SESSION_ID=%s
      AGENTSDX_PROFILE=%s
%sruncmd:
  - mkdir -p /home/ubuntu/.ssh && chmod 700 /home/ubuntu/.ssh && chown -R ubuntu:ubuntu /home/ubuntu/.ssh
%s`, authorizedKey, encodedKey, serverURL, sessionID, profileName, extraEnv.String(), cloneCmds.String())
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

- [ ] **Step 4: Update `manager.go` to pass `spec.Projects`**

In `server/internal/session/manager.go`, update the `BuildUserData` call inside `Start`:

```go
UserData: vm.BuildUserData(
	vaultData.VMAccessPublicKey,
	vaultData.GitPrivateKey,
	id,
	m.serverURL,
	profileName,
	vaultData.Secrets,
	spec.Projects,
),
```

- [ ] **Step 5: Run all server tests**

```bash
cd server && go test ./... -v
```

Expected: PASS — all tests, including the new clone tests.

- [ ] **Step 6: Commit**

```bash
git add server/internal/vm/userdata.go server/internal/vm/userdata_test.go server/internal/session/manager.go
git commit -m "feat(vm): add git clone commands to cloud-init user-data"
```

---

### Task 5: Add `Client.AddProject`

**Files:**
- Modify: `cli/internal/client/client.go`
- Modify: `cli/internal/client/client_test.go`

- [ ] **Step 1: Write a failing test for `AddProject`**

Add to `cli/internal/client/client_test.go`:

```go
func TestAddProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/profiles/myprofile/projects" || r.Method != http.MethodPost {
			http.Error(w, "unexpected", 400)
			return
		}
		var proj types.ProjectConfig
		if err := json.NewDecoder(r.Body).Decode(&proj); err != nil {
			http.Error(w, "decode", 400)
			return
		}
		if proj.Repo != "https://github.com/org/api.git" {
			http.Error(w, "wrong repo", 400)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := client.New(srv.URL)
	proj := types.ProjectConfig{
		Repo:         "https://github.com/org/api.git",
		Path:         "~/api",
		AuthTokenEnv: "GITHUB_TOKEN",
	}
	if err := c.AddProject("myprofile", proj); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd cli && go test ./internal/client/... -run TestAddProject -v
```

Expected: FAIL — `AddProject` method does not exist.

- [ ] **Step 3: Implement `Client.AddProject`**

Add to `cli/internal/client/client.go`:

```go
func (c *Client) AddProject(profile string, proj types.ProjectConfig) error {
	body, _ := json.Marshal(proj)
	resp, err := c.http.Post(c.base+"/profiles/"+profile+"/projects", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd cli && go test ./internal/client/... -v
```

Expected: PASS — all client tests.

- [ ] **Step 5: Commit**

```bash
git add cli/internal/client/client.go cli/internal/client/client_test.go
git commit -m "feat(client): add AddProject method"
```

---

### Task 6: CLI — remove wizard loop, add `profiles repo add`

**Files:**
- Modify: `cli/cmd/agentsdx/profiles.go`

- [ ] **Step 1: Remove the project loop from `runWizard`**

In `cli/cmd/agentsdx/profiles.go`, delete lines 102–129 (the entire `for { addProject }` block):

```go
	for {
		var addProject bool
		if err := survey.AskOne(&survey.Confirm{
			Message: "Add a project repository?",
			Default: false,
		}, &addProject); err != nil {
			return spec, err
		}
		if !addProject {
			break
		}
		var proj types.ProjectConfig
		if err := survey.Ask([]*survey.Question{
			{
				Name:     "repo",
				Prompt:   &survey.Input{Message: "Repo URL (e.g. https://github.com/org/repo.git):"},
				Validate: survey.Required,
			},
			{
				Name:     "path",
				Prompt:   &survey.Input{Message: "Mount path in VM (e.g. ~/projects/api):"},
				Validate: survey.Required,
			},
		}, &proj); err != nil {
			return spec, err
		}
		spec.Projects = append(spec.Projects, proj)
	}
```

The `runWizard` function should end immediately after the skills block (returning `spec, nil`).

- [ ] **Step 2: Register `profiles repo` in `newProfilesCmd`**

In `newProfilesCmd`, add the new subcommand group:

```go
func newProfilesCmd(c *client.Client) *cobra.Command {
	parent := &cobra.Command{
		Use:   "profiles",
		Short: "Interact with profiles",
	}
	parent.AddCommand(newProfilesListCmd(c))
	parent.AddCommand(newProfilesBuildCmd(c))
	parent.AddCommand(newCreateProfileCmd(c))
	parent.AddCommand(newProfilesRepoCmd(c))
	return parent
}
```

- [ ] **Step 3: Add `newProfilesRepoCmd`, `newProfilesRepoAddCmd`, and `repoNameFromURL`**

Add to `cli/cmd/agentsdx/profiles.go`:

```go
func newProfilesRepoCmd(c *client.Client) *cobra.Command {
	parent := &cobra.Command{
		Use:   "repo",
		Short: "Manage repositories in a profile",
	}
	parent.AddCommand(newProfilesRepoAddCmd(c))
	return parent
}

func newProfilesRepoAddCmd(c *client.Client) *cobra.Command {
	var authTokenEnv string
	cmd := &cobra.Command{
		Use:   "add <profile> <repo-url> [path]",
		Short: "Add a repository to a profile",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := args[0]
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
			if err := c.AddProject(profile, proj); err != nil {
				return fmt.Errorf("add project: %w", err)
			}
			fmt.Printf("Repository %q added to profile %q (path: %s).\n", repoURL, profile, mountPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&authTokenEnv, "auth-token-env", "", "Name of the secret whose value is used as git auth token")
	return cmd
}

func repoNameFromURL(rawURL string) (string, error) {
	rawURL = strings.TrimSuffix(rawURL, "/")
	var segment string
	if idx := strings.LastIndex(rawURL, "/"); idx >= 0 {
		segment = rawURL[idx+1:]
	} else if idx := strings.LastIndex(rawURL, ":"); idx >= 0 {
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

- [ ] **Step 4: Build and vet**

```bash
cd cli && go build ./... && go vet ./...
```

Expected: no errors.

- [ ] **Step 5: Verify the command tree**

```bash
cd cli && go run ./cmd/agentsdx profiles repo --help
```

Expected output includes `add` subcommand:
```
Manage repositories in a profile

Usage:
  agentsdx profiles repo [command]

Available Commands:
  add         Add a repository to a profile
```

- [ ] **Step 6: Commit**

```bash
git add cli/cmd/agentsdx/profiles.go
git commit -m "feat(cli): add profiles repo add command, remove repos from create wizard"
```

---

### Task 7: Final verification

- [ ] **Step 1: Run all tests across all modules**

```bash
cd shared && go test ./... && cd ../server && go test ./... && cd ../cli && go test ./...
```

Expected: PASS everywhere, no compilation errors.

- [ ] **Step 2: Smoke-test the CLI help output**

```bash
cd cli && go run ./cmd/agentsdx profiles --help
go run ./cmd/agentsdx profiles repo add --help
```

Expected: `profiles create` no longer mentions repositories; `profiles repo add` shows `<profile> <repo-url> [path]` usage with `--auth-token-env` flag.
