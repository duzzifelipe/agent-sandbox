# Cloud Image Base Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the full Ubuntu live-server ISO + autoinstall flow with Ubuntu Cloud Images (~500 MB pre-built qcow2) to cut download size by ~75% and build time from ~30 min to ~5 min.

**Architecture:** The builder downloads and caches the cloud image in `data/iso/`. Each Packer build gets an ephemeral SSH key pair and a tiny cloud-init seed ISO for authentication; QEMU boots the cloud image directly (`disk_image = true`), cloud-init injects the key, and Packer SSHes in to run existing provisioners unchanged.

**Tech Stack:** Go (`crypto/sha256`, `net/http`), Packer QEMU plugin, HCL2 locals for conditional qemuargs, existing `vm.WriteNoCloudISO` and `vault.GenerateKeyPair`.

---

## File Map

| File | Change |
|---|---|
| `server/internal/builder/builder.go` | Replace `isoRegistry` with `cloudImageRegistry`; add `isoDir`/`imageRegistry` fields; add `ensureCloudImage`, `verifyChecksum`, `downloadFile`, `packerSeedUserData`, `copyEFIVars`; rewrite `BuildQEMU` |
| `server/internal/builder/builder_test.go` | Add `TestVerifyChecksum`, `TestPackerSeedUserData`; update `TestBuildQEMU_*` tests for new args and struct fields |
| `server/cmd/agentsdxd/serve.go` | Pass `isoDir` to `builder.New()` |
| `vm/qemu.pkr.hcl` | Rewrite for cloud image source, ephemeral SSH key, seed ISO cdrom |
| `vm/base/provision.sh` | Remove SSH setup block (cloud-init handles it) |
| `vm/autoinstall/user-data.yaml` | Delete |

---

### Task 1: Cloud image registry, checksum verification, download/cache

**Files:**
- Modify: `server/internal/builder/builder.go`
- Modify: `server/internal/builder/builder_test.go`
- Modify: `server/cmd/agentsdxd/serve.go`

- [ ] **Step 1: Write failing tests for `verifyChecksum` and `packerSeedUserData`**

Add to `server/internal/builder/builder_test.go` (add imports `"crypto/sha256"` and `"encoding/hex"` to the existing import block):

```go
func TestVerifyChecksum(t *testing.T) {
	content := []byte("hello agentsdx")
	f, err := os.CreateTemp("", "checksum-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.Write(content)
	f.Close()

	h := sha256.New()
	h.Write(content)
	good := "sha256:" + hex.EncodeToString(h.Sum(nil))

	if err := verifyChecksum(f.Name(), good); err != nil {
		t.Fatalf("expected no error for correct checksum, got: %v", err)
	}
	if err := verifyChecksum(f.Name(), "sha256:deadbeef"); err == nil {
		t.Fatal("expected error for wrong checksum, got nil")
	}
	if err := verifyChecksum(f.Name(), "md5:abc123"); err == nil {
		t.Fatal("expected error for unsupported checksum format, got nil")
	}
}

func TestPackerSeedUserData(t *testing.T) {
	pubKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItest"
	data := packerSeedUserData(pubKey)
	if !strings.HasPrefix(data, "#cloud-config") {
		t.Errorf("expected #cloud-config header, got: %q", data[:min(20, len(data))])
	}
	if !strings.Contains(data, "/root/.ssh/authorized_keys") {
		t.Error("expected authorized_keys path in user-data")
	}
	if !strings.Contains(data, pubKey) {
		t.Error("expected public key content in user-data")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd server && go test ./internal/builder/ -run "TestVerifyChecksum|TestPackerSeedUserData" -v
```

Expected: compile error (functions not defined yet).

- [ ] **Step 3: Replace `isoRegistry` with `cloudImageRegistry` and add helpers in `builder.go`**

