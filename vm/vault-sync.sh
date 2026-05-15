#!/bin/bash
# Syncs agent state back to the server at session end.
# Invoked via trap in entrypoint.sh; reads context from /etc/agentsdx.env.
set -euo pipefail

if [[ -f /etc/agentsdx.env ]]; then
    set -a
    source /etc/agentsdx.env
    set +a
fi

: "${AGENTSDX_SERVER_URL:?AGENTSDX_SERVER_URL is not set}"
: "${AGENTSDX_SESSION_ID:?AGENTSDX_SESSION_ID is not set}"

TARBALL="$(mktemp /tmp/vault-sync-XXXXXX.tar)"

PATHS=()
for p in /root/.claude /root/.claude.json; do
    [[ -e "$p" ]] && PATHS+=("$(basename "$p")")
done

if [[ ${#PATHS[@]} -eq 0 ]]; then
    echo "vault-sync: no agent state found, skipping"
    exit 0
fi

cd /root
tar -cf "$TARBALL" "${PATHS[@]}"

curl -s -X POST \
    --data-binary "@$TARBALL" \
    -H "Content-Type: application/octet-stream" \
    "${AGENTSDX_SERVER_URL}/sessions/${AGENTSDX_SESSION_ID}/vault-sync"

rm -f "$TARBALL"
echo "vault-sync: agent state synced"
