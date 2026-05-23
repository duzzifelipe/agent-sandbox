# agentsdx

**Ephemeral VMs for AI coding agents.**

agentsdx wraps every Claude Code session in a fresh, isolated QEMU VM. Your agent gets a clean environment on every run; your credentials, memory, and git keys are encrypted at rest and synced back automatically when the session ends.

```
┌─ your machine ──────────────────────────────────────────────────────┐
│                                                                     │
│   agentsdx run work-backend                                         │
│       │                                                             │
│       ▼                                                             │
│   ┌─ agentsdxd (server) ──────────────────────────────────────┐    │
│   │  snapshots qcow2 → boots QEMU → injects SSH key + env     │    │
│   │  ┌─ VM (ephemeral) ──────────────────────────────────┐    │    │
│   │  │  vault restored → repos cloned → claude launched  │    │    │
│   │  │                                                    │    │    │
│   │  │  $ claude                                ◄── you  │    │    │
│   │  │                                                    │    │    │
│   │  │  [session ends] → vault-sync → destroy VM         │    │    │
│   │  └────────────────────────────────────────────────────┘    │    │
│   └────────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────┘
```

> **Status:** MVP — single-user, self-hosted, QEMU + Claude Code (macOS/Apple Silicon).

---

## How it works

1. **Profiles** describe a sandbox: base OS, tooling (mise, docker, gh…), and which agent to run.
2. **Images** are built once per profile by Packer — Ubuntu + your tooling baked into a qcow2 image.
3. **Sessions** create a copy-on-write snapshot of the image, boot the VM via QEMU, and inject your encrypted SSH keys and agent state via cloud-init. The VM calls back to the server to restore credentials before handing control to `claude`.
4. **Vault sync** runs automatically when you exit — the current `.claude/` state is encrypted and stored server-side so the next session picks up exactly where you left off.

---

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Go | 1.21+ | [go.dev/dl](https://go.dev/dl) |
| QEMU | latest | `brew install qemu` — includes `qemu-system-aarch64`, `qemu-img`, and vmnet-shared support |
| Packer | 1.11+ | `brew install packer` |
| Claude Code | latest | `npm install -g @anthropic-ai/claude-code && claude login` |

---

## Getting started

### 1. Build

```bash
mise build

# or individually
mise build:server
mise build:cli
```

### 2. One-time host setup

Verifies QEMU is installed and downloads the Packer QEMU plugin:

```bash
./agentsdxd setup
```

### 3. Start the server

```bash
export AGENTSDX_VAULT_SECRET="$(openssl rand -hex 32)"
export AGENTSDX_SERVER_URL="http://192.168.64.1:8080"

./agentsdxd serve
```

`AGENTSDX_VAULT_SECRET` encrypts the vault — store it safely, you cannot recover vault data without it.  
`AGENTSDX_SERVER_URL` is the address VMs use to call back to the server; `192.168.64.1` is the host gateway on the `vmnet-shared` interface used by QEMU on macOS.

### 4. Create a profile and build an image

```bash
./agentsdx create                        # interactive wizard
./agentsdx credentials set <profile>     # upload ~/.claude state + generate SSH keys
./agentsdx images build <profile>        # trigger Packer build (~15 min first time)
```

### 5. Run a session

```bash
./agentsdx run <profile>
```

This boots the VM, waits for it to come up, and drops you straight into `claude`. When you exit, the vault syncs automatically.

```bash
./agentsdx stop <profile>               # tear down the VM from another terminal if needed
```

---

## CLI reference

```
agentsdx create                         create a new profile (interactive)
agentsdx profiles                       list profiles
agentsdx credentials set <profile>      upload Claude auth state to the vault
agentsdx images build <profile>         build a VM image for a profile
agentsdx run <profile>                  start a session and open Claude
agentsdx stop <profile>                 sync vault and destroy the VM
agentsdx sync <profile> <file>          copy a local file into the running VM
```

---

## Server reference

```
agentsdxd setup                         one-time host setup (verify QEMU + packer init)
agentsdxd serve                         start the HTTP API server
```

| Variable | Required | Default | Description |
|---|---|---|---|
| `AGENTSDX_VAULT_SECRET` | **yes** | — | Master secret for AES-256-GCM vault encryption |
| `AGENTSDX_SERVER_URL` | **yes** | — | URL VMs use to reach back to the server |
| `AGENTSDX_ADDR` | no | `:8080` | Server listen address |
| `AGENTSDX_DATA_DIR` | no | `./data` | Data directory |
| `AGENTSDX_VM_DIR` | no | `./vm` | Path to the `vm/` provisioning scripts |

---

## Repository layout

```
agent-sandbox/
├── cli/            # agentsdx CLI (Go)
├── server/         # agentsdxd server (Go)
├── shared/         # shared types (Go)
└── vm/             # Packer template + provisioning scripts (bash)
    ├── base/           provision.sh — git, curl, ssh
    ├── tooling/        mise / docker / docker-compose / gh
    ├── agents/
    │   ├── claude/     provision.sh + entrypoint.sh
    │   └── _template/  starting point for new agents
    ├── vault-sync.sh   called on session end to sync state back
    └── qemu.pkr.hcl    Packer template (QEMU, macOS/Apple Silicon)
```

---

## Adding an agent

Copy `vm/agents/_template/` to `vm/agents/<name>/`, implement `provision.sh` (install the binary) and `entrypoint.sh` (restore state, exec the agent). No server changes required — the builder picks it up from the profile's `agent.provider` field.

## Adding a tool

Add `vm/tooling/<name>/provision.sh`. Declare it in the profile's `infrastructure.tooling` list. No server changes required.

---

## License

MIT
