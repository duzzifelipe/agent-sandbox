# Cloud Image Base for VM Builds

**Date:** 2026-05-20
**Status:** Approved

## Problem

The current Packer build downloads a ~2GB Ubuntu live server ISO, runs a full autoinstall (~30 min), and only then runs provisioners. Ubuntu Cloud Images are pre-built qcow2/raw images (~500MB) that boot directly — no installer required. Switching reduces download size by ~75% and build time from ~30 min to ~5 min.

## Scope

Build-time path only. Runtime session flow (nocloud ISOs, session manager, vault, QEMU provider) is untouched.

**Files changed:**
- `vm/qemu.pkr.hcl` — rewritten for cloud image source
- `server/internal/builder/builder.go` — cloud image registry, download/cache, seed ISO + ephemeral key generation
- `vm/base/provision.sh` — SSH setup block removed (cloud-init handles it)

**Files deleted:**
- `vm/autoinstall/user-data.yaml` — no longer needed

## Architecture

```
BuildQEMU()
  ├── ensureCloudImage()       → data/iso/<filename> (cached)
  ├── vault.GenerateKeyPair()  → ephemeral key pair (temp files)
  ├── vm.WriteNoCloudISO()     → seed ISO (temp file)
  ├── copy EFI vars (ARM64)    → temp writable vars file
  ├── writeOrchestrationScript() → vm/orchestrate.sh (unchanged)
  └── packer build             → data/images/<profile>.qcow2
        ├── file provisioner   → uploads vm/ to /tmp/agentsdx-vm/
        └── shell provisioner  → runs orchestrate.sh
```

## Cloud Image Registry

Replaces `isoRegistry` in `builder.go`. Same shape: `OS → arch → {URL, Checksum}`.

```
ubuntu-24.04:
  arm64: https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-arm64.img
  amd64: https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img
```

Checksums are SHA256, fetched from Ubuntu's `SHA256SUMS` at design time and hardcoded. On cache miss the downloader re-verifies before saving.

## Caching (`ensureCloudImage`)

```
func ensureCloudImage(url, checksum, destPath string) error
```

1. If `destPath` exists, compute its SHA256; if it matches `checksum`, return (cache hit).
2. Otherwise download `url` to a temp file in the same directory, verify SHA256, rename to `destPath`.

Images land in `data/iso/` (already created at server startup), named by the filename segment of the URL.

## Per-Build Orchestration

For each `BuildQEMU` call:

1. **Resolve cloud image** — `ensureCloudImage` → local path
2. **Generate ephemeral key pair** — `vault.GenerateKeyPair()`; private key written to `os.CreateTemp`
3. **Write seed ISO** — `vm.WriteNoCloudISO` with user-data:
   ```yaml
   #cloud-config
   bootcmd:
     - mkdir -p /root/.ssh
     - chmod 700 /root/.ssh
   write_files:
     - path: /root/.ssh/authorized_keys
       permissions: '0600'
       content: <ephemeral_public_key>
   ```
   Ubuntu cloud images already have `PermitRootLogin prohibit-password` set, so key-based root login works without sshd changes.
4. **Copy EFI vars (ARM64 only)** — copy `/opt/homebrew/share/qemu/edk2-aarch64-vars.fd` to a temp file (QEMU needs a writable copy per run).
5. **Write orchestration script** — unchanged from today.
6. **Run Packer** — see variables below.
7. **Clean up** — defer removal of temp key file, seed ISO, EFI vars copy, and orchestration script.

## Packer HCL Changes

**Variables removed:** `iso_url`, `iso_checksum`, `ssh_password`

**Variables added:**

| Variable | Description |
|---|---|
| `cloud_image_path` | Local absolute path to cached cloud image |
| `seed_iso_path` | Local absolute path to cloud-init seed ISO |
| `ssh_private_key_file` | Local absolute path to ephemeral private key |
| `efi_firmware_vars` | Writable EDK2 vars path (ARM64); empty string on amd64 |

**Source block changes:**
- `disk_image = true`
- `iso_url = var.cloud_image_path`, `iso_checksum = "none"`
- `ssh_username = "root"`, `ssh_private_key_file = var.ssh_private_key_file`
- `ssh_timeout = "10m"` (down from 30m)
- Remove `http_content`, `boot_command`, `boot_wait`
- `qemuargs` gains two entries:
  - Seed ISO as cdrom: `["-drive", "file=<seed_iso_path>,media=cdrom,readonly=on"]`
  - Writable EFI vars (ARM64): `["-drive", "if=pflash,format=raw,file=<efi_firmware_vars>"]`

The `file` provisioner and `shell` provisioner blocks are unchanged.

## base/provision.sh

Remove the SSH setup block (mkdir, chmod, authorized_keys, sshd_config edits, systemctl enable). Cloud-init handles all of this via the seed ISO. Keep the `apt-get install` block.

## Error Handling

- Download failure: return error from `ensureCloudImage`; partial download temp file is removed before returning
- Checksum mismatch: return explicit error with expected vs actual hash
- Key generation failure: return error before any Packer invocation
- EFI vars copy failure (ARM64): return error before Packer invocation
- All temp files are cleaned up via `defer os.Remove(...)` regardless of build outcome
