# Plan 2b — VMProvider Interface + VirtualBox Implementation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the VMProvider interface and a VirtualBox backend that wraps VBoxManage, plus the supporting NoCloud ISO generator and images store.

**Architecture:** `server/internal/vm/` contains an interface (`VMProvider`) plus a VirtualBox implementation that shells out to `VBoxManage`. A separate `ImageStore` reads `images.json` from the data directory to resolve profile names to OVA file paths. NoCloud ISOs are generated in pure Go using `github.com/kdomanski/iso9660` and mounted to VMs at boot to deliver SSH keys and user-data.

**Tech Stack:** Go, VBoxManage CLI (external), `github.com/kdomanski/iso9660`, `os/exec`, `encoding/json`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `server/internal/vm/provider.go` | VMProvider interface + shared types (CreateVMRequest, VM) |
| `server/internal/vm/images.go` | Load/save `images.json`; resolve profile → OVA path |
| `server/internal/vm/nocloud.go` | Generate NoCloud ISO (meta-data + user-data) in a temp dir |
| `server/internal/vm/vboxmanage.go` | Low-level `runVBoxManage` helper + output parsers |
| `server/internal/vm/virtualbox.go` | VirtualBoxProvider implementing VMProvider |
| `server/internal/vm/images_test.go` | Unit tests for ImageStore |
| `server/internal/vm/nocloud_test.go` | Unit tests for NoCloud ISO generation |
| `server/internal/vm/vboxmanage_test.go` | Unit tests for output parsers |
| `server/internal/vm/virtualbox_test.go` | Integration tests (skipped if VBoxManage not in PATH) |

---

## Context

### Existing packages
- `server/internal/db` — `db.Open(path) (*sql.DB, error)`
- `server/internal/vault` — `DeriveKey`, `Encrypt`, `Decrypt`, `StoreVaultData`, `LoadVaultData`
- `server/internal/profile` — `profile.Store` (YAML + SQLite CRUD)
- `server/go.mod` — module `github.com/duck-labs/agentsdx-server`

### shared types (already defined)
```go
// shared/types/api.go
type ImageEntry struct {
    ProfileName string `json:"profile_name"`
    VirtualBox  string `json:"virtualbox"`
    Hetzner     string `json:"hetzner"`
}
```

### VBoxManage commands used
- `VBoxManage import <ova> --vsys 0 --vmname <name>` — import OVA
- `VBoxManage modifyvm <name> --nic1 hostonly --hostonlyadapter1 vboxnet0` — host-only network
- `VBoxManage storagectl <name> --name IDE --add ide` — add IDE controller (if not present)
- `VBoxManage storageattach <name> --storagectl IDE --port 1 --device 0 --type dvddrive --medium <iso>` — mount NoCloud ISO
- `VBoxManage startvm <name> --type headless` — start VM
- `VBoxManage controlvm <name> poweroff` — force stop
- `VBoxManage unregistervm <name> --delete` — delete VM and all files
- `VBoxManage showvminfo <name> --machinereadable` — VM state and properties
- `VBoxManage guestproperty get <name> /VirtualBox/GuestInfo/Net/0/V4/IP` — get VM IP (requires VirtualBox Guest Additions in image)

### images.json format (stored at `<dataDir>/images.json`)
```json
{
  "work-backend": {
    "virtualbox": "/data/images/work-backend.ova",
    "hetzner": ""
  }
}
```

---

## Task 1: VMProvider interface and shared VM types

**Files:**
- Create: `server/internal/vm/provider.go`

- [ ] **Step 1: Write the file**

```go
// Package vm defines the VMProvider interface and supporting types.
package vm

import "context"

type VMProvider interface {
    CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error)
    DestroyVM(ctx context.Context, vmID string) error
    GetVM(ctx context.Context, vmID string) (*VM, error)
}

type CreateVMRequest struct {
    ProfileName   string
    AuthorizedKey string // VM access public key placed in authorized_keys
    UserData      string // cloud-init user-data content
}

type VM struct {
    ID        string
    IPAddress string
    State     string // "starting" | "running" | "stopped" | "unknown"
}

const (
    VMStateStarting = "starting"
    VMStateRunning  = "running"
    VMStateStopped  = "stopped"
    VMStateUnknown  = "unknown"
)
```

