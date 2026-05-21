# Hetzner Cloud Provider Design

**Date:** 2026-05-21  
**Status:** Approved

## Overview

Replace the local QEMU/Packer VM backend with Hetzner Cloud. `agentsdxd` runs on the user's machine and authenticates to the Hetzner Cloud API. Session VMs and image build VMs are `cx22` servers in the `nbg1` datacenter (configurable via env var).

QEMU, Packer, and the NoCloud ISO machinery are removed entirely. The cloud-init `user_data` mechanism is preserved — Hetzner accepts it natively as a string on server creation.

---

## Provider Interfaces

Two interfaces live in `server/internal/vm/`:

```go
// VMProvider — session lifecycle (unchanged)
type VMProvider interface {
    CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error)
    GetVM(ctx context.Context, vmID string) (*VM, error)
    DestroyVM(ctx context.Context, vmID string) error
}

// ImageProvider — image build lifecycle (new)
type ImageProvider interface {
    CreateBuildVM(ctx context.Context, baseImage, authorizedKey string) (*VM, error)
    SnapshotVM(ctx context.Context, vmID, snapshotName string) (string, error)
    DestroyBuildVM(ctx context.Context, vmID string) error
}
```

`HetznerProvider` implements both. Future providers (AWS, GCP) implement both to plug into the same builder and session manager.

---

## HetznerProvider (`server/internal/vm/hetzner.go`)

```go
type HetznerProvider struct {
    client   *hcloud.Client
    location string // default "nbg1"
}
```

**`CreateVM(req)`**
1. Create an hcloud SSH key resource named `agentsdx-{vmID}` with `req.AuthorizedKey`
2. Create a `cx22` server: image=`req.ImageID` (snapshot ID), location, SSH key, `user_data=req.UserData`
3. Return `VM{ID: serverID, IPAddress: publicIPv4, State: VMStateStarting}`

**`GetVM(vmID)`**
- Fetch server by ID from hcloud API
- Map `server.Status`: `"running"` → `VMStateRunning`, `"off"/"deleting"` → `VMStateStopped`, else → `VMStateStarting`
- Always populate `IPAddress`

**`DestroyVM(vmID)`**
- Delete the hcloud server
- Delete the associated SSH key resource (`agentsdx-{vmID}`)

**`CreateBuildVM(baseImage, authorizedKey)`**
- Create a `cx22` server from `baseImage` (e.g. `"ubuntu-24.04"`) with the given public key
- Poll hcloud server status until `"running"` (up to 5 min), then return `VM{IPAddress: ...}`
- Blocks until running so the builder can SSH in immediately after return

**`SnapshotVM(vmID, snapshotName)`**
- Call hcloud create-image API with type `snapshot` and description = `snapshotName`
- Poll until image action completes
- Return the image ID as a string

**`DestroyBuildVM(vmID)`**
- Delete the hcloud server (no SSH key to clean up — build VMs use a separate ephemeral key managed by the builder)

---

## Builder (`server/internal/builder/`)

```go
type Builder struct {
    vmDir    string
    images   *vm.ImageStore
    provider vm.ImageProvider
}
```

### `Build(ctx, profile)` flow

1. Generate ephemeral ED25519 key pair via `vault.GenerateKeyPair()`
2. `provider.CreateBuildVM(ctx, profile.Infrastructure.Image, pubKey)` — blocks until running
3. SSH connect to `vm.IPAddress:22` using the ephemeral private key (`golang.org/x/crypto/ssh`)
4. Upload `vmDir/` as a gzip tar stream over SSH into `/tmp/agentsdx-vm/`
5. Generate orchestration script via `writeOrchestrationScript` (unchanged), upload, `chmod +x`, execute — stream output to server log
6. `provider.SnapshotVM(ctx, vm.ID, profile.Name)` → `snapshotID`
7. `images.SetHetznerSnapshotID(profile.Name, snapshotID)`
8. `provider.DestroyBuildVM(ctx, vm.ID)`
9. Return `snapshotID`

