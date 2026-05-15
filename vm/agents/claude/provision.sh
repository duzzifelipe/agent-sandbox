#!/bin/bash
# Installs Node.js 22.x (required by claude CLI) and the claude CLI itself.
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
apt-get install -y nodejs

npm install -g @anthropic-ai/claude-code

echo "claude provisioning complete"