- [ ] **Step 2: Verify compilation**

```bash
cd server && mise exec -- go build ./internal/vm/...
```
Expected: no output (clean compile)

- [ ] **Step 3: Commit**

```bash
git add server/internal/vm/provider.go
git commit -m "feat: add VMProvider interface and VM types"
```

---

## Task 2: Images store

**Files:**
- Create: `server/internal/vm/images.go`
- Create: `server/internal/vm/images_test.go`

- [ ] **Step 1: Write the failing test**

```go
package vm_test

import (
    "encoding/json"
    "os"
    "path/filepath"
    "testing"

    "github.com/duck-labs/agentsdx-server/internal/vm"
)

func writeImagesJSON(t *testing.T, dir string, data map[string]vm.ImageRecord) string {
    t.Helper()
    path := filepath.Join(dir, "images.json")
    b, _ := json.Marshal(data)
    _ = os.WriteFile(path, b, 0644)
    return path
}

func TestImageStore_GetVirtualBoxPath_Found(t *testing.T) {
    dir := t.TempDir()
    writeImagesJSON(t, dir, map[string]vm.ImageRecord{
        "my-profile": {VirtualBox: "/data/images/my-profile.ova"},
    })

    store := vm.NewImageStore(filepath.Join(dir, "images.json"))
    path, err := store.GetVirtualBoxPath("my-profile")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if path != "/data/images/my-profile.ova" {
        t.Errorf("got %q, want %q", path, "/data/images/my-profile.ova")
    }
}

func TestImageStore_GetVirtualBoxPath_NotFound(t *testing.T) {
    dir := t.TempDir()
    writeImagesJSON(t, dir, map[string]vm.ImageRecord{})

    store := vm.NewImageStore(filepath.Join(dir, "images.json"))
    _, err := store.GetVirtualBoxPath("missing")
    if err == nil {
        t.Fatal("expected error for missing profile")
    }
}

func TestImageStore_GetVirtualBoxPath_EmptyPath(t *testing.T) {
    dir := t.TempDir()
    writeImagesJSON(t, dir, map[string]vm.ImageRecord{
        "no-image": {VirtualBox: ""},
    })

    store := vm.NewImageStore(filepath.Join(dir, "images.json"))
    _, err := store.GetVirtualBoxPath("no-image")
    if err == nil {
        t.Fatal("expected error for empty virtualbox path")
    }
}

func TestImageStore_SetVirtualBoxPath(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "images.json")
    store := vm.NewImageStore(path)

    if err := store.SetVirtualBoxPath("my-profile", "/data/images/my-profile.ova"); err != nil {
        t.Fatalf("SetVirtualBoxPath: %v", err)
    }

    got, err := store.GetVirtualBoxPath("my-profile")
    if err != nil {
        t.Fatalf("GetVirtualBoxPath after set: %v", err)
    }
    if got != "/data/images/my-profile.ova" {
        t.Errorf("got %q, want %q", got, "/data/images/my-profile.ova")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && mise exec -- go test ./internal/vm/... -run TestImageStore -v
```
Expected: compile error (ImageStore not defined yet)

- [ ] **Step 3: Write the implementation**

