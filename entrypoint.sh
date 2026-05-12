#!/usr/bin/env bash
set -euo pipefail

HOST_CLAUDE=/host-claude
DEST=/home/ubuntu/.claude

# Seed non-credential config from the host on first start.
# Deliberately does NOT touch .credentials.json or .claude.json — those must be
# created by a one-time interactive `claude` login inside the container, and
# persist via the claude-config named volume.
if [ -d "$HOST_CLAUDE" ]; then
  for f in settings.json .settings.json CLAUDE.md; do
    if [ -f "$HOST_CLAUDE/$f" ] && [ ! -e "$DEST/$f" ]; then
      cp "$HOST_CLAUDE/$f" "$DEST/$f"
    fi
  done
fi

# Start the Docker daemon in the background (DinD).
if ! pgrep -x dockerd > /dev/null 2>&1; then
  sudo dockerd --host=unix:///var/run/docker.sock > /tmp/dockerd.log 2>&1 &
  # Wait until the socket is ready.
  timeout=15
  while [ $timeout -gt 0 ] && ! docker info > /dev/null 2>&1; do
    sleep 1
    timeout=$((timeout - 1))
  done
fi

exec "$@"