Replace the entire `isoRegistry` block and add `cloudImageEntry` type, the new registry, `verifyChecksum`, `downloadFile`, `ensureCloudImage`, and `packerSeedUserData`. Also update the `Builder` struct and `New()` to accept `isoDir`, and add the injectable `imageRegistry` field.

Replace the import block at the top of `server/internal/builder/builder.go`:

```go
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/duck-labs/agentsdx-server/internal/vault"
	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/duck-labs/agentsdx-shared/types"
)
```

Replace the `Builder` struct and `New()`:

```go
// Builder orchestrates Packer image builds.
type Builder struct {
	vmDir         string
	outputDir     string
	isoDir        string
	images        *vm.ImageStore
	runner        PackerRunner
	imageRegistry map[string]map[string]cloudImageEntry // nil = use cloudImageRegistry
}

func New(vmDir, outputDir, isoDir string, images *vm.ImageStore) *Builder {
	return &Builder{
		vmDir:     vmDir,
		outputDir: outputDir,
		isoDir:    isoDir,
		images:    images,
		runner:    &realPackerRunner{},
	}
}
```

Replace the `isoRegistry` block with:

```go
type cloudImageEntry struct {
	URL      string
	Checksum string
}

var cloudImageRegistry = map[string]map[string]cloudImageEntry{
	"ubuntu-24.04": {
		"amd64": {
			URL:      "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img",
			Checksum: "sha256:6e7016f2c9f4d3c00f48789eb6b9043ba2172ccc1b6b1eaf3ed1e29dd3e52bb3",
		},
		"arm64": {
			URL:      "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-arm64.img",
			Checksum: "sha256:c7eff9b3ee6e7b212882e680a9e06cac939107fbf5298384340a0ad1c667a38a",
		},
	},
}
```

Add these functions after the registry (before `composeScripts`):

```go
func verifyChecksum(path, expected string) error {
	parts := strings.SplitN(expected, ":", 2)
	if len(parts) != 2 || parts[0] != "sha256" {
		return fmt.Errorf("unsupported checksum format %q", expected)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != parts[1] {
		return fmt.Errorf("checksum mismatch: want %s, got %s", parts[1], actual)
	}
	return nil
}

func downloadFile(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func (b *Builder) ensureCloudImage(ctx context.Context, url, checksum, destPath string) error {
	if _, err := os.Stat(destPath); err == nil {
		if verifyChecksum(destPath, checksum) == nil {
			return nil
		}
	}
	tmpPath := destPath + ".tmp"
	if err := downloadFile(ctx, url, tmpPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("download: %w", err)
	}
	if err := verifyChecksum(tmpPath, checksum); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("verify: %w", err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("save: %w", err)
	}
	return nil
}

func packerSeedUserData(publicKey string) string {
	return fmt.Sprintf(`#cloud-config
bootcmd:
  - mkdir -p /root/.ssh
  - chmod 700 /root/.ssh
write_files:
  - path: /root/.ssh/authorized_keys
    permissions: '0600'
    content: %s
`, strings.TrimSpace(publicKey))
}
```

- [ ] **Step 4: Update `serve.go` to pass `isoDir` to `builder.New()`**

In `server/cmd/agentsdxd/serve.go`, change:

```go
imageBuilder := builder.New(vmDir, filepath.Join(dataDir, "images"), images)
```

to:

```go
imageBuilder := builder.New(vmDir, filepath.Join(dataDir, "images"), filepath.Join(dataDir, "iso"), images)
```

- [ ] **Step 5: Run the new tests to confirm they pass**

```bash
cd server && go test ./internal/builder/ -run "TestVerifyChecksum|TestPackerSeedUserData" -v
```

Expected:
```
--- PASS: TestVerifyChecksum
--- PASS: TestPackerSeedUserData
```

- [ ] **Step 6: Confirm the project still compiles**

```bash
cd server && go build ./...
```

Expected: no output (success).

- [ ] **Step 7: Commit**

```bash
git add server/internal/builder/builder.go server/internal/builder/builder_test.go server/cmd/agentsdxd/serve.go
git commit -m "feat: cloud image registry, download/cache, seed user-data helpers"
```