```go
package vm

import (
    "encoding/json"
    "fmt"
    "os"
)

// ImageRecord holds built image paths per provider for a single profile.
type ImageRecord struct {
    VirtualBox string `json:"virtualbox"`
    Hetzner    string `json:"hetzner"`
}

// ImageStore reads and writes images.json.
type ImageStore struct {
    path string
}

// NewImageStore creates an ImageStore backed by the given file path.
func NewImageStore(path string) *ImageStore {
    return &ImageStore{path: path}
}

// GetVirtualBoxPath returns the OVA path for profileName or an error if absent.
func (s *ImageStore) GetVirtualBoxPath(profileName string) (string, error) {
    records, err := s.load()
    if err != nil {
        return "", fmt.Errorf("load images: %w", err)
    }
    rec, ok := records[profileName]
    if !ok {
        return "", fmt.Errorf("no image record for profile %q", profileName)
    }
    if rec.VirtualBox == "" {
        return "", fmt.Errorf("no virtualbox image built for profile %q", profileName)
    }
    return rec.VirtualBox, nil
}

// SetVirtualBoxPath writes or updates the VirtualBox OVA path for profileName.
func (s *ImageStore) SetVirtualBoxPath(profileName, ovaPath string) error {
    records, err := s.load()
    if err != nil && !os.IsNotExist(err) {
        return fmt.Errorf("load images: %w", err)
    }
    if records == nil {
        records = make(map[string]ImageRecord)
    }
    rec := records[profileName]
    rec.VirtualBox = ovaPath
    records[profileName] = rec
    return s.save(records)
}

func (s *ImageStore) load() (map[string]ImageRecord, error) {
    data, err := os.ReadFile(s.path)
    if err != nil {
        return nil, err
    }
    var records map[string]ImageRecord
    if err := json.Unmarshal(data, &records); err != nil {
        return nil, fmt.Errorf("parse images.json: %w", err)
    }
    return records, nil
}

func (s *ImageStore) save(records map[string]ImageRecord) error {
    data, err := json.MarshalIndent(records, "", "  ")
    if err != nil {
        return fmt.Errorf("marshal images: %w", err)
    }
    return os.WriteFile(s.path, data, 0644)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd server && mise exec -- go test ./internal/vm/... -run TestImageStore -v
```
Expected: 4 tests PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/vm/images.go server/internal/vm/images_test.go
git commit -m "feat: add ImageStore for images.json resolution"
```

---

## Task 3: NoCloud ISO generator

**Files:**
- Create: `server/internal/vm/nocloud.go`
- Create: `server/internal/vm/nocloud_test.go`

The NoCloud data source for cloud-init delivers VM configuration via an ISO mounted as a CD-ROM. The ISO must have the volume label `cidata` and contain two files: `meta-data` and `user-data`.

- [ ] **Step 1: Add iso9660 dependency**

```bash
cd server && mise exec -- go get github.com/kdomanski/iso9660@latest
```

- [ ] **Step 2: Write the failing test**

```go
package vm_test

import (
    "os"
    "testing"

    "github.com/duck-labs/agentsdx-server/internal/vm"
    "github.com/kdomanski/iso9660"
)

func TestWriteNoCloudISO_CreatesFile(t *testing.T) {
    dir := t.TempDir()
    metaData := "instance-id: test\nlocal-hostname: test-vm\n"
    userData := "#cloud-config\nssh_authorized_keys:\n  - ssh-rsa AAAA...\n"

    isoPath, err := vm.WriteNoCloudISO(dir, metaData, userData)
    if err != nil {
        t.Fatalf("WriteNoCloudISO: %v", err)
    }

    if _, err := os.Stat(isoPath); err != nil {
        t.Fatalf("ISO file not found: %v", err)
    }
}

