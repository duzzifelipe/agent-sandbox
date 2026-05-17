# Apple VZ Provider Design

**Date:** 2026-05-17
**Status:** Approved

## Problem

VirtualBox does not work on Apple Silicon (darwin/arm64). The `hostonlyif` feature requires kernel extensions that are not supported on ARM Macs. The project needs a local VM provider that works on Apple Silicon while keeping VirtualBox for Linux hosts.

## Solution

Add Apple's Virtualization.framework as a third VM provider (`applevz`), peer to `virtualbox` and `hetzner`. On darwin/arm64 the server auto-selects the Apple VZ provider; on Linux it keeps VirtualBox. No user configuration required.

## Architecture

```
serve.go
  └── vm.NewProvider(images, isoDir)          ← platform factory
        ├── darwin/arm64 → applevz.NewProvider(...)
        └── linux        → vm.NewVirtualBoxProvider(...)

session.Manager  ──uses──▶  vm.VMProvider (interface, unchanged)
api.Handler      ──uses──▶  builder.Builder (gains BuildAppleVZ)
                 ──uses──▶  vm.ImageStore   (gains ProviderAppleVZ)
```

Everything above `VMProvider` — `session.Manager`, `api.Handler` — is unchanged.

## Provider: applevz

**Package:** `server/internal/vm/applevz/`
**Build constraint:** `//go:build darwin && arm64`
**Library:** `github.com/Code-Hex/vz`

### CreateVM

1. Resolve raw disk image path from `ImageStore.GetAppleVZPath`.
2. Copy image to a per-VM working file (each VM needs its own writable disk).
3. Write NoCloud ISO to `isoDir` using the existing `vm.WriteNoCloudISO`.
4. Configure `VZVirtualMachineConfiguration`:
   - 2 vCPUs, 2 GB RAM
   - `VZNATNetworkDeviceAttachment` (NAT networking — requires sudo or `com.apple.vm.networking` entitlement)
   - Virtio disk: per-VM disk copy (read-write)
   - Virtio disk: NoCloud ISO (read-only)
   - Virtio-serial console
5. Start the VM. Return `VM{ID: vmName, State: VMStateStarting}` immediately.

**VM ID format:** `agentsdx-<profile>-<timestamp-ms>` (same pattern as VirtualBox).

The provider holds an in-memory `map[string]*vz.VirtualMachine` and a `map[string]string` (vmID → IP) populated when the IP callback fires.

### GetVM

- IP present in map → `VMStateRunning` + IP
- VM running, no IP yet → `VMStateStarting`
- VM stopped → `VMStateStopped`

No Guest Additions polling; IP comes from the HTTP callback.

### DestroyVM

Stop the `vz.VirtualMachine`, delete the per-VM disk copy and NoCloud ISO.

## IP Discovery

**Primary mechanism:** VM callback via cloud-init.

`vm.BuildUserData` gains an additional `runcmd` step that fires after network is up:

```yaml
runcmd:
  - |
    IP=$(ip -4 addr show eth0 | grep -oP '(?<=inet\s)\d+(\.\d+){3}')
    curl -sf -X POST <serverURL>/sessions/<sessionID>/ip \
      -H 'Content-Type: application/json' \
      -d "{\"ip\":\"$IP\"}"
```

`serverURL` contains the host address as configured by the operator. From inside a NAT VM, `localhost` or `127.0.0.1` in `serverURL` will not resolve to the host — the VM must use the NAT gateway IP instead (Apple VZ NAT default: `192.168.64.1`). The `BuildUserData` function therefore accepts a separate `vmCallbackURL` parameter (derived from the gateway + server port) distinct from `serverURL` (which is used by the agent process inside the VM for its normal API calls and may use a DNS name or external IP).

**New API endpoint:** `POST /sessions/{id}/ip`
- Body: `{"ip": "192.168.64.x"}`
- Response: 204 No Content
- No auth (VM-to-host only; the NAT network is host-private)
- Writes IP directly to `session.Store`