---

### Task 2: Rewrite `BuildQEMU` to use cloud image flow

**Files:**
- Modify: `server/internal/builder/builder.go`
- Modify: `server/internal/builder/builder_test.go`

- [ ] **Step 1: Update the three existing `TestBuildQEMU_*` tests**

Replace all three test functions in `server/internal/builder/builder_test.go`. They need `isoDir`, the injectable `imageRegistry`, a pre-seeded cache file, and updated arg assertions:

```go
func testBuilder(t *testing.T, fake *fakeRunner) *Builder {
	t.Helper()
	vmDir := t.TempDir()
	outputDir := t.TempDir()
	isoDir := t.TempDir()
	imagesPath := filepath.Join(t.TempDir(), "images.json")
	imageStore := vm.NewImageStore(imagesPath)

	imgFilename := "noble-server-cloudimg-" + runtime.GOARCH + ".img"
	testContent := []byte("fake-cloud-image")
	h := sha256.New()
	h.Write(testContent)
	checksum := "sha256:" + hex.EncodeToString(h.Sum(nil))
	if err := os.WriteFile(filepath.Join(isoDir, imgFilename), testContent, 0644); err != nil {
		t.Fatal(err)
	}

	fake.vmDir = vmDir
	return &Builder{
		vmDir:     vmDir,
		outputDir: outputDir,
		isoDir:    isoDir,
		images:    imageStore,
		runner:    fake,
		imageRegistry: map[string]map[string]cloudImageEntry{
			"ubuntu-24.04": {
				runtime.GOARCH: {
					URL:      "https://example.com/" + imgFilename,
					Checksum: checksum,
				},
			},
		},
	}
}

func TestBuildQEMU_PassesCorrectArgs(t *testing.T) {
	fake := &fakeRunner{}
	b := testBuilder(t, fake)

	profile := types.ProfileSpec{
		Name: "test-profile",
		Infrastructure: types.InfrastructureConfig{
			Image:   "ubuntu-24.04",
			Tooling: []string{},
		},
		Agent: types.AgentConfig{Provider: "claude"},
	}

	_, err := b.BuildQEMU(context.Background(), profile)
	if err != nil {
		t.Fatalf("BuildQEMU: %v", err)
	}
	if fake.readErr != nil {
		t.Fatalf("fakeRunner failed to read orchestrate.sh: %v", fake.readErr)
	}

	argsStr := strings.Join(fake.capturedArgs, " ")
	for _, want := range []string{
		"-var=cloud_image_path=",
		"-var=seed_iso_path=",
		"-var=ssh_private_key_file=",
		"-var=vm_name=test-profile",
		"-var=provision_script=",
		"qemu.pkr.hcl",
	} {
		if !strings.Contains(argsStr, want) {
			t.Errorf("expected arg containing %q, got args: %v", want, fake.capturedArgs)
		}
	}
}

func TestBuildQEMU_PackerFailure_ReturnsError(t *testing.T) {
	fake := &fakeRunner{err: errors.New("packer failed")}
	b := testBuilder(t, fake)

	profile := types.ProfileSpec{
		Name: "failure-profile",
		Infrastructure: types.InfrastructureConfig{
			Image:   "ubuntu-24.04",
			Tooling: []string{},
		},
		Agent: types.AgentConfig{Provider: "claude"},
	}

	_, err := b.BuildQEMU(context.Background(), profile)
	if err == nil {
		t.Fatal("expected error from packer failure, got nil")
	}
	if !strings.Contains(err.Error(), "packer build") {
		t.Errorf("expected 'packer build' in error message, got: %v", err)
	}
}

func TestBuildQEMU_UnknownImage_ReturnsError(t *testing.T) {
	fake := &fakeRunner{}
	b := testBuilder(t, fake)

	profile := types.ProfileSpec{
		Name: "test-profile",
		Infrastructure: types.InfrastructureConfig{
			Image: "unknown-os-9.9",
		},
		Agent: types.AgentConfig{Provider: "claude"},
	}

	_, err := b.BuildQEMU(context.Background(), profile)
	if err == nil {
		t.Fatal("expected error for unknown image, got nil")
	}
	if !strings.Contains(err.Error(), "unknown base image") {
		t.Errorf("expected 'unknown base image' in error, got: %v", err)
	}
	if len(fake.capturedArgs) > 0 {
		t.Errorf("expected packer not to be called, but got args: %v", fake.capturedArgs)
	}
}
```

