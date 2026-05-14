# agentsdx MVP Design

**Date:** 2026-05-14
**Status:** Approved

---

## Overview

A managed environment layer that sits below any AI coding agent. Each session runs on a fresh, ephemeral VM isolated from the developer's local machine. Code state is managed by git; agent state is managed by a vault. The VM is always disposable.

MVP scope: single-user self-hosted deployment, VirtualBox as the only VM provider, Claude Code as the only agent.

---

## Repo Layout

Single Git repository, three Go modules, plus VM provisioning assets.

```
agent-sandbox/
├── cli/                          # go.mod: github.com/duck-labs/agentsdx-cli
│   ├── cmd/agentsdx/main.go
│   └── internal/
│       ├── client/               # HTTP client for server API
│       ├── ssh/                  # SSH session management
│       └── profile/              # local profile config read/write
│
├── server/                       # go.mod: github.com/duck-labs/agentsdx-server
│   ├── cmd/agentsdxd/main.go
│   └── internal/
│       ├── api/                  # HTTP handlers
│       ├── vm/                   # VMProvider interface + VirtualBox impl
│       ├── vault/                # AES-256-GCM encrypted storage
│       ├── profile/              # profile CRUD
│       └── session/              # session lifecycle, timeout watcher
│
├── shared/                       # go.mod: github.com/duck-labs/agentsdx-shared
│   └── types/                    # Profile spec, Vault schema, API request/response structs
│                                 # Pure data types only — no business logic, stdlib only
│
├── vm/                           # Packer configs + entrypoint scripts (not a Go module)
│   ├── virtualbox.pkr.hcl        # Packer template, driven by build manifest from server
│   ├── hetzner.pkr.hcl           # placeholder for future use
│   ├── base/
│   │   └── provision.sh          # minimal base tools (git, curl, ssh)
│   ├── tooling/
│   │   ├── mise/provision.sh
│   │   ├── docker/provision.sh
│   │   ├── docker-compose/provision.sh
│   │   └── gh/provision.sh
│   ├── agents/
│   │   ├── claude/
│   │   │   ├── provision.sh      # installs claude binary
│   │   │   └── entrypoint.sh     # restores vault state, execs claude
│   │   └── _template/
│   │       ├── provision.sh      # documented template for new agents
│   │       └── entrypoint.sh
│   └── vault-sync.sh             # tars AGENTSDX_VAULT_PATHS, POSTs to server
│
└── docker-compose.yml            # server self-hosting setup
```

During local development, `cli/go.mod` and `server/go.mod` use `replace` directives to point to `../shared`.

---

## Database

SQLite, stored in the `./data` volume. Used by the server only. Stores profiles, sessions, and image references. Go driver: `modernc.org/sqlite` (pure Go, no CGo).

The `database/sql` interface is used throughout so the backend can be swapped later (e.g. PostgreSQL for a future SaaS layer) without touching business logic.

---

## Data Layout (Filesystem)

```
data/
├── agentsdx.db                   # SQLite database
└── profiles/
    └── {profile-name}/
        ├── profile.yaml          # profile spec (plaintext)
        └── vault/
            ├── vault.json.enc    # encrypted: SSH keys
            └── agent-state.tar.enc  # encrypted: agent auth + memory state
```

---

## Vault

**Encryption:** AES-256-GCM. The master secret is set via `AGENTSDX_VAULT_SECRET` env var. A unique per-profile key is derived using HKDF-SHA256 with the profile name as salt — one compromised profile does not affect others.

**Vault package interface:**

```go
Store(profileName string, v *VaultData) error
Load(profileName string) (*VaultData, error)
```

**VaultData fields:**
- `GitPrivateKey` / `GitPublicKey` — SSH key pair for repo cloning
- `VMAccessPrivateKey` / `VMAccessPublicKey` — SSH key pair for CLI→VM access
- `AgentStatePaths []string` — paths to pack/unpack (default: `.claude/`, `.claude.json`)

`AgentStatePaths` is configurable so future vault contents require only a profile config change.

**Agent auth state:**

Claude Code authenticates via subscription (not API key). Auth state lives in `.claude.json` and `.claude/` on the local machine after `claude login`. These files are what `agentsdx credentials set <profile>` copies into the vault as `agent-state.tar.enc`. Each session start unpacks them into the VM; each session end packs them back. The first `credentials set` bootstraps the vault; subsequent session syncs keep it fresh.

---

## VMProvider Interface

Defined in `server/internal/vm/`:

```go
type VMProvider interface {
    CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error)
    DestroyVM(ctx context.Context, vmID string) error
    GetVM(ctx context.Context, vmID string) (*VM, error)
}

type CreateVMRequest struct {
    ProfileName   string   // used to resolve the built image (.ova path or snapshot ID)
    AuthorizedKey string   // VM access public key, registered at VM creation
    UserData      string   // NoCloud ISO content (cloud-init payload)
}

type VM struct {
    ID        string
    IPAddress string
    State     string
}
```

**VirtualBox implementation:**
- Wraps `VBoxManage` CLI calls
- Delivers SSH key and user-data via a NoCloud ISO built at runtime and mounted to the VM
- Resolves image names to `.ova` file paths via `images.json` on the server filesystem

**Image references (`images.json`):**
```json
{
  "work-backend": {
    "virtualbox": "/data/images/work-backend.ova",
    "hetzner": ""
  }
}
```

