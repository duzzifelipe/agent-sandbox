#!/bin/bash
# Template entrypoint for a new agent.
# This script runs inside the VM when a user SSH-connects.
# Copy this to vm/agents/<agent-name>/entrypoint.sh and replace exec line.
set -euo pipefail

if [[ -f /etc/agentsdx.env ]]; then
    set -a
    source /etc/agentsdx.env
    set +a
fi

: "${AGENTSDX_SERVER_URL:?}"
: "${AGENTSDX_SESSION_ID:?}"
: "${AGENTSDX_PROFILE:?}"

chmod 600 /root/.ssh/id_rsa

# Restore agent state from server vault.
STATE_FILE="$(mktemp /tmp/agent-state-XXXXXX.tar)"
HTTP_STATUS=$(curl -s -o "$STATE_FILE" -w "%{http_code}" \
    "${AGENTSDX_SERVER_URL}/sessions/${AGENTSDX_SESSION_ID}/agent-state")
if [[ "$HTTP_STATUS" == "200" ]] && [[ -s "$STATE_FILE" ]]; then
    tar -xf "$STATE_FILE" -C /root/ 2>/dev/null || true
fi
rm -f "$STATE_FILE"

trap '/usr/local/bin/vault-sync.sh' EXIT

# TODO: replace with the actual agent binary.
exec <agent-binary>
