#!/bin/bash
# Runs inside the VM when the user SSH-connects.
set -euo pipefail

# Ensure ~/.local/bin (where the opencode installer puts the binary) is in PATH.
export PATH="$HOME/.local/bin:$PATH"

# Load session context written by cloud-init.
if [[ -f /etc/agentsdx.env ]]; then
    set -a
    # shellcheck source=/dev/null
    source /etc/agentsdx.env
    set +a
fi

# Ensure git SSH key permissions (cloud-init sets mode, but be defensive).
chmod 600 ~/.ssh/id_rsa
chmod 700 ~/.ssh

exec bash
