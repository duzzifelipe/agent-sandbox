package builder

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

// PackerRunner abstracts packer execution for testing.
type PackerRunner interface {
	Run(ctx context.Context, workDir string, args []string) error
}

type realPackerRunner struct{}

func (r *realPackerRunner) Run(ctx context.Context, workDir string, args []string) error {
	cmd := exec.CommandContext(ctx, "packer", args...)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Builder orchestrates Packer image builds.
type Builder struct {
	vmDir          string
	outputDir      string
	isoDir         string
	images         *vm.ImageStore
	runner         PackerRunner
	imageRegistry  map[string]map[string]cloudImageEntry // nil = use cloudImageRegistry
	copyEFIVarsFn  func() (string, error)                // nil = use copyEFIVars
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
	_, copyErr := io.Copy(f, resp.Body)
	f.Close()
	if copyErr != nil {
		os.Remove(destPath)
		return copyErr
	}
	return nil
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
    content: "%s"
`, strings.TrimSpace(publicKey))
}

// composeScripts returns the ordered list of in-VM provisioning script paths
// for a given profile. Paths are relative to /tmp/agentsdx-vm/ inside the VM.
func composeScripts(profile types.ProfileSpec) []string {
	scripts := []string{"/tmp/agentsdx-vm/base/provision.sh"}
	for _, tool := range profile.Infrastructure.Tooling {
		scripts = append(scripts, fmt.Sprintf("/tmp/agentsdx-vm/tooling/%s/provision.sh", tool))
	}
	scripts = append(scripts, fmt.Sprintf("/tmp/agentsdx-vm/agents/%s/provision.sh", profile.Agent.Provider))
	return scripts
}

// orchestrationTpl is the template for the temp orchestration script.
const orchestrationTpl = `#!/bin/bash
set -euo pipefail
{{range .Scripts}}
bash "{{.}}"
{{end}}
cp "/tmp/agentsdx-vm/agents/{{.Agent}}/entrypoint.sh" /usr/local/bin/entrypoint.sh
cp /tmp/agentsdx-vm/vault-sync.sh /usr/local/bin/vault-sync.sh
chmod +x /usr/local/bin/entrypoint.sh /usr/local/bin/vault-sync.sh
`

// writeOrchestrationScript writes a temp bash script that runs all provision
// scripts in order and copies the agent entrypoint and vault-sync into the image.
// Returns the script path; caller must delete it.
func writeOrchestrationScript(scripts []string, agentProvider string) (string, error) {
	f, err := os.CreateTemp("", "agentsdx-orchestrate-*.sh")
	if err != nil {
		return "", fmt.Errorf("create temp script: %w", err)
	}
	defer f.Close()

	tmpl, err := template.New("orch").Parse(orchestrationTpl)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	data := struct {
		Scripts []string
		Agent   string
	}{Scripts: scripts, Agent: agentProvider}
	if err := tmpl.Execute(f, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	if err := os.Chmod(f.Name(), 0o755); err != nil {
		return "", fmt.Errorf("chmod script: %w", err)
	}
	return f.Name(), nil
}

// BuildQEMU builds a QEMU qcow2 image for the given profile using the host architecture.
// It downloads/caches the cloud image, generates an ephemeral SSH key pair, writes a
// cloud-init seed ISO, generates a temp orchestration script, invokes Packer, stores the
// image path in ImageStore, and cleans up temp files.
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
	if err := privKeyFile.Close(); err != nil {
		return "", fmt.Errorf("close packer key file: %w", err)
	}
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
		copyFn := b.copyEFIVarsFn
		if copyFn == nil {
			copyFn = copyEFIVars
		}
		efiVarsPath, err = copyFn()
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
	if _, err := io.Copy(tmpF, srcF); err != nil {
		tmpF.Close()
		os.Remove(tmpF.Name())
		return "", fmt.Errorf("copy efi vars: %w", err)
	}
	name := tmpF.Name()
	if err := tmpF.Close(); err != nil {
		os.Remove(name)
		return "", fmt.Errorf("close efi vars temp: %w", err)
	}
	return name, nil
}

// qemuPackerVars returns architecture-specific Packer variable overrides.
func qemuPackerVars(arch string) []string {
	if arch == "arm64" {
		return []string{
			"-var=qemu_binary=qemu-system-aarch64",
			"-var=machine_type=virt",
			"-var=cpu_model=host",
			"-var=efi_firmware_code=/opt/homebrew/share/qemu/edk2-aarch64-code.fd",
		}
	}
	return []string{
		"-var=qemu_binary=qemu-system-x86_64",
		"-var=machine_type=q35",
		"-var=cpu_model=host",
		"-var=efi_firmware_code=",
	}
}
