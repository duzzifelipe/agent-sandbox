package builder

import (
	"context"
	"fmt"
	"log"
	"os"
	"text/template"

	"github.com/duck-labs/agentsdx-server/internal/vault"
	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/duck-labs/agentsdx-shared/types"
)

// Builder orchestrates image builds across multiple providers.
type Builder struct {
	vmDir     string
	images    *vm.ImageStore
	providers map[string]vm.ImageProvider // key: provider name ("hetzner", "local")
	// provision is injectable for testing; defaults to sshProvision.
	provision func(ctx context.Context, addr, privKey, vmDir, orchScriptPath string) error
}

// New creates a Builder.
func New(vmDir string, images *vm.ImageStore, providers map[string]vm.ImageProvider) *Builder {
	b := &Builder{vmDir: vmDir, images: images, providers: providers}
	b.provision = b.sshProvision
	return b
}

// Build provisions a snapshot for the given profile and returns the snapshot ID.
func (b *Builder) Build(ctx context.Context, profile types.ProfileSpec) (string, error) {
	provider, ok := b.providers[profile.Infrastructure.Provider]
	if !ok {
		return "", fmt.Errorf("provider %q not configured — check server credentials", profile.Infrastructure.Provider)
	}

	privKey, pubKey, err := vault.GenerateKeyPair()
	if err != nil {
		return "", fmt.Errorf("generate key pair: %w", err)
	}

	buildVM, err := provider.CreateBuildVM(ctx, profile.Infrastructure.Image, pubKey)
	if err != nil {
		return "", fmt.Errorf("create build vm: %w", err)
	}
	defer func() { _ = provider.DestroyBuildVM(ctx, buildVM.ID) }()

	scripts := composeScripts(profile)
	orchScript, err := writeOrchestrationScript(scripts, profile.Agent.Provider)
	if err != nil {
		return "", fmt.Errorf("write orchestration script: %w", err)
	}
	defer os.Remove(orchScript)

	var sshAddr string
	if buildVM.SSHPort != 0 {
		sshAddr = fmt.Sprintf("127.0.0.1:%d", buildVM.SSHPort)
	} else {
		sshAddr = buildVM.IPAddress + ":22"
	}

	log.Printf("provisioning profile %s on %s", profile.Name, sshAddr)
	if err := b.provision(ctx, sshAddr, privKey, b.vmDir, orchScript); err != nil {
		return "", fmt.Errorf("provision: %w", err)
	}

	snapshotID, err := provider.SnapshotVM(ctx, buildVM.ID, profile.Name)
	if err != nil {
		return "", fmt.Errorf("snapshot vm: %w", err)
	}

	if err := b.images.SetImageID(vm.Provider(profile.Infrastructure.Provider), profile.Name, snapshotID); err != nil {
		return "", fmt.Errorf("store snapshot id: %w", err)
	}

	log.Printf("build complete for profile %s: snapshot %s", profile.Name, snapshotID)
	return snapshotID, nil
}

func (b *Builder) sshProvision(ctx context.Context, addr, privKey, vmDir, orchScriptPath string) error {
	conn, err := dialSSHWithRetry(ctx, addr, privKey)
	if err != nil {
		return fmt.Errorf("dial ssh: %w", err)
	}
	defer conn.Close()
	if err := uploadDir(conn, vmDir); err != nil {
		return fmt.Errorf("upload vm dir: %w", err)
	}
	if err := uploadFile(conn, orchScriptPath, "/tmp/agentsdx-orchestrate.sh"); err != nil {
		return fmt.Errorf("upload orchestration script: %w", err)
	}
	return runRemoteCommand(conn, "/tmp/agentsdx-orchestrate.sh")
}

// composeScripts returns the ordered list of in-VM provisioning script paths for a profile.
func composeScripts(profile types.ProfileSpec) []string {
	scripts := []string{"/tmp/agentsdx-vm/base/provision.sh"}
	for _, tool := range profile.Infrastructure.Tooling {
		scripts = append(scripts, fmt.Sprintf("/tmp/agentsdx-vm/tooling/%s/provision.sh", tool))
	}
	scripts = append(scripts, fmt.Sprintf("/tmp/agentsdx-vm/agents/%s/provision.sh", profile.Agent.Provider))
	return scripts
}

const orchestrationTpl = `#!/bin/bash
set -euo pipefail
{{range .Scripts}}
bash "{{.}}"
{{end}}
cp "/tmp/agentsdx-vm/agents/{{.Agent}}/entrypoint.sh" /usr/local/bin/entrypoint.sh
sync
`

// writeOrchestrationScript writes a temp bash script that runs provision scripts in order.
// Caller must delete the returned file path.
func writeOrchestrationScript(scripts []string, agentProvider string) (string, error) {
	f, err := os.CreateTemp("", "agentsdx-orchestrate-*.sh")
	if err != nil {
		return "", fmt.Errorf("create temp script: %w", err)
	}

	tmpl, err := template.New("orch").Parse(orchestrationTpl)
	if err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("parse template: %w", err)
	}
	data := struct {
		Scripts []string
		Agent   string
	}{Scripts: scripts, Agent: agentProvider}
	if err := tmpl.Execute(f, data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("execute template: %w", err)
	}
	name := f.Name()
	f.Close()
	if err := os.Chmod(name, 0o755); err != nil {
		os.Remove(name)
		return "", fmt.Errorf("chmod script: %w", err)
	}
	return name, nil
}
