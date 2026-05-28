#!/bin/bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

# Ensure ubuntu user and home exist before installing (installer writes ~/.claude)
id ubuntu 2>/dev/null || useradd -m -s /bin/bash -u 1000 ubuntu
mkdir -p /home/ubuntu
chown 1000:1000 /home/ubuntu

# Run installer as ubuntu — the native build writes to ~/.claude and ~/.local
su ubuntu -c 'curl -fsSL https://claude.ai/install.sh | bash'

cp /tmp/agentsdx-vm/vault-sync.sh /usr/local/bin/vault-sync.sh
chmod +x /usr/local/bin/vault-sync.sh

cp /tmp/agentsdx-vm/agents/claude/entrypoint.sh /usr/local/bin/agentsdx-entrypoint.sh
chmod +x /usr/local/bin/agentsdx-entrypoint.sh

echo 'ForceCommand /usr/local/bin/agentsdx-entrypoint.sh' >> /etc/ssh/sshd_config

echo "claude provisioning complete"
