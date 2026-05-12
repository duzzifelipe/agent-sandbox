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

exec "$@"
