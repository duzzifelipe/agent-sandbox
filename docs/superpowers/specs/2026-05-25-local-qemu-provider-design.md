# Local QEMU Provider Design

**Date:** 2026-05-25

## Overview

Add a `local` VM provider that runs Ubuntu VMs on macOS arm64 using raw QEMU commands (no Packer). Both `VMProvider` and `ImageProvider` are implemented, giving full parity with the existing Hetzner provider. Both providers coexist in the server simultaneously; the profile's `infrastructure.provider` field determines which is used per session or build.

## Scope & Constraints

- macOS arm64 only: uses `-accel hvf` and the Homebrew EDK2 firmware at `/opt/homebrew/share/qemu/edk2-aarch64-code.fd`.
- `AGENTSDX_HCLOUD_TOKEN` becomes optional at startup. Hetzner provider is registered only if the token is present; requests for Hetzner profiles fail at runtime if it is not.
- One provider per profile: `infrastructure.provider` is singular. Building `claudin-fofin-local` and `claudin-fofin-hetzner` are separate profiles.

## Architecture

### Provider Registry

`Builder` and `Manager` each hold a `map[string]<ProviderInterface>` keyed by provider name (`"hetzner"`, `"local"`). At startup, `serve.go` populates both maps. If a profile references a provider not in the map, the operation fails with a clear error.

### New Components

**`server/internal/vm/local.go`** — `LocalProvider` implementing `VMProvider` and `ImageProvider`.

**`server/internal/db/` qemu_vms table** — persists running QEMU process state across server restarts:

```sql
CREATE TABLE IF NOT EXISTS qemu_vms (
  id           TEXT PRIMARY KEY,
  pid          INTEGER NOT NULL,
  ssh_port     INTEGER NOT NULL,
  overlay_path TEXT NOT NULL,
  seed_iso_path TEXT NOT NULL,
  created_at   DATETIME DEFAULT CURRENT_TIMESTAMP
)
```

### Modified Components

| File | Change |
|------|--------|
| `vm/provider.go` | Add `SSHPort int` to `VM` struct (0 = port 22) |
| `vm/images.go` | Replace `GetHetznerSnapshotID`/`SetHetznerSnapshotID` with `GetImageID(provider, profileName)` / `SetImageID(provider, profileName, id)`. Add `ProviderLocal = "local"`. |
| `shared/types/api.go` | Add `Local string \`json:"local,omitempty"\`` to `ImageEntry` |
| `session/manager.go` | Accept `map[string]vm.VMProvider`; `Start` takes `types.ProfileSpec` instead of `profileName` |
| `builder/builder.go` | Accept `map[string]vm.ImageProvider`; compute SSH addr from `buildVM.SSHPort`; call `SetImageID` with provider from profile |
| `api/handler.go` | `createSession` loads profile before calling `sessions.Start(ctx, spec)` |
| `cmd/agentsdxd/serve.go` | Always register `LocalProvider`; register `HetznerProvider` only when token is present |
| `cmd/agentsdxd/setup.go` | Verify `qemu-system-aarch64` on PATH always; verify Hetzner token only if set |

## LocalProvider: Method Details

### ImageProvider

**`CreateBuildVM(ctx, baseImage, authorizedKey)`**

`baseImage` is a local filesystem path set in the profile as `infrastructure.image` (e.g. `/Users/me/jammy-server-cloudimg-arm64.img`).

1. Write `user-data` and `meta-data` to a temp directory.
2. Create seed ISO: `hdiutil makehybrid -o <tmpdir>/seed.iso -joliet -iso -default-volume-name cidata <tmpdir>`.
3. Create overlay: `qemu-img create -f qcow2 -b <baseImage> -F qcow2 <tmpdir>/build-overlay.qcow2`.
4. Pick a free port in range 10000–20000 via `net.Listen` probe.
5. Launch `qemu-system-aarch64` as a background process with `-nographic`, `-pidfile <tmpdir>/qemu.pid`, and SSH forwarding `hostfwd=tcp::<port>-:22`.
6. Retry SSH on `127.0.0.1:<port>` using existing `dialSSHWithRetry`.
7. Read PID from pidfile; insert row into `qemu_vms`.
8. Return `VM{ID: uuid, IPAddress: "127.0.0.1", SSHPort: port, State: VMStateRunning}`.

