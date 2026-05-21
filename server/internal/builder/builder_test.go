package builder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

	scriptPath, err := writeOrchestrationScript(scripts, "claude")
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
	if !strings.Contains(content, "/usr/local/bin/entrypoint.sh") {
		t.Errorf("expected script to copy entrypoint.sh, content: %q", content)
	}
	if !strings.Contains(content, "/usr/local/bin/vault-sync.sh") {
		t.Errorf("expected script to copy vault-sync.sh, content: %q", content)
	}
	if !strings.Contains(content, "agents/claude/entrypoint.sh") {
		t.Errorf("expected script to reference claude entrypoint, content: %q", content)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

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
		copyEFIVarsFn: func() (string, error) { return "", nil },
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
