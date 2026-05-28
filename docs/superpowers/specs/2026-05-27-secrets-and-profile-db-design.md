# Secrets Management & Profile DB Migration Design

**Date:** 2026-05-27  
**Status:** Approved

## Overview

Two related changes shipped together:

1. **Profile store → full SQLite** — eliminate the YAML+SQLite hybrid; store profile specs as JSON in the existing `profiles` table.
2. **Generic per-profile secrets** — let users store arbitrary key/value secrets (GitHub PATs, AI API keys, etc.) encrypted in the vault, injected as env vars into VM sessions at boot.

## Data Model

### SQLite: `profiles` table

Add a `spec TEXT NOT NULL DEFAULT ''` column to the existing `profiles` table via a schema migration. All profile CRUD operations read/write this column as JSON. The `name` TEXT PRIMARY KEY column is unchanged.

**Migration strategy:** On server startup, after applying the schema migration, scan the profiles directory for `*.yaml` files. For each file whose `name` is not already present in the DB (i.e., `spec` is empty or the row is missing), insert/update it. After migration the YAML files are left on disk but no longer read or written.

### `VaultData` struct (`shared/types/vault.go`)

Add one field:

```go
Secrets map[string]string `json:"secrets,omitempty"`
```

Secrets are stored inside the existing per-profile AES-256-GCM encrypted vault file (`<vaultDir>/<profileName>.vault.enc`). No new files or encryption infrastructure.

## API

Three new routes under the existing profile namespace:

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/profiles/{name}/secrets/{key}` | Set or overwrite a single secret. Body: `{"value":"..."}` |
| `DELETE` | `/profiles/{name}/secrets/{key}` | Remove a single secret |
| `GET` | `/profiles/{name}/secrets` | List secret key names only — never values |

**PUT handler flow:** load vault (or initialize it if missing, same as `setCredentials`), upsert `Secrets[key] = value`, save vault.

**DELETE handler flow:** load vault, delete `Secrets[key]`, save vault. No-op if key does not exist.

**GET handler:** returns a JSON array of key names, e.g. `["GITHUB_PAT", "OPENAI_API_KEY"]`. Values are never returned to prevent leaking secrets through the API.

## CLI

New `secrets` subcommand in `cli/cmd/agentsdx/secrets.go`:

```
agentsdx secrets set <profile> <KEY> <VALUE>   → PUT /profiles/{name}/secrets/{key}
agentsdx secrets delete <profile> <KEY>         → DELETE /profiles/{name}/secrets/{key}
agentsdx secrets list <profile>                 → GET /profiles/{name}/secrets
```

Registered in `main.go` alongside existing `credentials` and `profiles` commands.

`cli/internal/client/client.go` gets three new methods: `SetSecret(profile, key, value string)`, `DeleteSecret(profile, key string)`, `ListSecrets(profile string) ([]string, error)`.

## VM Injection

`BuildUserData` in `server/internal/vm/userdata.go` gains a `secrets map[string]string` parameter. Each entry is appended to `/etc/agentsdx.env` after the existing `AGENTSDX_*` vars:

```
AGENTSDX_SERVER_URL=...
AGENTSDX_SESSION_ID=...
AGENTSDX_PROFILE=...
GITHUB_PAT=ghp_...
OPENAI_API_KEY=sk-...
```

The VM entrypoint already sources `/etc/agentsdx.env` — no VM-side changes required.

In `server/internal/session/manager.go`, `Start` passes `vaultData.Secrets` to `BuildUserData` after loading the vault.

## Files Changed

| File | Change |
|------|--------|
| `shared/types/vault.go` | Add `Secrets map[string]string` to `VaultData` |
| `server/internal/db/db.go` | Add `spec TEXT` column to `profiles` table; YAML migration on startup |
| `server/internal/profile/store.go` | Remove file I/O; read/write `spec` JSON column only |
| `server/internal/api/handler.go` | Add `setSecret`, `deleteSecret`, `listSecrets` handlers; remove `profilesDir` dependency |
| `server/internal/vm/userdata.go` | Add `secrets map[string]string` param; append to env file |
| `server/internal/session/manager.go` | Pass `vaultData.Secrets` to `BuildUserData` |
| `cli/cmd/agentsdx/secrets.go` | New file: `secrets set/delete/list` commands |
| `cli/cmd/agentsdx/main.go` | Register `secrets` command |
| `cli/internal/client/client.go` | Add `SetSecret`, `DeleteSecret`, `ListSecrets` methods |

## Out of Scope

- Secret rotation for running sessions (secrets are baked in at boot; re-creating the session picks up new values)
- Bulk import from `.env` files
- Global/shared secrets across profiles
- Returning secret values via the API
