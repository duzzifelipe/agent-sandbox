# Agent Sandbox

A Docker-based sandbox for running [Claude Code](https://claude.ai/code) in an isolated environment on macOS. The container includes Docker-in-Docker (DinD), Node.js (via mise), and a pre-installed `claude` CLI — giving Claude a safe, self-contained workspace to execute code, spin up services, and use tools without touching your host system.

## What's inside

- **Ubuntu 24.04** base image
- **Claude Code CLI** (`claude`) — pre-installed and ready to use
- **Docker Engine + Compose** — full DinD so Claude can build and run containers
- **Node.js LTS** — managed by [mise](https://mise.jdx.dev)
- **zsh** shell with `yolo` alias (`claude --dangerously-skip-permissions`)
- Your host `~/.claude` config (settings, CLAUDE.md) seeded into the container on first start

## Prerequisites

- [Docker Desktop for Mac](https://www.docker.com/products/docker-desktop/) installed and running
- A Claude Code account (free or Pro) — you'll log in once inside the container

## Quick start

### 1. Build the image

```bash
docker compose build
```

### 2. Start a shell

```bash
docker compose run --rm claude
```

This drops you into a zsh shell inside the container at `/workspace`, which maps to the parent directory (`..`) of this repo on your host by default.

### 3. Log in to Claude (one-time)

Inside the container, run:

```bash
claude
```

Follow the OAuth flow to authenticate. Credentials are stored in the `claude-config` named Docker volume and persist across container restarts — you won't need to log in again.

### 4. Use Claude Code

```bash
# Normal interactive session
claude

# Skip all permission prompts (convenient inside this sandbox)
yolo
```

## Configuration

### Mounting a different workspace

By default `/workspace` maps to the directory above this repo (`..`). Set `CLAUDE_WORKSPACE` to point anywhere on your host:

```bash
CLAUDE_WORKSPACE=~/my-project docker compose run --rm claude
```

Or export it in your shell profile:

```bash
export CLAUDE_WORKSPACE=~/my-project
```

### Timezone

The default timezone is `America/Sao_Paulo`. Override it at build time:

```bash
TZ=America/New_York docker compose build
```

### Host Claude config sync

On first start the entrypoint copies `settings.json`, `.settings.json`, and `CLAUDE.md` from your host `~/.claude` into the container (only if they don't already exist in the volume). Credentials (`~/.claude.json`, `.credentials.json`) are **never** copied — they are created by the in-container login and stored only in the named volume.

## Persistence

| What | Where |
|------|-------|
| Claude credentials & config | `claude-config` Docker volume |
| Inner Docker images/layers | `docker-data` Docker volume |
| Your code | Host filesystem (mounted at `/workspace`) |

To wipe Claude state and start fresh (re-login required):

```bash
docker volume rm agent-sandbox_claude-config
```

## Stopping and cleaning up

```bash
# Exit the container shell
exit

# Remove all volumes (loses Claude login — re-auth required next time)
docker compose down -v
```
