#!/bin/bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

# Run installer as ubuntu — the native build writes to ~/.claude and ~/.local
su ubuntu -c 'curl -fsSL https://claude.ai/install.sh | bash'

echo "alias claude-unsafe='claude --dangerouslySkipPermissions'" >> /home/ubuntu/.bashrc

cp /tmp/agentsdx-vm/agents/claude/entrypoint.sh /usr/local/bin/agentsdx-entrypoint.sh
chmod +x /usr/local/bin/agentsdx-entrypoint.sh

echo 'ForceCommand /usr/local/bin/agentsdx-entrypoint.sh' >> /etc/ssh/sshd_config

echo "claude provisioning complete"
