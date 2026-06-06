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

echo "base provisioning complete"