func TestWriteNoCloudISO_ContainsFiles(t *testing.T) {
    dir := t.TempDir()
    metaData := "instance-id: abc\nlocal-hostname: my-vm\n"
    userData := "#cloud-config\npackages:\n  - git\n"

    isoPath, err := vm.WriteNoCloudISO(dir, metaData, userData)
    if err != nil {
        t.Fatalf("WriteNoCloudISO: %v", err)
    }

    f, err := os.Open(isoPath)
    if err != nil {
        t.Fatalf("open ISO: %v", err)
    }
    defer f.Close()

    img, err := iso9660.OpenImage(f)
    if err != nil {
        t.Fatalf("open iso9660 image: %v", err)
    }

    root, err := img.RootDir()
    if err != nil {
        t.Fatalf("root dir: %v", err)
    }
    children, err := root.GetChildren()
    if err != nil {
        t.Fatalf("get children: %v", err)
    }

    names := make(map[string]bool)
    for _, c := range children {
        names[c.Name()] = true
    }
    for _, want := range []string{"meta-data", "user-data"} {
        if !names[want] {
            t.Errorf("ISO missing file %q; got names: %v", want, names)
        }
    }
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd server && mise exec -- go test ./internal/vm/... -run TestWriteNoCloudISO -v
```
Expected: compile error (WriteNoCloudISO not defined)

- [ ] **Step 4: Write the implementation**

```go
package vm

import (
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "github.com/kdomanski/iso9660"
)

// WriteNoCloudISO creates a NoCloud data source ISO at dir/nocloud.iso.
// The ISO has volume label "cidata" and contains meta-data and user-data files.
// Returns the absolute path to the generated ISO.
func WriteNoCloudISO(dir, metaData, userData string) (string, error) {
    writer, err := iso9660.NewWriter()
    if err != nil {
        return "", fmt.Errorf("new iso writer: %w", err)
    }
    defer writer.Cleanup()

    if err := writer.AddFile(strings.NewReader(metaData), "meta-data"); err != nil {
        return "", fmt.Errorf("add meta-data: %w", err)
    }
    if err := writer.AddFile(strings.NewReader(userData), "user-data"); err != nil {
        return "", fmt.Errorf("add user-data: %w", err)
    }

    isoPath := filepath.Join(dir, "nocloud.iso")
    f, err := os.OpenFile(isoPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
    if err != nil {
        return "", fmt.Errorf("create iso file: %w", err)
    }
    defer f.Close()

    if err := writer.WriteTo(f, "cidata"); err != nil {
        return "", fmt.Errorf("write iso: %w", err)
    }
    return isoPath, nil
}

// NoCloudMetaData returns minimal cloud-init meta-data for the given instance ID.
func NoCloudMetaData(instanceID string) string {
    return fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", instanceID, instanceID)
}

// NoCloudUserData returns cloud-init user-data that installs an SSH authorized key.
func NoCloudUserData(authorizedKey string) string {
    return fmt.Sprintf("#cloud-config\nssh_authorized_keys:\n  - %s\n", authorizedKey)
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd server && mise exec -- go test ./internal/vm/... -run TestWriteNoCloudISO -v
```
Expected: 2 tests PASS

- [ ] **Step 6: Commit**

```bash
git add server/internal/vm/nocloud.go server/internal/vm/nocloud_test.go server/go.mod server/go.sum
git commit -m "feat: add NoCloud ISO generator for cloud-init delivery"
```

---

## Task 4: VBoxManage helpers and output parsers

**Files:**
- Create: `server/internal/vm/vboxmanage.go`
- Create: `server/internal/vm/vboxmanage_test.go`

- [ ] **Step 1: Write the failing test**

```go
package vm_test

import (
    "testing"

    "github.com/duck-labs/agentsdx-server/internal/vm"
)

func TestParseVMInfo_Running(t *testing.T) {
    output := `VMState="running"
VMStateChangeTime="2026-05-14T10:00:00.000000000"
name="agentsdx-abc123"
`
    info := vm.ParseVMInfo(output)
    if info["VMState"] != "running" {
        t.Errorf("VMState: got %q, want %q", info["VMState"], "running")
    }
    if info["name"] != "agentsdx-abc123" {
        t.Errorf("name: got %q, want %q", info["name"], "agentsdx-abc123")
    }
}

func TestParseVMInfo_PoweredOff(t *testing.T) {
    output := `VMState="poweroff"
name="agentsdx-xyz"
`
    info := vm.ParseVMInfo(output)
    if info["VMState"] != "poweroff" {
        t.Errorf("VMState: got %q, want %q", info["VMState"], "poweroff")
    }
}

func TestParseGuestProperty_Found(t *testing.T) {
    output := "Value: 192.168.56.101\n"
    val, ok := vm.ParseGuestProperty(output)
    if !ok {
        t.Fatal("expected ok=true")
    }
    if val != "192.168.56.101" {
        t.Errorf("got %q, want %q", val, "192.168.56.101")
    }
}

func TestParseGuestProperty_NoValue(t *testing.T) {
    output := "No value set!\n"
    _, ok := vm.ParseGuestProperty(output)
    if ok {
        t.Fatal("expected ok=false for no value")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && mise exec -- go test ./internal/vm/... -run TestParseVMInfo -v
```
Expected: compile error

- [ ] **Step 3: Write the implementation**

```go
package vm

import (
    "context"
    "fmt"
    "os/exec"
    "strings"
)

// runVBoxManage executes VBoxManage with the given arguments and returns combined output.
func runVBoxManage(ctx context.Context, args ...string) (string, error) {
    cmd := exec.CommandContext(ctx, "VBoxManage", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return "", fmt.Errorf("VBoxManage %v: %w\noutput: %s", args, err, out)
    }
    return string(out), nil
}

// ParseVMInfo parses the --machinereadable output of VBoxManage showvminfo.
// Returns a map of key → value with surrounding quotes stripped.
func ParseVMInfo(output string) map[string]string {
    result := make(map[string]string)
    for _, line := range strings.Split(output, "\n") {
        idx := strings.Index(line, "=")
        if idx < 0 {
            continue
        }
        key := line[:idx]
        val := strings.Trim(line[idx+1:], `"`)
        result[key] = val
    }
    return result
}

// ParseGuestProperty parses VBoxManage guestproperty get output.
// Returns the value and true if a value was found, or "", false if not set.
func ParseGuestProperty(output string) (string, bool) {
    output = strings.TrimSpace(output)
    if !strings.HasPrefix(output, "Value:") {
        return "", false
    }
    return strings.TrimSpace(strings.TrimPrefix(output, "Value:")), true
}
```

- [ ] **Step 4: Run tests**

```bash
cd server && mise exec -- go test ./internal/vm/... -run "TestParseVMInfo|TestParseGuestProperty" -v
```
Expected: 4 tests PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/vm/vboxmanage.go server/internal/vm/vboxmanage_test.go
git commit -m "feat: add VBoxManage helpers and output parsers"
```

---

## Task 5: VirtualBox VMProvider implementation

**Files:**
- Create: `server/internal/vm/virtualbox.go`
- Create: `server/internal/vm/virtualbox_test.go`

The VirtualBox implementation uses host-only networking (`vboxnet0`). The VM gets its IP from the VirtualBox DHCP server for the host-only interface and reports it via VirtualBox Guest Additions (Guest Properties). The Packer-built image must include VirtualBox Guest Additions.

- [ ] **Step 1: Write integration test (skips if VBoxManage not in PATH)**

```go
package vm_test

import (
    "context"
    "os/exec"
    "testing"

    "github.com/duck-labs/agentsdx-server/internal/vm"
)

func skipIfNoVBoxManage(t *testing.T) {
    t.Helper()
    if _, err := exec.LookPath("VBoxManage"); err != nil {
        t.Skip("VBoxManage not found in PATH — skipping integration test")
    }
}

func TestVirtualBoxProvider_ImplementsInterface(t *testing.T) {
    dir := t.TempDir()
    images := vm.NewImageStore(dir + "/images.json")
    var _ vm.VMProvider = vm.NewVirtualBoxProvider(images, dir)
}

func TestVirtualBoxProvider_GetVM_NotFound(t *testing.T) {
    skipIfNoVBoxManage(t)
    dir := t.TempDir()
    images := vm.NewImageStore(dir + "/images.json")
    provider := vm.NewVirtualBoxProvider(images, dir)

    _, err := provider.GetVM(context.Background(), "agentsdx-nonexistent-vm-xyz")
    if err == nil {
        t.Fatal("expected error for non-existent VM")
    }
}
```

- [ ] **Step 2: Run test to verify compilation**

```bash
cd server && mise exec -- go test ./internal/vm/... -run TestVirtualBoxProvider -v
```
Expected: compile error (VirtualBoxProvider not defined)

- [ ] **Step 3: Write the implementation**

```go
package vm

import (
    "context"
    "fmt"
    "os"
    "strings"
    "time"
)

// VirtualBoxProvider implements VMProvider using VBoxManage.
// The Packer-built VM image must have VirtualBox Guest Additions installed
// so that guest properties (including the IP address) are available.
type VirtualBoxProvider struct {
    images  *ImageStore
    isoDir  string // directory for temporary NoCloud ISOs
}

// NewVirtualBoxProvider creates a VirtualBoxProvider.
// isoDir is used to write temporary NoCloud ISOs before attaching them to VMs.
func NewVirtualBoxProvider(images *ImageStore, isoDir string) *VirtualBoxProvider {
    return &VirtualBoxProvider{images: images, isoDir: isoDir}
}

// CreateVM imports the profile's OVA, configures host-only networking,
// attaches a NoCloud ISO with the authorized key, and starts the VM headlessly.
// The returned VM has State=VMStateStarting; call GetVM to poll until running.
func (p *VirtualBoxProvider) CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error) {
    ovaPath, err := p.images.GetVirtualBoxPath(req.ProfileName)
    if err != nil {
        return nil, fmt.Errorf("resolve image: %w", err)
    }

    vmName := fmt.Sprintf("agentsdx-%s-%d", req.ProfileName, time.Now().UnixMilli())

    // Import OVA
    if _, err := runVBoxManage(ctx, "import", ovaPath, "--vsys", "0", "--vmname", vmName); err != nil {
        return nil, fmt.Errorf("import ova: %w", err)
    }

    // Configure host-only networking
    if _, err := runVBoxManage(ctx, "modifyvm", vmName, "--nic1", "hostonly", "--hostonlyadapter1", "vboxnet0"); err != nil {
        _ = p.forceDelete(ctx, vmName)
        return nil, fmt.Errorf("configure network: %w", err)
    }

    // Generate and attach NoCloud ISO
    isoPath, err := WriteNoCloudISO(
        p.isoDir,
        NoCloudMetaData(vmName),
        NoCloudUserData(req.AuthorizedKey),
    )
    if err != nil {
        _ = p.forceDelete(ctx, vmName)
        return nil, fmt.Errorf("generate nocloud iso: %w", err)
    }

    if _, err := runVBoxManage(ctx, "storageattach", vmName,
        "--storagectl", "IDE",
        "--port", "1",
        "--device", "0",
        "--type", "dvddrive",
        "--medium", isoPath,
    ); err != nil {
        _ = p.forceDelete(ctx, vmName)
        _ = os.Remove(isoPath)
        return nil, fmt.Errorf("attach nocloud iso: %w", err)
    }

    // Start VM
    if _, err := runVBoxManage(ctx, "startvm", vmName, "--type", "headless"); err != nil {
        _ = p.forceDelete(ctx, vmName)
        return nil, fmt.Errorf("start vm: %w", err)
    }

    return &VM{ID: vmName, State: VMStateStarting}, nil
}

// DestroyVM powers off and deletes the VM unconditionally.
func (p *VirtualBoxProvider) DestroyVM(ctx context.Context, vmID string) error {
    return p.forceDelete(ctx, vmID)
}

// GetVM returns current state and IP of the VM.
// IPAddress is populated only when the VM is running and has reported its IP via Guest Additions.
func (p *VirtualBoxProvider) GetVM(ctx context.Context, vmID string) (*VM, error) {
    out, err := runVBoxManage(ctx, "showvminfo", vmID, "--machinereadable")
    if err != nil {
        return nil, fmt.Errorf("showvminfo: %w", err)
    }

    info := ParseVMInfo(out)
    rawState := info["VMState"]
    state := mapVMState(rawState)

    vm := &VM{ID: vmID, State: state}

    if state == VMStateRunning {
        ipOut, err := runVBoxManage(ctx, "guestproperty", "get", vmID, "/VirtualBox/GuestInfo/Net/0/V4/IP")
        if err == nil {
            if ip, ok := ParseGuestProperty(ipOut); ok {
                vm.IPAddress = ip
            }
        }
    }

    return vm, nil
}

func (p *VirtualBoxProvider) forceDelete(ctx context.Context, vmID string) error {
    // Try graceful poweroff first; ignore errors (VM may already be off)
    _, _ = runVBoxManage(ctx, "controlvm", vmID, "poweroff")
    time.Sleep(500 * time.Millisecond)
    _, err := runVBoxManage(ctx, "unregistervm", vmID, "--delete")
    if err != nil {
        return fmt.Errorf("unregistervm: %w", err)
    }
    return nil
}

func mapVMState(raw string) string {
    switch strings.ToLower(raw) {
    case "running":
        return VMStateRunning
    case "poweroff", "aborted", "saved":
        return VMStateStopped
    case "starting", "restoring":
        return VMStateStarting
    default:
        return VMStateUnknown
    }
}
```

- [ ] **Step 4: Run tests**

```bash
cd server && mise exec -- go test ./internal/vm/... -v
```
Expected: all unit tests PASS; integration tests SKIP (unless VBoxManage is installed)

- [ ] **Step 5: Verify all tests pass**

```bash
cd server && mise exec -- go test ./... -v 2>&1 | grep -E "^(ok|FAIL|---)"
```
Expected: all packages ok, no FAIL

- [ ] **Step 6: Commit**

```bash
git add server/internal/vm/virtualbox.go server/internal/vm/virtualbox_test.go
git commit -m "feat: add VirtualBoxProvider implementing VMProvider"
```
