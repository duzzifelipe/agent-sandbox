#!/bin/bash
# Base provisioner: installs minimal tools and configures root SSH access.
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

systemctl enable ssh

mkdir -p /root/.ssh
chmod 700 /root/.ssh
touch /root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys

# Allow root login with authorized keys; no password.
sed -i 's/^#*PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
sed -i 's/^#*PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config

echo "base provisioning complete"
