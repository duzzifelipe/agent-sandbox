# Profile Repo Add — Design Spec

**Date:** 2026-06-05

## Summary

Remove git repository configuration from the `profiles create` wizard and introduce a standalone `profiles repo add` CLI command for adding repositories to an existing profile one at a time. Repositories are cloned automatically at VM boot via cloud-init, with optional per-repo token-based authentication referencing a stored secret.

## Motivation

The interactive profile creation wizard currently ends with an open-ended loop asking the user to add project repositories. This conflates two concerns: defining the sandbox environment (provider, image, tooling, agent) and tracking repositories to clone. Separating them makes each step clearer and allows repositories to be added or scripted independently of profile creation.

## Data Model Changes (`shared/types/profile.go`)

`ProjectConfig` gains an optional `AuthTokenEnv` field:

```go
type ProjectConfig struct {
    Repo         string `yaml:"repo"           json:"repo"`
    Path         string `yaml:"path"           json:"path"`
    AuthTokenEnv string `yaml:"auth_token_env" json:"auth_token_env,omitempty"`
}
```

`AuthTokenEnv` holds the **name** of a secret (as stored via `agentsdx secrets set`) whose value is a GitHub PAT or similar token. If empty, the repo is cloned without injected credentials (suitable for public repos).

## CLI Changes (`cli/cmd/agentsdx/profiles.go`)

### Remove from `profiles create` wizard

The `for { addProject }` loop in `runWizard()` is removed entirely. Profile creation no longer asks about repositories.

### New command: `profiles repo add`

```
agentsdx profiles repo add <profile> <repo-url> [path] [--auth-token-env <secret-name>]
```

- `<profile>` — required; name of an existing profile
- `<repo-url>` — required; git remote URL (e.g. `https://github.com/org/api.git`)
- `[path]` — optional positional arg; mount path in the VM. Defaults to `~/<repo-name>` where `repo-name` is the last path segment of the URL with any `.git` suffix stripped (e.g. `https://github.com/org/api.git` → `~/api`)
- `--auth-token-env <secret-name>` — optional flag; name of the secret to use as a git authentication token when cloning (e.g. `GITHUB_TOKEN`)

The command is grouped under a `profiles repo` subcommand so future repo operations (list, remove) fit naturally.

**Example usage:**
```sh
agentsdx secrets set myprofile GITHUB_TOKEN ghp_xxx
agentsdx profiles repo add myprofile https://github.com/org/api.git --auth-token-env GITHUB_TOKEN
agentsdx profiles repo add myprofile https://github.com/org/public-lib.git
```

## Client Changes (`cli/internal/client/client.go`)

New method:

```go
func (c *Client) AddProject(profile string, proj types.ProjectConfig) error
```

Sends `POST /profiles/{profile}/projects` with JSON body:

```json
{ "repo": "https://github.com/org/api.git", "path": "~/api", "auth_token_env": "GITHUB_TOKEN" }
```

Returns an error if the server responds with a non-204 status.

## Server — API Changes (`server/internal/api/handler.go`)

New route registered in `Router()`:

```go
r.Post("/profiles/{name}/projects", h.addProject)
```

Handler decodes a `types.ProjectConfig` from the request body and delegates to `h.profiles.AddProject(name, proj)`. Returns `204 No Content` on success.

## Server — Store Changes (`server/internal/profile/store.go`)

New method:

```go
func (s *Store) AddProject(name string, proj types.ProjectConfig) error
```

1. Loads the existing profile spec via `Get(name)`
2. Appends `proj` to `spec.Projects`
3. Marshals the updated spec and runs:
   ```sql
   UPDATE profiles SET spec = ? WHERE name = ?
   ```
4. Returns an error if the profile does not exist or the update fails.

## VM Provisioning Changes (`server/internal/vm/userdata.go`)

`BuildUserData` is extended to clone all profile projects in `runcmd` at VM boot.

For each project in the profile spec:
- If `AuthTokenEnv` is set, look up its value from the `secrets` map passed to `BuildUserData` and construct an authenticated clone URL: `https://<token>@<host>/<path>`
- If `AuthTokenEnv` is empty or the secret is not found, clone the URL as-is (public repo)
- Append a `git clone <url> <path>` entry to `runcmd`, running as the `ubuntu` user

The token value is embedded in the clone URL only within the cloud-init script. It is not written to any file on disk and does not appear in `/etc/agentsdx.env`.

**Example generated `runcmd` addition:**
```yaml
runcmd:
  - mkdir -p /home/ubuntu/.ssh && chmod 700 /home/ubuntu/.ssh && chown -R ubuntu:ubuntu /home/ubuntu/.ssh
  - su - ubuntu -c "git clone https://ghp_xxx@github.com/org/api.git ~/api"
  - su - ubuntu -c "git clone https://github.com/org/public-lib.git ~/public-lib"
```

The `BuildUserData` signature gains a `projects []types.ProjectConfig` parameter to receive the project list from the session manager.

## Session Manager

The session manager (which calls `BuildUserData`) already has the profile spec. It passes `spec.Projects` and the vault's `secrets` map to `BuildUserData` — no structural change needed beyond adding the new parameter.

## Error Cases

| Scenario | Behavior |
|---|---|
| Profile does not exist on `repo add` | Server returns 404; CLI prints error |
| Invalid JSON body | Server returns 400 |
| `AuthTokenEnv` names a secret that doesn't exist | Clone URL has no token; clone fails at boot if repo is private |
| `path` omitted by CLI | CLI derives `~/<repo-name>` before sending |
| Repo URL has no recognizable name segment | CLI returns an error asking for an explicit path |