`composeScripts` and `writeOrchestrationScript` are unchanged — they produce the same ordered shell script regardless of provider.

### `ImageBuilder` interface (in `api/handler.go`)

```go
type ImageBuilder interface {
    Build(ctx context.Context, profile types.ProfileSpec) (string, error)
}
```

---

## ImageStore (`server/internal/vm/images.go`)

Two new methods:

```go
func (s *ImageStore) GetHetznerSnapshotID(profileName string) (string, error)
func (s *ImageStore) SetHetznerSnapshotID(profileName, snapshotID string) error
```

Snapshot IDs are stored as opaque strings in `images.json`. `HetznerProvider.CreateVM` calls `GetHetznerSnapshotID` to resolve which snapshot to boot from.

---

## `CreateVMRequest` (`server/internal/vm/provider.go`)

`ImageID string` field added — the snapshot ID (or base image name for non-snapshot providers) to boot from. The session manager resolves this from `ImageStore.GetHetznerSnapshotID(profileName)` before calling `CreateVM`, keeping `HetznerProvider` decoupled from `ImageStore`.

---

## Session Manager (`server/internal/session/manager.go`)

- Receives `*vm.ImageStore` as a new constructor parameter
- `Start()` calls `images.GetHetznerSnapshotID(profileName)` and sets `CreateVMRequest.ImageID`
- `ReportReady` method removed
- `pollUntilRunning` simplified: only the `GetVM` polling path remains (no `/ready` callback fallback)

---

## API (`server/internal/api/handler.go`)

- `POST /sessions/{id}/ready` route and `sessionReady` handler removed
- `buildImage` handler calls `h.builder.Build(...)` instead of `h.builder.BuildQEMU(...)`

---

## `userdata.go` (renamed from `nocloud.go`)

- `WriteNoCloudISO` and `NoCloudMetaData` deleted
- `BuildUserData` unchanged — Hetzner accepts cloud-init user-data as a plain string on server creation

---

## `serve.go`

New env vars:

| Var | Required | Default | Description |
|---|---|---|---|
| `AGENTSDX_HCLOUD_TOKEN` | yes | — | Hetzner Cloud API token |
| `AGENTSDX_HCLOUD_LOCATION` | no | `nbg1` | Hetzner datacenter |

- One `*hcloud.Client` created from the token
- `HetznerProvider` passed as both `VMProvider` and `ImageProvider`
- `data/iso` and `data/qemu` dir creation removed
- `AGENTSDX_VM_DIR` retained (builder needs it to locate `vm/` scripts for upload)

---

## `setup.go`

Replaces QEMU/Packer checks with:
1. Verify `AGENTSDX_HCLOUD_TOKEN` is set
2. Make a lightweight hcloud API call to validate the token
3. Print success or a clear error message

---

## What's Removed

| Item | Location |
|---|---|
| `qemu.go` | `server/internal/vm/` — deleted |
| `qemu.pkr.hcl` | `vm/` — deleted |
| `WriteNoCloudISO`, `NoCloudMetaData` | `nocloud.go` — deleted |
| `iso9660` dependency | `server/go.mod` — dropped |
| `POST /sessions/{id}/ready` | API — removed |
| `Manager.ReportReady` | session manager — removed |
| `Builder.BuildQEMU` | builder — removed |
| `PackerRunner` interface | builder — removed |
| `data/iso`, `data/qemu` dirs | `serve.go` — removed |
| QEMU/Packer checks in setup | `setup.go` — replaced |

The `vm/` provisioning shell scripts are unchanged.

---

## New Dependencies

| Package | Use |
|---|---|
| `github.com/hetznercloud/hcloud-go/v2` | Hetzner Cloud API client |
| `golang.org/x/crypto/ssh` | SSH client for image build provisioning |
