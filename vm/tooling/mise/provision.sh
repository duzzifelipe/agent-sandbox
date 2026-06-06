#!/bin/bash
# Installs mise version manager and activates it for root.
set -euo pipefail

curl -fsSL https://mise.run | sh

echo 'eval "$(~/.local/bin/mise activate bash)"' >> /root/.bashrc
echo 'eval "$(~/.local/bin/mise activate bash)"' >> /etc/profile.d/mise.sh
chmod +x /etc/profile.d/mise.sh

echo "mise provisioning complete"
