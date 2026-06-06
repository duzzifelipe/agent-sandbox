#!/bin/bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

# Install OpenCode as the ubuntu user — binary lands in ~/.local/bin
su ubuntu -c 'curl -fsSL https://opencode.ai/install | bash'

cp /tmp/agentsdx-vm/agents/opencode/entrypoint.sh /usr/local/bin/agentsdx-entrypoint.sh
chmod +x /usr/local/bin/agentsdx-entrypoint.sh

echo 'ForceCommand /usr/local/bin/agentsdx-entrypoint.sh' >> /etc/ssh/sshd_config

echo "opencode provisioning complete"
