#!/bin/bash
# VS Code Tunnel entrypoint for agentsdx.
# Runs inside the VM when the user SSH-connects.
# Attempts non-interactive auth from vault, starts tunnel, drops to shell.
set -euo pipefail

export PATH="$HOME/.local/bin:$PATH"

/usr/local/bin/agentsdx-vscode-auth.sh

systemctl start agentsdx-vscode-tunnel.service 2>/dev/null || true

/usr/local/bin/agentsdx-tunnel-status.sh

exec /usr/local/bin/agentsdx-session.sh
