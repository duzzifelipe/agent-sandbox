# VS Code Tunnel Agent

Run a VS Code Remote Tunnels endpoint inside your agentsdx sandbox. Connect from any local VS Code instance — no SSH, no exposed ports, no firewall rules.

## How It Works

```
┌─────────────────────┐         ┌─────────────────────┐
│  Your machine        │         │  Sandbox VM          │
│  VS Code +           │  HTTPS  │                      │
│  Remote - Tunnels    │◄───────►│  code tunnel          │
│  extension           │  (TLS)  │  (systemd service)   │
└─────────────────────┘         └─────────────────────┘
```

The VS Code CLI (`code`) runs as a **systemd service** inside the sandbox VM. It establishes an outbound WebSocket to Microsoft's tunnel service. Your local VS Code connects to the same service. No inbound ports needed — the tunnel punches through NAT and firewalls.

## Agent Structure

```
vm/tooling/vscode/
├── provision.sh    # Installed during image build
└── entrypoint.sh   # Runs on every session start
```

### provision.sh

- Downloads the `code` CLI for the target architecture (x86_64, aarch64, armv7l)
- Installs it as `/usr/local/bin/code`
- Creates a systemd service (`agentsdx-vscode-tunnel.service`) that:
  1. Runs `agentsdx-vscode-auth.sh` (non-interactive login if `AGENTSDX_VSCODE_TOKEN` is set in vault)
  2. Starts `code tunnel --accept-server-license-terms`
- Uses `VSCODE_CLI_USE_FILE_KEYCHAIN=1` to store auth tokens on disk instead of system keychain

### entrypoint.sh

- Runs auth check (from vault)
- Starts the tunnel systemd service
- Prints connection status
- Drops to the standard interactive shell

## Authentication

### Non-Interactive (recommended)

The VS Code CLI supports programmatic authentication via `--access-token`. Store your GitHub personal access token in the agentsdx vault:

```bash
agentsdx secrets set my-sandbox AGENTSDX_VSCODE_TOKEN
# Paste your GitHub personal access token (needs no specific scopes; the CLI
# only calls read:user + read:org)
```

Create a token at [github.com/settings/tokens](https://github.com/settings/tokens).

For Microsoft account auth, also set the provider:

```bash
agentsdx secrets set my-sandbox AGENTSDX_VSCODE_PROVIDER
# Paste: microsoft
```

On session start, `agentsdx-vscode-auth.sh` reads the token from the vault environment and calls:

```
code tunnel user login --access-token <token> --provider github
```

This stores the credential on disk and the tunnel starts immediately — no browser, no device code, fully headless.

**GitHub token requirements:**
- Personal access token (classic or fine-grained)
- No special scopes needed — the CLI validates the token against `/user` and `/orgs`

### Interactive (fallback)

If `AGENTSDX_VSCODE_TOKEN` is not set, the tunnel service needs manual authentication:

1. SSH into the sandbox
2. Run `code tunnel`
3. Follow the device code flow: open `https://github.com/login/device` and enter the code
4. The tunnel is now active for the session

## Creating a VS Code Tunnel Profile

```bash
agentsdx profiles create
```

In the interactive wizard:
- **Infrastructure** → Choose your base image (ubuntu-24.04) and any tooling (docker, gh, mise, vscode, etc.)
- **Agent** → Select any agent (claude, opencode, hermes) or none if only using tooling
- **Projects** → Add repos to clone on start (optional)

Then build the image:

```bash
agentsdx profiles build vscode-sandbox
```

## Starting a Session

```bash
agentsdx profiles run vscode-sandbox
```

With vault-stored auth, the tunnel starts automatically:
```
vs code tunnel: authenticated as github
vs code tunnel: RUNNING
```

Without auth:
```
vs code tunnel: no AGENTSDX_VSCODE_TOKEN set, skipping auto-login
vs code tunnel: INSTALLED (not authenticated)
```

## Connecting from VS Code

1. Install the **Remote - Tunnels** extension in your local VS Code (`ms-vscode.remote-server`)
2. Open the command palette (`Ctrl+Shift+P` / `Cmd+Shift+P`)
3. Run **Remote-Tunnels: Connect to Tunnel...**
4. Sign in with the same GitHub or Microsoft account used for the sandbox
5. Your sandbox VM appears in the tunnel list — select it to connect

Once connected, VS Code opens a full remote workspace on the sandbox:
- File explorer shows the sandbox filesystem
- Integrated terminal runs inside the VM
- Extensions run in the sandbox context
- Port forwarding works automatically

## Architecture Decisions

| Decision | Rationale |
|----------|-----------|
| Binary kept as `code` | Matches upstream naming; the sandbox VM has no desktop VS Code to shadow |
| Systemd service (not user session) | Tunnel survives SSH disconnect; starts on boot without user login |
| `--accept-server-license-terms` | Non-interactive startup; terms accepted during image build |
| `VSCODE_CLI_USE_FILE_KEYCHAIN` | VMs don't have a persistent system keychain; file storage at `~/.vscode-cli/token.json` is reliable |
| `ExecStartPre` auth script | Runs `code tunnel user login --access-token` before the tunnel starts; idempotent (no-op if already authenticated) |
| Vault secret for auth token | `AGENTSDX_VSCODE_TOKEN` is encrypted at rest alongside other sandbox secrets; surfaces at session start via `agentsdx.env` |

## Future Improvements

- **Persistent tunnel auth** — Token refresh is already built in (`code` CLI refreshes automatically); store across sessions via vault sync
- **Named tunnels** — `code tunnel --name <profile>` to identify sandboxes in VS Code's tunnel list
- **Tunnel health watchdog** — systemd `WatchdogSec=` integration so the service restarts if the tunnel goes down
- **Multi-agent profiles** — Run both `vscode` and `claude`/`opencode` in the same sandbox for hybrid workflows
