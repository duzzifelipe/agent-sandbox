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
// It generates a temp orchestration script, invokes Packer, stores the image path in
// ImageStore, and cleans up the temp file.
func (b *Builder) BuildQEMU(ctx context.Context, profile types.ProfileSpec) (string, error) {
	arch := runtime.GOARCH // "arm64" or "amd64"
	registry := b.imageRegistry
	if registry == nil {
		registry = cloudImageRegistry
	}
	archImages, ok := registry[profile.Infrastructure.Image]
	if !ok {
		return "", fmt.Errorf("unknown base image %q", profile.Infrastructure.Image)
	}
	iso, ok := archImages[arch]
	if !ok {
		return "", fmt.Errorf("no ISO for %s/%s", profile.Infrastructure.Image, arch)
	}

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
		fmt.Sprintf("-var=iso_url=%s", iso.URL),
		fmt.Sprintf("-var=iso_checksum=%s", iso.Checksum),
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
