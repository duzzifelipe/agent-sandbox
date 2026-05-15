#!/bin/bash
# Claude entrypoint: restores vault state, clones repos, registers vault-sync on exit,
# then hands off to the claude CLI.
# Runs inside the VM when the user SSH-connects.
set -euo pipefail

# Load session context written by cloud-init.
if [[ -f /etc/agentsdx.env ]]; then
    set -a
    # shellcheck source=/dev/null
    source /etc/agentsdx.env
    set +a
fi

: "${AGENTSDX_SERVER_URL:?AGENTSDX_SERVER_URL is not set in /etc/agentsdx.env}"
: "${AGENTSDX_SESSION_ID:?AGENTSDX_SESSION_ID is not set}"
: "${AGENTSDX_PROFILE:?AGENTSDX_PROFILE is not set}"

# Ensure git SSH key permissions (cloud-init sets mode, but be defensive).
chmod 600 /root/.ssh/id_rsa
chmod 700 /root/.ssh

# Configure SSH to use the git key for code hosts.
cat > /root/.ssh/config <<'EOF'
Host github.com
    IdentityFile /root/.ssh/id_rsa
    StrictHostKeyChecking accept-new

Host gitlab.com
    IdentityFile /root/.ssh/id_rsa
    StrictHostKeyChecking accept-new
EOF
chmod 600 /root/.ssh/config

# Restore agent state (claude memory, settings) from server vault.
STATE_FILE="$(mktemp /tmp/agent-state-XXXXXX.tar)"
HTTP_STATUS=$(curl -s -o "$STATE_FILE" -w "%{http_code}" \
    "${AGENTSDX_SERVER_URL}/sessions/${AGENTSDX_SESSION_ID}/agent-state")
if [[ "$HTTP_STATUS" == "200" ]] && [[ -s "$STATE_FILE" ]]; then
    tar -xf "$STATE_FILE" -C /root/ || echo "warning: vault restore failed, starting fresh"
fi
rm -f "$STATE_FILE"

# Clone repos declared in the profile.
PROFILE_JSON=$(curl -sf "${AGENTSDX_SERVER_URL}/profiles/${AGENTSDX_PROFILE}" || echo "{}")
while IFS= read -r line; do
    repo=$(echo "$line" | cut -d' ' -f1)
    path=$(echo "$line" | cut -d' ' -f2-)
    expanded="${path/#\~//root}"
    if [[ -n "$repo" ]] && [[ ! -d "$expanded" ]]; then
        mkdir -p "$(dirname "$expanded")"
        git clone "$repo" "$expanded" || echo "warning: failed to clone $repo"
    fi
done < <(echo "$PROFILE_JSON" | jq -r '.projects[]? | "\(.repo) \(.path)"')

# Sync vault state back to server when this SSH session ends.
trap '/usr/local/bin/vault-sync.sh' EXIT

exec claude
