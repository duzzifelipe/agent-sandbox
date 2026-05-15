package builder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/duck-labs/agentsdx-shared/types"
)

// fakeRunner captures the packer args and reads the orchestration script content.
type fakeRunner struct {
	vmDir        string // set by the test
	capturedArgs []string
	orchContent  string
	err          error
	readErr      error
}

func (f *fakeRunner) Run(_ context.Context, _ string, args []string) error {
	f.capturedArgs = args
	data, err := os.ReadFile(filepath.Join(f.vmDir, "orchestrate.sh"))
	f.readErr = err
	f.orchContent = string(data)
	return f.err
}

func TestComposeScripts_BaseOnly(t *testing.T) {
	profile := types.ProfileSpec{
		Name: "test",
		Infrastructure: types.InfrastructureConfig{
			Tooling: nil,
		},
		Agent: types.AgentConfig{
			Provider: "claude",
		},
	}

	scripts := composeScripts(profile)

	expected := []string{
		"/tmp/agentsdx-vm/base/provision.sh",
		"/tmp/agentsdx-vm/agents/claude/provision.sh",
	}

	if len(scripts) != len(expected) {
		t.Fatalf("expected %d scripts, got %d: %v", len(expected), len(scripts), scripts)
	}
	for i, s := range scripts {
		if s != expected[i] {
			t.Errorf("script[%d]: expected %q, got %q", i, expected[i], s)
		}
	}
}

func TestComposeScripts_WithTooling(t *testing.T) {
	profile := types.ProfileSpec{
		Name: "test",
		Infrastructure: types.InfrastructureConfig{
			Tooling: []string{"mise", "docker"},
		},
		Agent: types.AgentConfig{
			Provider: "claude",
		},
	}

	scripts := composeScripts(profile)

	expected := []string{
		"/tmp/agentsdx-vm/base/provision.sh",
		"/tmp/agentsdx-vm/tooling/mise/provision.sh",
		"/tmp/agentsdx-vm/tooling/docker/provision.sh",
		"/tmp/agentsdx-vm/agents/claude/provision.sh",
	}

	if len(scripts) != len(expected) {
		t.Fatalf("expected %d scripts, got %d: %v", len(expected), len(scripts), scripts)
	}
	for i, s := range scripts {
		if s != expected[i] {
			t.Errorf("script[%d]: expected %q, got %q", i, expected[i], s)
		}
	}
}

func TestWriteOrchestrationScript_ContainsBashCalls(t *testing.T) {
	scripts := []string{"/path/one", "/path/two"}

	scriptPath, err := writeOrchestrationScript(scripts)
	if err != nil {
		t.Fatalf("writeOrchestrationScript: %v", err)
	}
	defer os.Remove(scriptPath)

	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	content := string(data)

	if !strings.HasPrefix(content, "#!/bin/bash") {
		t.Errorf("expected script to start with #!/bin/bash, got: %q", content[:min(30, len(content))])
	}
	if !strings.Contains(content, `bash "/path/one"`) {
		t.Errorf("expected script to contain bash call for /path/one, content: %q", content)
	}
	if !strings.Contains(content, `bash "/path/two"`) {
		t.Errorf("expected script to contain bash call for /path/two, content: %q", content)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestBuildVirtualBox_PassesCorrectArgs(t *testing.T) {
	vmDir := t.TempDir()
	outputDir := t.TempDir()
	imagesPath := filepath.Join(t.TempDir(), "images.json")
	imageStore := vm.NewImageStore(imagesPath)

	fake := &fakeRunner{vmDir: vmDir}
	b := &Builder{
		vmDir:     vmDir,
		outputDir: outputDir,
		images:    imageStore,
		runner:    fake,
	}

	profile := types.ProfileSpec{
		Name: "test-profile",
		Infrastructure: types.InfrastructureConfig{
			Image:   "ubuntu-24.04",
			Tooling: []string{},
		},
		Agent: types.AgentConfig{
			Provider: "claude",
		},
	}

	_, err := b.BuildVirtualBox(context.Background(), profile)
	if err != nil {
		t.Fatalf("BuildVirtualBox: %v", err)
	}

	if fake.readErr != nil {
		t.Fatalf("fakeRunner failed to read orchestrate.sh: %v", fake.readErr)
	}

	argsStr := strings.Join(fake.capturedArgs, " ")

	if !strings.Contains(argsStr, "-var=iso_url=https://releases.ubuntu.com/24.04.2/ubuntu-24.04.2-live-server-amd64.iso") {
		t.Errorf("expected iso_url arg, got args: %v", fake.capturedArgs)
	}
	if !strings.Contains(argsStr, "-var=vm_name=test-profile") {
		t.Errorf("expected vm_name arg, got args: %v", fake.capturedArgs)
	}
	if !strings.Contains(argsStr, "-var=provision_script=/tmp/agentsdx-vm/orchestrate.sh") {
		t.Errorf("expected provision_script arg, got args: %v", fake.capturedArgs)
	}
}

func TestBuildVirtualBox_PackerFailure_ReturnsError(t *testing.T) {
	vmDir := t.TempDir()
	outputDir := t.TempDir()
	imagesPath := filepath.Join(t.TempDir(), "images.json")
	imageStore := vm.NewImageStore(imagesPath)

	fake := &fakeRunner{
		vmDir: vmDir,
		err:   errors.New("packer failed"),
	}
	b := &Builder{
		vmDir:     vmDir,
		outputDir: outputDir,
		images:    imageStore,
		runner:    fake,
	}

	profile := types.ProfileSpec{
		Name: "failure-profile",
		Infrastructure: types.InfrastructureConfig{
			Image:   "ubuntu-24.04",
			Tooling: []string{},
		},
		Agent: types.AgentConfig{
			Provider: "claude",
		},
	}

	_, err := b.BuildVirtualBox(context.Background(), profile)
	if err == nil {
		t.Fatal("expected error from packer failure, got nil")
	}
	if !strings.Contains(err.Error(), "packer build") {
		t.Errorf("expected 'packer build' in error message, got: %v", err)
	}
}

func TestBuildVirtualBox_UnknownImage_ReturnsError(t *testing.T) {
	vmDir := t.TempDir()
	outputDir := t.TempDir()
	imagesPath := filepath.Join(t.TempDir(), "images.json")
	imageStore := vm.NewImageStore(imagesPath)

	fake := &fakeRunner{vmDir: vmDir}
	b := &Builder{
		vmDir:     vmDir,
		outputDir: outputDir,
		images:    imageStore,
		runner:    fake,
	}

	profile := types.ProfileSpec{
		Name: "test-profile",
		Infrastructure: types.InfrastructureConfig{
			Image: "unknown-os-9.9",
		},
		Agent: types.AgentConfig{
			Provider: "claude",
		},
	}

	_, err := b.BuildVirtualBox(context.Background(), profile)
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