Images are keyed by profile name — each profile owns its image. The built artifact is an internal concern; it never appears in the profile spec.

---

## Agent Pluggable Structure & Image Building

Each image is composed from three layers, assembled by the server before invoking Packer:

1. **Base** (`vm/base/provision.sh`) — minimal tools: git, curl, ssh
2. **Tooling** (`vm/tooling/{tool}/provision.sh`) — one script per declared tool, run in order
3. **Agent** (`vm/agents/{agent}/provision.sh` + `entrypoint.sh`) — agent binary and its entrypoint

The server generates a build manifest from the profile's `infrastructure.tooling` list and `agent.provider`, then passes it to Packer. The Packer template executes each provisioner in sequence.

```
vm/
├── virtualbox.pkr.hcl        # Packer template, driven by build manifest from server
├── hetzner.pkr.hcl           # placeholder for future use
├── base/
│   └── provision.sh
├── tooling/
│   ├── mise/provision.sh
│   ├── docker/provision.sh
│   ├── docker-compose/provision.sh
│   └── gh/provision.sh
├── agents/
│   ├── claude/
│   │   ├── provision.sh      # installs claude binary
│   │   └── entrypoint.sh     # restores .claude/ + .claude.json, execs claude
│   └── _template/            # documented template for new agents
└── vault-sync.sh             # tars AGENTSDX_VAULT_PATHS, POSTs to server
```

The profile spec declares the base OS, tooling, and agent. The built image is derived from the profile name — no separate image name field:

```yaml
infrastructure:
  provider: virtualbox
  image: ubuntu-24.04              # base OS Packer starts from
  tooling:
    - mise
    - docker
    - gh

agent:
  provider: claude
```

Adding a new agent: new `agents/{name}/` directory. Adding a new tool: new `tooling/{name}/provision.sh`. Neither requires changes to server code or shared scripts.

---

## Session Lifecycle

**States:** `pending → starting → running → stopping → destroyed`

**Start — server:**
1. Create session record (state: `pending`)
2. Load and decrypt vault
3. Build NoCloud user-data payload: git SSH private key, `AGENTSDX_VAULT_PATHS`
4. Call `VMProvider.CreateVM` with VM access public key and user-data
5. Poll until VM is reachable on SSH (state: `running`)
6. Start idle timeout watcher (default: 2h, configurable)

**Start — CLI:**
1. `POST /sessions` → receive session ID
2. Poll `GET /sessions/{id}` until state = `running`, receive IP
3. `GET /sessions/{id}/key` → receive VM access private key (acceptable for single-user MVP; revisit for SaaS multi-tenant)
4. Write key to temp file, exec `ssh -i <tempfile> root@<IP>`

**Stop — server:**
1. CLI calls `POST /sessions/{id}/stop`
2. Server SSHes into VM, runs `vault-sync.sh`
3. `vault-sync.sh` tars `AGENTSDX_VAULT_PATHS`, POSTs tarball to `POST /sessions/{id}/vault-sync`
4. Server encrypts tarball, stores as `agent-state.tar.enc`
5. `VMProvider.DestroyVM` called, state = `destroyed`

---

## HTTP API

No authentication for MVP (single-user self-hosted). All endpoints consumed by the CLI except `/sessions/{id}/vault-sync` which is called by the VM.

```
# Profiles
GET    /profiles                       list all profiles
POST   /profiles                       create profile
GET    /profiles/{name}                get profile
PUT    /profiles/{name}                update profile
DELETE /profiles/{name}                delete profile
POST   /profiles/{name}/credentials    bootstrap agent auth state in vault (accepts tarball of .claude/ + .claude.json)

# Sessions
POST   /sessions                       start session (body: profile name)
GET    /sessions/{id}                  get session state + IP
GET    /sessions/{id}/key              fetch VM access private key
POST   /sessions/{id}/stop             trigger vault sync + VM destroy
POST   /sessions/{id}/vault-sync       receive agent state tarball from VM

# Images
POST   /images/build                   trigger Packer build (body: profile name — reads its image, tooling, agent)
GET    /images                         list built images
```

---

## CLI Commands

```bash
agentsdx run <profile>              # start session, open SSH transparently
agentsdx stop <profile>             # sync vault and destroy VM
agentsdx profiles                   # list sandbox profiles
agentsdx create <profile>           # create a new profile interactively
agentsdx credentials set <profile>  # copy local agent auth state (.claude/ + .claude.json) into vault
agentsdx sync <profile> <file>      # push a local file into the running VM via SCP (uses vault-managed VM access key)
agentsdx images build <profile>     # trigger Packer build for a profile (reads its image, tooling, agent)
```

---

## Key Decisions

| Decision | MVP | Future |
|---|---|---|
| Deployment | Self-hosted (Docker) | SaaS layer for multi-tenant |
| VM providers | VirtualBox only | Hetzner + others |
| Agent providers | Claude Code only (subscription auth) | New agent = new `agents/{name}/` dir + new image build |
| Agent images | One image per profile (base OS + tooling + agent); keyed by profile name in `images.json` | — |
| Vault paths | `.claude/`, `.claude.json` | Configurable via `AgentStatePaths` |
| Secret store | AES-256-GCM + env var key | OpenBao or cloud KMS |
| Database | SQLite | PostgreSQL (SaaS) |
| API auth | None (single-user) | Token-based (SaaS) |