The `fakeRunner.vmDir` field is still used — `testBuilder` sets it and `fakeRunner.Run` reads it to find `orchestrate.sh`. Keep the field as-is.

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd server && go test ./internal/builder/ -run "TestBuildQEMU" -v
```

Expected: compile errors because `BuildQEMU` still references `isoRegistry` and the old arg names.

- [ ] **Step 3: Rewrite `BuildQEMU` and add `copyEFIVars`**

Replace `BuildQEMU` and `qemuPackerVars` in `server/internal/builder/builder.go`:

```go
func (b *Builder) BuildQEMU(ctx context.Context, profile types.ProfileSpec) (string, error) {
	arch := runtime.GOARCH

	reg := cloudImageRegistry
	if b.imageRegistry != nil {
		reg = b.imageRegistry
	}
	archImages, ok := reg[profile.Infrastructure.Image]
	if !ok {
		return "", fmt.Errorf("unknown base image %q", profile.Infrastructure.Image)
	}
	img, ok := archImages[arch]
	if !ok {
		return "", fmt.Errorf("no cloud image for %s/%s", profile.Infrastructure.Image, arch)
	}

	// Resolve cloud image (download if not cached).
	parts := strings.Split(img.URL, "/")
	filename := parts[len(parts)-1]
	cloudImagePath := filepath.Join(b.isoDir, filename)
	if err := b.ensureCloudImage(ctx, img.URL, img.Checksum, cloudImagePath); err != nil {
		return "", fmt.Errorf("ensure cloud image: %w", err)
	}

	// Ephemeral SSH key pair for Packer.
	privKey, pubKey, err := vault.GenerateKeyPair()
	if err != nil {
		return "", fmt.Errorf("generate packer key pair: %w", err)
	}
	privKeyFile, err := os.CreateTemp("", "agentsdx-packer-key-*")
	if err != nil {
		return "", fmt.Errorf("create packer key file: %w", err)
	}
	privKeyPath := privKeyFile.Name()
	defer os.Remove(privKeyPath)
	if _, err := privKeyFile.WriteString(privKey); err != nil {
		privKeyFile.Close()
		return "", fmt.Errorf("write packer key: %w", err)
	}
	privKeyFile.Close()
	if err := os.Chmod(privKeyPath, 0o600); err != nil {
		return "", fmt.Errorf("chmod packer key: %w", err)
	}

	// Cloud-init seed ISO so Packer can SSH into the cloud image.
	seedDir, err := os.MkdirTemp("", "agentsdx-seed-*")
	if err != nil {
		return "", fmt.Errorf("create seed dir: %w", err)
	}
	defer os.RemoveAll(seedDir)
	seedISOPath, err := vm.WriteNoCloudISO(seedDir, vm.NoCloudMetaData("packer"), packerSeedUserData(pubKey))
	if err != nil {
		return "", fmt.Errorf("write seed iso: %w", err)
	}

	// Writable EFI vars copy (ARM64 only).
	efiVarsPath := ""
	if arch == "arm64" {
		efiVarsPath, err = copyEFIVars()
		if err != nil {
			return "", fmt.Errorf("copy efi vars: %w", err)
		}
		defer os.Remove(efiVarsPath)
	}

	// Orchestration script.
	scripts := composeScripts(profile)
	orchScript, err := writeOrchestrationScript(scripts, profile.Agent.Provider)
	if err != nil {
		return "", err
	}
	defer os.Remove(orchScript)

	orchDest := filepath.Join(b.vmDir, "orchestrate.sh")
	data, err := os.ReadFile(orchScript)
	if err != nil {
		return "", fmt.Errorf("read orchestration script: %w", err)
	}
	if err := os.WriteFile(orchDest, data, 0o755); err != nil {
		return "", fmt.Errorf("write orchestration script to vmDir: %w", err)
	}
	defer os.Remove(orchDest)

	orchDestAbs, err := filepath.Abs(orchDest)
	if err != nil {
		return "", fmt.Errorf("resolve orchestration script path: %w", err)
	}

	imagePath := filepath.Join(b.outputDir, profile.Name+".qcow2")

	args := []string{
		"build",
		fmt.Sprintf("-var=vm_name=%s", profile.Name),
		fmt.Sprintf("-var=cloud_image_path=%s", cloudImagePath),
		fmt.Sprintf("-var=seed_iso_path=%s", seedISOPath),
		fmt.Sprintf("-var=ssh_private_key_file=%s", privKeyPath),
		fmt.Sprintf("-var=efi_firmware_vars=%s", efiVarsPath),
		fmt.Sprintf("-var=provision_script=%s", orchDestAbs),
		fmt.Sprintf("-var=output_dir=%s", b.outputDir),
	}
	args = append(args, qemuPackerVars(arch)...)
	args = append(args, "qemu.pkr.hcl")

	if err := b.runner.Run(ctx, b.vmDir, args); err != nil {
		return "", fmt.Errorf("packer build: %w", err)
	}

	if err := b.images.SetQEMUPath(profile.Name, imagePath); err != nil {
		return "", fmt.Errorf("store image reference: %w", err)
	}
	return imagePath, nil
}

