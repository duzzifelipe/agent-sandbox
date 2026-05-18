#!/usr/bin/env bash
# Packer adds -boot once=d unconditionally, but the QEMU ARM64 virt machine
# type doesn't support the -boot flag (boot order is managed by UEFI).
# This wrapper strips all -boot arguments before delegating to the real binary.
set -euo pipefail
ARGS=()
while [[ $# -gt 0 ]]; do
  if [[ "$1" == "-boot" ]]; then
    shift 2  # drop -boot and its value
    continue
  fi
  ARGS+=("$1")
  shift
done
exec qemu-system-aarch64 "${ARGS[@]}"
