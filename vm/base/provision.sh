#!/bin/bash
# Base provisioner: installs minimal tools needed by all profiles.
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

apt-get update -y
apt-get install -y \
    curl \
    wget \
    git \
    ca-certificates \
    jq \
    tar \
    openssh-server

id ubuntu 2>/dev/null || useradd -m -s /bin/bash -u 1000 ubuntu
mkdir -p /home/ubuntu
chown 1000:1000 /home/ubuntu

cat > /usr/local/bin/agentsdx-check-repos.sh << 'SCRIPT'
#!/bin/bash
# Check every git repo under /home/ubuntu for uncommitted or unpushed changes.
# Exits 0 if the user confirms shutdown (or nothing is dirty).
# Exits 1 if the user wants to stay in the session.
set -uo pipefail

dirty_repos=()

while IFS= read -r -d '' git_dir; do
    repo_dir="$(dirname "$git_dir")"
    pushd "$repo_dir" >/dev/null

    # Uncommitted changes (tracked or untracked files).
    if [[ -n "$(git status --porcelain 2>/dev/null)" ]]; then
        dirty_repos+=("$repo_dir:uncommitted")
        popd >/dev/null
        continue
    fi

    # Commits not yet pushed to any remote tracking branch.
    if git rev-parse --abbrev-ref '@{u}' &>/dev/null; then
        if [[ -n "$(git log '@{u}..HEAD' --oneline 2>/dev/null)" ]]; then
            dirty_repos+=("$repo_dir:unpushed")
        fi
    fi

    popd >/dev/null
done < <(find /home/ubuntu -maxdepth 4 -name ".git" -type d -print0 2>/dev/null)

if [[ ${#dirty_repos[@]} -eq 0 ]]; then
    exit 0
fi

echo ""
echo "⚠  The following repos have uncommitted or unpushed changes:"
for entry in "${dirty_repos[@]}"; do
    repo="${entry%%:*}"
    reason="${entry##*:}"
    echo "   • $repo  ($reason)"
done
echo ""
read -r -p "Shut down anyway? [y/N] " answer
echo ""

if [[ "${answer,,}" == "y" ]]; then
    exit 0
fi

echo "Session kept open. Commit and push your changes, then type 'exit' again."
exit 1
SCRIPT
chmod +x /usr/local/bin/agentsdx-check-repos.sh

cat > /usr/local/bin/agentsdx-session.sh << 'SCRIPT'
#!/bin/bash
# Wraps the interactive shell for every agent profile.
# Re-opens bash until the user confirms they are ready to shut down.
set -euo pipefail

if [[ -f /etc/agentsdx.env ]]; then
    set -a
    source /etc/agentsdx.env
    set +a
fi

chmod 600 ~/.ssh/id_rsa
chmod 700 ~/.ssh

while true; do
    bash
    if /usr/local/bin/agentsdx-check-repos.sh; then
        break
    fi
done
SCRIPT
chmod +x /usr/local/bin/agentsdx-session.sh

echo "base provisioning complete"