func copyEFIVars() (string, error) {
	src := "/opt/homebrew/share/qemu/edk2-aarch64-vars.fd"
	srcF, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("open efi vars: %w", err)
	}
	defer srcF.Close()
	tmpF, err := os.CreateTemp("", "agentsdx-efi-vars-*")
	if err != nil {
		return "", fmt.Errorf("create efi vars temp: %w", err)
	}
	defer tmpF.Close()
	if _, err := io.Copy(tmpF, srcF); err != nil {
		os.Remove(tmpF.Name())
		return "", fmt.Errorf("copy efi vars: %w", err)
	}
	return tmpF.Name(), nil
}
```

`qemuPackerVars` is unchanged — it still passes `efi_firmware_code`.

- [ ] **Step 4: Run all builder tests**

```bash
cd server && go test ./internal/builder/ -v
```

Expected: all tests pass, including the three `TestBuildQEMU_*` tests and the two new ones.

- [ ] **Step 5: Confirm build**

```bash
cd server && go build ./...
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add server/internal/builder/builder.go server/internal/builder/builder_test.go
git commit -m "feat: rewrite BuildQEMU for cloud image, ephemeral key, and seed ISO"
```

---

### Task 3: Rewrite `vm/qemu.pkr.hcl`

**Files:**
- Modify: `vm/qemu.pkr.hcl`
- Delete: `vm/autoinstall/user-data.yaml`

- [ ] **Step 1: Rewrite `vm/qemu.pkr.hcl`**

Replace the entire file with:

```hcl
packer {
  required_plugins {
    qemu = {
      source  = "github.com/hashicorp/qemu"
      version = "~> 1"
    }
  }
}

variable "vm_name" {
  type        = string
  description = "VM name; used as the image filename base"
}

variable "cloud_image_path" {
  type        = string
  description = "Local absolute path to the cached Ubuntu cloud image"
}

variable "seed_iso_path" {
  type        = string
  description = "Local absolute path to the cloud-init seed ISO for Packer SSH access"
}

variable "ssh_private_key_file" {
  type        = string
  description = "Local absolute path to the ephemeral private key for Packer SSH"
}