**`SnapshotVM(ctx, vmID, snapshotName)`**

1. Retrieve record from `qemu_vms`.
2. Kill QEMU process (SIGTERM, 10s grace, then SIGKILL).
3. Convert overlay to a self-contained image: `qemu-img convert -O qcow2 <overlay> <dataDir>/snapshots/<snapshotName>.qcow2`.
4. Delete overlay and seed ISO temp files; remove row from `qemu_vms`.
5. Return snapshot path as the image ID.

**`DestroyBuildVM(ctx, vmID)`**

Kill process, delete temp files (overlay + seed ISO), remove DB row.

### VMProvider

**`CreateVM(ctx, req)`**

`req.ImageID` is the snapshot path written by `SnapshotVM`.

1. Create session overlay: `qemu-img create -f qcow2 -b <snapshotPath> -F qcow2 <dataDir>/sessions/<uuid>-overlay.qcow2`.
2. Write cloud-init from `req.UserData` → seed ISO in a session-specific temp dir.
3. Pick free port; launch QEMU; insert into `qemu_vms`.
4. Return `VM{..., State: VMStateStarting}` immediately — Manager polls `GetVM`.

**`GetVM(ctx, vmID)`**

Look up PID from `qemu_vms`; test liveness with `syscall.Kill(pid, 0)`. Alive → `VMStateRunning`, dead or missing → `VMStateUnknown`.

**`DestroyVM(ctx, vmID)`**

Kill process, delete overlay and seed ISO, remove DB row.

## Data Flow: Image Build

```
POST /images/build {profile: "claudin-fofin-local"}
  → handler loads ProfileSpec (infrastructure.provider: "local", image: "/path/to/base.img")
  → builder.Build(ctx, spec)
      → imgProviders["local"].CreateBuildVM(ctx, "/path/to/base.img", pubKey)
      → SSH provision (upload vm/ dir, run orchestration script)
      → imgProviders["local"].SnapshotVM(ctx, vmID, "claudin-fofin-local")
          → qemu-img convert → data/qemu/snapshots/claudin-fofin-local.qcow2
      → images.SetImageID(ProviderLocal, "claudin-fofin-local", "/data/qemu/snapshots/...")
```

## Data Flow: Session Start

```
POST /sessions {profile_name: "claudin-fofin-local"}
  → handler loads ProfileSpec
  → manager.Start(ctx, spec)
      → images.GetImageID(ProviderLocal, "claudin-fofin-local") → snapshot path
      → vmProviders["local"].CreateVM(ctx, req)
          → creates overlay qcow2
          → launches QEMU on free port
          → stores PID+port in qemu_vms
      → polls GetVM until VMStateRunning
```

## Error Handling

- **Provider not configured:** if `spec.Infrastructure.Provider` is not in the registry, return `"provider %q not configured — check server credentials"`.
- **QEMU launch failure:** clean up temp files and DB row before returning error.
- **`SnapshotVM` convert failure:** overlay is already gone (process killed); error is surfaced to the builder, which already deferred `DestroyBuildVM`. The build must be re-run.
- **Port collision:** `findFreePort` probes with `net.Listen` before committing; retries up to 10 times before failing.

## Testing

`LocalProvider` shells out to `qemu-img`, `qemu-system-aarch64`, and `hdiutil`. These are isolated behind an `executor` interface (injectable in tests), mirroring how `HetznerProvider` uses narrow hcloud interfaces. `qemu_vms` CRUD gets unit tests alongside existing `db_test.go`. `Builder` and `Manager` tests require no structural changes — the map constructor accepts `map[string]fakeProvider`.
