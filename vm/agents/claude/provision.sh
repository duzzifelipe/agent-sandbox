#!/bin/bash
# Installs Node.js 22.x (required by claude CLI) and the claude CLI itself.
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
apt-get install -y nodejs

npm install -g @anthropic-ai/claude-code

cp /tmp/agentsdx-vm/vault-sync.sh /usr/local/bin/vault-sync.sh
chmod +x /usr/local/bin/vault-sync.sh

cp /tmp/agentsdx-vm/agents/claude/entrypoint.sh /usr/local/bin/agentsdx-entrypoint.sh
chmod +x /usr/local/bin/agentsdx-entrypoint.sh

echo 'ForceCommand /usr/local/bin/agentsdx-entrypoint.sh' >> /etc/ssh/sshd_config

echo "claude provisioning complete"