variable "efi_firmware_vars" {
  type        = string
  default     = ""
  description = "Path to a writable EDK2 vars file (ARM64 only); empty string skips this drive"
}

variable "provision_script" {
  type        = string
  description = "Local absolute path to the generated orchestration shell script"
}

variable "output_dir" {
  type        = string
  description = "Directory where the built qcow2 image will be written"
}

variable "qemu_binary" {
  type    = string
  default = "qemu-system-aarch64"
}

variable "machine_type" {
  type    = string
  default = "virt"
}

variable "cpu_model" {
  type    = string
  default = "host"
}

variable "efi_firmware_code" {
  type        = string
  default     = "/opt/homebrew/share/qemu/edk2-aarch64-code.fd"
  description = "Path to EDK2 ARM64 firmware (read-only); empty string skips this drive"
}

locals {
  efi_code_args = var.efi_firmware_code != "" ? [
    ["-drive", "if=pflash,format=raw,readonly=on,file=${var.efi_firmware_code}"]
  ] : []
  efi_vars_args = var.efi_firmware_vars != "" ? [
    ["-drive", "if=pflash,format=raw,file=${var.efi_firmware_vars}"]
  ] : []
  seed_args    = [["-drive", "file=${var.seed_iso_path},media=cdrom,readonly=on"]]
  all_qemuargs = concat(local.efi_code_args, local.efi_vars_args, local.seed_args)
}

source "qemu" "vm" {
  vm_name              = var.vm_name
  iso_url              = var.cloud_image_path
  iso_checksum         = "none"
  disk_image           = true
  qemu_binary          = var.qemu_binary
  machine_type         = var.machine_type
  cpu_model            = var.cpu_model
  accelerator          = "hvf"
  disk_size            = "20480M"
  memory               = 2048
  cpus                 = 2
  headless             = true
  ssh_username         = "root"
  ssh_private_key_file = var.ssh_private_key_file
  ssh_timeout          = "10m"
  shutdown_command     = "shutdown -P now"

  qemuargs = local.all_qemuargs

  output_directory = var.output_dir
  format           = "qcow2"
}

build {
  sources = ["source.qemu.vm"]

  provisioner "file" {
    source      = "${path.root}/"
    destination = "/tmp/agentsdx-vm/"
  }

  provisioner "shell" {
    script = var.provision_script
  }
}
```

- [ ] **Step 2: Delete `vm/autoinstall/user-data.yaml`**

```bash
git rm vm/autoinstall/user-data.yaml
```

- [ ] **Step 3: Validate Packer can parse the new HCL**

```bash
cd vm && packer validate \
  -var=vm_name=test \
  -var=cloud_image_path=/tmp/fake.img \
  -var=seed_iso_path=/tmp/seed.iso \
  -var=ssh_private_key_file=/tmp/key \
  -var=provision_script=/tmp/orch.sh \
  -var=output_dir=/tmp/out \
  qemu.pkr.hcl
```

Expected output contains `The configuration is valid.` (file-not-found warnings for the fake paths are OK; a parse error is not).

- [ ] **Step 4: Commit**

```bash
git add vm/qemu.pkr.hcl
git commit -m "feat: switch Packer to cloud image source with ephemeral SSH and seed ISO"
```

---

### Task 4: Simplify `vm/base/provision.sh`

**Files:**
- Modify: `vm/base/provision.sh`

- [ ] **Step 1: Remove the SSH setup block from `base/provision.sh`**

Replace the entire file with:

```bash
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

echo "base provisioning complete"
```

The SSH setup (mkdir, chmod, authorized_keys, sshd_config edits, systemctl enable) is removed. The cloud-init seed ISO handles root SSH access during the Packer build, and the Ubuntu cloud image already has `openssh-server` running with `PermitRootLogin prohibit-password`.

- [ ] **Step 2: Commit**

```bash
git add vm/base/provision.sh
git commit -m "feat: remove SSH setup from base provisioner (cloud-init handles it)"
```
