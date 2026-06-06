#!/bin/bash
# Installs docker-compose v2 standalone binary.
set -euo pipefail

COMPOSE_VERSION="v2.35.0"
ARCH=$(uname -m)
curl -fsSL \
  "https://github.com/docker/compose/releases/download/${COMPOSE_VERSION}/docker-compose-linux-${ARCH}" \
  -o /usr/local/bin/docker-compose
chmod +x /usr/local/bin/docker-compose

echo "docker-compose provisioning complete"
