# agentsdx

Turn AI coding agents into ephemeral, sandboxed VMs.

```
agentsdx profiles run work-backend
  └── boots QEMU VM → injects secrets → clones repos → launches agent
  └── [session ends] → VM destroyed
```

> **Status:** MVP — single-user, self-hosted, QEMU + macOS/Apple Silicon.

---

## Why VMs instead of containers?

I started this project with Docker containers. Containers on macOS already run inside a Linux VM if you're using Docker Desktop, so I figured: why not just use that VM directly?

**Containers share the host kernel** — a container breakout or a malicious `--privileged` flag compromises the entire environment. Docker Desktop also uses a hypervisor-managed VM, but you don't control it, and **Docker Desktop's business license** is restrictive for commercial use (requires a paid subscription for teams of any size).

A dedicated QEMU VM gives you:
- **Real isolation** — separate kernel, separate memory, no shared surfaces
- **Full control** — you own the base image, the provisioning, the lifecycle
- **No licensing surprises** — QEMU is pure open source (GPL)
- **Deterministic teardown** — session ends, VM is destroyed, nothing persists

---

## How it works

1. **Profiles** describe a sandbox: base OS, tooling (mise, docker, gh…), repos, and which agent to run.
2. **Images** are built once per profile — a QEMU VM is booted, provisioned over SSH, then snapshotted.
3. **Sessions** create a copy-on-write overlay of the snapshot, boot it via QEMU, and inject your encrypted secrets and SSH keys via cloud-init before handing control to the agent.
4. **Vault** stores encrypted secrets (including Claude / OpenCode credentials) per-profile so the next session picks up where you left off.

---

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Go | 1.21+ | use [`mise`](https://mise.jdx.dev) for setting up the env |
| QEMU | latest | `brew install qemu` — includes `qemu-system-aarch64`, `qemu-img`, and HVF acceleration |

---

## Getting started

### 1. Build

```bash
mise build
```

Produces `dist/agentsdx`.

### 2. Set your vault secret

```bash
echo "\nAGENTSDX_VAULT_SECRET=$(openssl rand -hex 32)" >> .env
```

Store this safely — you cannot recover vault data without it.

### 3. Create a profile

```bash
./dist/agentsdx profiles create      # interactive wizard
```

Prompts for a name, base OS image, tooling (mise, docker, gh, vscode, etc.), and agent (claude / opencode / hermes).

### 4. Build the image

```bash
./dist/agentsdx profiles build <profile>
```

Boots a temporary QEMU VM, provisions it over SSH with the profile's tooling and agent, then snapshots it. Takes ~5–10 min on first run.

### 5. Add secrets

```bash
./dist/agentsdx secrets add <profile> <secret-name>
```

You can use, for example to set a scoped Github PAT key (like `GITHUB_SECRET_KEY`) to pull/push your repo.

### 6. Add repositories

```bash
./dist/agentsdx profiles repo add <profile> <repo-url> [path]
```

Optional `--auth-token-env <GITHUB_SECRET_KEY>` if the repo needs an auth token from the vault.

### 7. Import credentials (optional)

Claude Code:

```bash
./dist/agentsdx secrets import-from-claude <profile>
```

Reads Claude Code credentials from your machine and stores them encrypted in the vault. When you start a new session, credentials are injected automatically.

OpenCode:

```bash
./dist/agentsdx secrets import-from-opencode <profile>
```

Reads OpenCode config, auth, and account files from your machine and stores them encrypted in the vault.

VS Code Tunnel:

```bash
./dist/agentsdx secrets set <profile> AGENTSDX_VSCODE_TOKEN
# Paste your GitHub personal access token
```

Creates a token at https://github.com/settings/tokens (no special scopes required).

> Since neither method updates refresh tokens automatically, run these periodically to ensure new sessions receive valid tokens.

### 8. Run a session

```bash
./dist/agentsdx profiles run <profile>
```

Boots the VM from the snapshot and drops you into the agent. The VM is destroyed when you exit.

---

## CLI reference

```
agentsdx profiles list                           list profiles
agentsdx profiles create                         create a new profile (interactive)
agentsdx profiles delete <profile>               delete a profile, its secrets, and its image
agentsdx profiles build <profile>                build a VM image for a profile
agentsdx profiles run <profile>                  start a session
agentsdx profiles repo add <profile> <url>       add a repository to a profile

agentsdx secrets list <profile>                  list secret key names
agentsdx secrets set <profile> <KEY>             set or overwrite a secret
agentsdx secrets delete <profile> <KEY>          remove a secret
agentsdx secrets import-from-claude <profile>    import Claude Code credentials
agentsdx secrets import-from-opencode <profile>  import OpenCode config, auth, and account
```

### Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `AGENTSDX_VAULT_SECRET` | **yes** | — | Master secret for AES-256-GCM vault encryption |
| `AGENTSDX_DATA_DIR` | no | `./data` | Data directory (images, vault, sessions) |

---

## Adding an agent

Copy `vm/agents/_template/` to `vm/agents/<name>/`, implement `provision.sh` (install the binary) and `entrypoint.sh` (restore state, exec the agent). Declare the agent name in the profile wizard's `agent.provider` field — no other changes required.

## Adding a tool

Add `vm/tooling/<name>/provision.sh`. Declare it in the profile's `infrastructure.tooling` list. No other changes required.

---

## License

MIT
