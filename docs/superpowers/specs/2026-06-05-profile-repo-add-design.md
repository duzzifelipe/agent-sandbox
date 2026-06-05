# Profile Repo Add — Design Spec

**Date:** 2026-06-05

## Summary

Remove git repository configuration from the `profiles create` wizard and introduce a standalone `profiles repo add` CLI command for adding repositories to an existing profile one at a time.

## Motivation

The interactive profile creation wizard currently ends with an open-ended loop asking the user to add project repositories. This conflates two concerns: defining the sandbox environment (provider, image, tooling, agent) and tracking repositories to clone. Separating them makes each step clearer and allows repositories to be added or scripted independently of profile creation.

## CLI Changes

### Remove from `profiles create` wizard

The `for { addProject }` loop in `runWizard()` (`cli/cmd/agentsdx/profiles.go`) is removed entirely. Profile creation no longer asks about repositories.

### New command: `profiles repo add`

```
agentsdx profiles repo add <profile> <repo-url> [path]
```

- `<profile>` — required; name of an existing profile
- `<repo-url>` — required; git remote URL (e.g. `https://github.com/org/api.git`)
- `[path]` — optional; mount path in the VM. Defaults to `~/<repo-name>` where `repo-name` is the last path segment of the URL with any `.git` suffix stripped (e.g. `https://github.com/org/api.git` → `~/api`)

The command is grouped under a `profiles repo` subcommand so future repo operations (list, remove) fit naturally.

## Client Changes (`cli/internal/client/client.go`)

New method:

```go
func (c *Client) AddProject(profile, repo, path string) error
```

Sends `POST /profiles/{profile}/projects` with JSON body:

```json
{ "repo": "<repo-url>", "path": "<path>" }
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

## Data Model

No changes to `types.ProfileSpec` or `types.ProjectConfig` — both already exist in `shared/types/profile.go`.

## Error Cases

| Scenario | Behavior |
|---|---|
| Profile does not exist | Server returns 404; CLI prints error |
| Invalid JSON body | Server returns 400 |
| Missing `repo` field | Server returns 400 (empty required field) |
| `path` omitted by client | CLI derives default before sending |
