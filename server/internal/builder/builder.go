package builder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	vmDir     string // host path to vm/ directory
	outputDir string // host path where OVA files are written
	images    *vm.ImageStore
	runner    PackerRunner
}

func New(vmDir, outputDir string, images *vm.ImageStore) *Builder {
	return &Builder{
		vmDir:     vmDir,
		outputDir: outputDir,
		images:    images,
		runner:    &realPackerRunner{},
	}
}

// isoRegistry maps base OS names to ISO URL + checksum, keyed by GOARCH.
var isoRegistry = map[string]map[string]struct {
	URL      string
	Checksum string
}{
	"ubuntu-24.04": {
		"amd64": {
			URL:      "https://releases.ubuntu.com/24.04.2/ubuntu-24.04.2-live-server-amd64.iso",
			Checksum: "sha256:d6fea3a0b8f5a53455e7fc0b2bfeadb36e72b2432f31b0b93d7e09f07f695a42",
		},
		"arm64": {
			URL:      "https://cdimage.ubuntu.com/releases/24.04.2/release/ubuntu-24.04.2-live-server-arm64.iso",
			Checksum: "sha256:3c69d7f0f0b44fc82d0ec6f85694e8b7c11db11aaba1060e56f61d4bc3cbdb1b",
		},
	},
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
	archISOs, ok := isoRegistry[profile.Infrastructure.Image]
	if !ok {
		return "", fmt.Errorf("unknown base image %q", profile.Infrastructure.Image)
	}
	iso, ok := archISOs[arch]
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

	imagePath := filepath.Join(b.outputDir, profile.Name+".qcow2")

	args := []string{
		"build",
		fmt.Sprintf("-var=vm_name=%s", profile.Name),
		fmt.Sprintf("-var=iso_url=%s", iso.URL),
		fmt.Sprintf("-var=iso_checksum=%s", iso.Checksum),
		"-var=provision_script=/tmp/agentsdx-vm/orchestrate.sh",
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