**Alternative (not primary):** vmnet bridged/host networking for direct host↔VM reachability without callbacks. This is a documented alternative for operators who can grant the entitlement.

## Image Building

**New Packer template:** `vm/applevz.pkr.hcl`
- Builder: `qemu`, `arch = "aarch64"`, `machine_type = "virt"`
- Boot: UEFI via `edk2-aarch64` firmware
- Output: raw disk image (`.img`)
- No Guest Additions provisioner step
- Same autoinstall flow as VirtualBox (Ubuntu cloud-init over HTTP)

**New ISO registry entry in `builder.go`:**
```go
"ubuntu-24.04-arm64": {
    URL:      "https://cdimage.ubuntu.com/releases/24.04/release/ubuntu-24.04.2-live-server-arm64.iso",
    Checksum: "sha256:<arm64-checksum>",
}
```

**`Builder.BuildAppleVZ`:** Mirrors `BuildVirtualBox` — writes orchestration script, calls Packer with `applevz.pkr.hcl`, stores `.img` path via `ImageStore.SetAppleVZPath`.

## Files Changed

### New files

| File | Purpose |
|------|---------|
| `server/internal/vm/applevz/provider.go` | `AppleVZProvider` implementing `VMProvider` |
| `server/internal/vm/provider_factory_darwin_arm64.go` | `NewProvider()` → `applevz.NewProvider(...)` |
| `server/internal/vm/provider_factory_linux.go` | `NewProvider()` → `NewVirtualBoxProvider(...)` |
| `vm/applevz.pkr.hcl` | Packer QEMU ARM64 template |

### Modified files

| File | Change |
|------|--------|
| `server/internal/vm/images.go` | Add `ProviderAppleVZ`, `GetAppleVZPath`, `SetAppleVZPath` |
| `shared/types/api.go` | Add `AppleVZ string` to `ImageEntry` |
| `server/internal/builder/builder.go` | Add `BuildAppleVZ` method |
| `server/internal/api/handler.go` | Add `POST /sessions/{id}/ip` endpoint |
| `server/cmd/agentsdxd/setup.go` | Add darwin/arm64 path: skip vboxnet0, init QEMU Packer plugin |
| `server/cmd/agentsdxd/serve.go` | Replace hardcoded `NewVirtualBoxProvider` with `vm.NewProvider(...)` |
| `e2e/main_test.go` | Skip vboxnet0 setup on darwin/arm64 |

## Provider Selection

Two platform-tagged factory files export the same `NewProvider` function:

- `provider_factory_darwin_arm64.go` (`//go:build darwin && arm64`) — returns Apple VZ provider
- `provider_factory_linux.go` (`//go:build linux`) — returns VirtualBox provider

`serve.go` calls `vm.NewProvider(images, isoDir)` with no platform-specific logic.

## Setup Command

**darwin/arm64 path:**
1. Skip `setupVboxnet0` entirely
2. Run `packer init applevz.pkr.hcl` to install the QEMU plugin
3. Print note that `agentsdxd serve` requires sudo (or the `com.apple.vm.networking` entitlement) for vmnet

**Linux path:** unchanged (vboxnet0 + virtualbox Packer plugin).

## Testing

**Unit tests:** IP map logic and state transitions in `applevz/` are pure Go and fully testable without a real VM. VirtualMachine interactions are hidden behind an interface for mocking.

**e2e tests (`vm` build tag):** Provider-agnostic — go through CLI and HTTP API. `main_test.go` skips vboxnet0 setup on darwin/arm64. Same tests run against whichever provider is compiled in.

**e2e tests (`applevz` build tag):** Apple VZ-specific tests (IP callback, disk copy lifecycle). Only run on macOS Apple Silicon CI runners.

**CI:** Linux runners keep the existing `vm`-tagged e2e suite against VirtualBox (unchanged).
