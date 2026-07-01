#!/bin/bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

ARCH="$(uname -m)"
case "$ARCH" in
    x86_64)  CLI_ARCH="x64" ;;
    aarch64) CLI_ARCH="arm64" ;;
    armv7l)  CLI_ARCH="armhf" ;;
    *)       echo "unsupported arch: $ARCH"; exit 1 ;;
esac

TMP_DIR="$(mktemp -d)"
CLI_URL="https://code.visualstudio.com/sha/download?build=stable&os=cli-alpine-${CLI_ARCH}"
curl -fsSL "$CLI_URL" -o "$TMP_DIR/vscode_cli.tar.gz"
tar -xzf "$TMP_DIR/vscode_cli.tar.gz" -C "$TMP_DIR"
mv "$TMP_DIR/code" /usr/local/bin/code
chmod +x /usr/local/bin/code
rm -rf "$TMP_DIR"

cp /tmp/agentsdx-vm/tooling/vscode/entrypoint.sh /usr/local/bin/agentsdx-entrypoint-vscode.sh
chmod +x /usr/local/bin/agentsdx-entrypoint-vscode.sh

cat > /usr/local/bin/agentsdx-vscode-auth.sh << 'SCRIPT'
#!/bin/bash
set -euo pipefail

if [[ -f /etc/agentsdx.env ]]; then
    set -a
    source /etc/agentsdx.env
    set +a
fi

TOKEN="${AGENTSDX_VSCODE_TOKEN:-}"
PROVIDER="${AGENTSDX_VSCODE_PROVIDER:-github}"

if [[ -z "$TOKEN" ]]; then
    echo "vs code tunnel: no AGENTSDX_VSCODE_TOKEN set, skipping auto-login"
    echo "  Set the secret via: agentsdx secrets set <profile> AGENTSDX_VSCODE_TOKEN"
    echo "  Then run: code tunnel user login --access-token <token> --provider github"
    exit 0
fi

runuser -u ubuntu -- code tunnel user login \
    --access-token "$TOKEN" \
    --provider "$PROVIDER"

echo "vs code tunnel: authenticated as ${PROVIDER}"
SCRIPT
chmod +x /usr/local/bin/agentsdx-vscode-auth.sh

cat > /usr/local/bin/agentsdx-tunnel-status.sh << 'SCRIPT'
#!/bin/bash
set -euo pipefail

if [[ ! -x /usr/local/bin/code ]]; then
    echo "vs code tunnel: NOT INSTALLED"
    exit 1
fi

if runuser -u ubuntu -- code tunnel status &>/dev/null; then
    echo "vs code tunnel: RUNNING"
    runuser -u ubuntu -- code tunnel status 2>&1 || true
else
    echo "vs code tunnel: INSTALLED (not authenticated)"
    echo ""
    echo "To authenticate:"
    echo "  code tunnel user login --access-token <token> --provider github"
    echo ""
    echo "Or run interactively:"
    echo "  code tunnel"
    echo ""
    echo "Then start the service:"
    echo "  sudo systemctl start agentsdx-vscode-tunnel"
fi
SCRIPT
chmod +x /usr/local/bin/agentsdx-tunnel-status.sh

cat > /etc/systemd/system/agentsdx-vscode-tunnel.service << 'UNIT'
[Unit]
Description=VS Code Tunnel for agentsdx sandbox
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=ubuntu
ExecStartPre=/usr/local/bin/agentsdx-vscode-auth.sh
ExecStart=/usr/local/bin/code tunnel --accept-server-license-terms
Restart=on-failure
RestartSec=10
Environment=HOME=/home/ubuntu
Environment=VSCODE_CLI_USE_FILE_KEYCHAIN=1

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable agentsdx-vscode-tunnel.service

echo "vscode tunnel provisioning complete"
