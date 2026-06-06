# agentsdx

**Ephemeral VMs for AI coding agents.**

agentsdx wraps every agent session in a fresh, isolated QEMU VM. Your agent gets a clean environment on every run; credentials and session state are encrypted at rest and restored automatically when a new session starts.

```
┌─ your machine ──────────────────────────────────────────────────┐
│                                                                 │
│   agentsdx profiles run work-backend                           │
│       │                                                         │
│       ▼                                                         │
│   downloads base image → creates overlay → boots QEMU VM       │
│   ┌─ VM (ephemeral) ──────────────────────────────────────┐    │
│   │  secrets injected → repos cloned → agent launched     │    │
│   │                                                        │    │
│   │  $ claude / opencode / hermes            ◄── you      │    │
│   │                                                        │    │
│   │  [session ends] → VM destroyed                         │    │
│   └────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
```

> **Status:** MVP — single-user, self-hosted, QEMU + macOS/Apple Silicon.

---

## How it works

1. **Profiles** describe a sandbox: base OS, tooling (mise, docker, gh…), repos, and which agent to run.
2. **Images** are built once per profile — a QEMU VM is booted, provisioned over SSH, then snapshotted.
3. **Sessions** create a copy-on-write overlay of the snapshot, boot it via QEMU, and inject your encrypted secrets and SSH keys via cloud-init before handing control to the agent.
4. **Vault** stores encrypted secrets (including Claude credentials) per-profile so the next session picks up where you left off.

---

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Go | 1.21+ | [go.dev/dl](https://go.dev/dl) |
| QEMU | latest | `brew install qemu` — includes `qemu-system-aarch64`, `qemu-img`, and HVF acceleration |
| Claude Code | latest | `npm install -g @anthropic-ai/claude-code && claude login` (if using the `claude` agent) |

---

## Getting started

### 1. Build

```bash
mise build
```

Produces `dist/agentsdx`.

### 2. Set your vault secret

```bash
export AGENTSDX_VAULT_SECRET="$(openssl rand -hex 32)"
```

Store this safely — you cannot recover vault data without it.

### 3. Create a profile

```bash
./dist/agentsdx profiles create      # interactive wizard
```

Prompts for a name, base OS image, tooling, and agent (claude / opencode / hermes).

### 4. Add repositories

```bash
./dist/agentsdx profiles repo add <profile> <repo-url> [path]
```

Optional `--auth-token-env <SECRET_KEY>` if the repo needs an auth token from the vault.

### 5. Import credentials

```bash
./dist/agentsdx secrets import-from-claude <profile>
```

Reads Claude Code credentials from this machine and stores them encrypted in the vault.

### 6. Build the image

```bash
./dist/agentsdx profiles build <profile>
```

Boots a temporary QEMU VM, provisions it over SSH with the profile's tooling and agent, then snapshots it. Takes ~5–10 min on first run.

### 7. Run a session

```bash
./dist/agentsdx profiles run <profile>
```

Boots the VM from the snapshot and drops you into the agent. The VM is destroyed when you exit.

---

## CLI reference

```
agentsdx profiles list                           list profiles
agentsdx profiles create                         create a new profile (interactive)
agentsdx profiles build <profile>                build a VM image for a profile
agentsdx profiles run <profile>                  start a session
agentsdx profiles repo add <profile> <url>       add a repository to a profile

agentsdx secrets list <profile>                  list secret key names
agentsdx secrets set <profile> <KEY>             set or overwrite a secret
agentsdx secrets delete <profile> <KEY>          remove a secret
agentsdx secrets import-from-claude <profile>    import Claude Code credentials
```

### Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `AGENTSDX_VAULT_SECRET` | **yes** | — | Master secret for AES-256-GCM vault encryption |
| `AGENTSDX_DATA_DIR` | no | `./data` | Data directory (images, vault, sessions) |

---

## Repository layout

```
agent-sandbox/
├── cmd/agentsdx/   # CLI entry point
├── internal/
│   ├── builder/    # image build orchestration (SSH provisioning)
│   ├── claudecreds/# Claude credential extraction
│   ├── datadir/    # data directory paths
│   ├── profile/    # profile store
│   ├── session/    # session lifecycle
│   ├── types/      # shared types
│   ├── vault/      # AES-256-GCM vault
│   └── vm/         # QEMU VM and image provider
└── vm/             # provisioning scripts (bash)
    ├── base/           provision.sh — git, curl, ssh
    ├── tooling/        mise / docker / docker-compose / gh
    ├── agents/
    │   ├── claude/     provision.sh + entrypoint.sh
    │   └── _template/  starting point for new agents
    └── data/           cloud-init templates
```

---

## Adding an agent

Copy `vm/agents/_template/` to `vm/agents/<name>/`, implement `provision.sh` (install the binary) and `entrypoint.sh` (restore state, exec the agent). Declare the agent name in the profile wizard's `agent.provider` field — no other changes required.

## Adding a tool

Add `vm/tooling/<name>/provision.sh`. Declare it in the profile's `infrastructure.tooling` list. No other changes required.

---

## License

MIT
