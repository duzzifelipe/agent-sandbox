#!/bin/bash
# Runs inside the VM when the user SSH-connects.
set -euo pipefail

export PATH="$HOME/.local/bin:$PATH"

exec /usr/local/bin/agentsdx-session.sh
