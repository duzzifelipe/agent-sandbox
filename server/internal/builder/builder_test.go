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

// fakeImageProvider is a test double for vm.ImageProvider.
type fakeImageProvider struct {
	buildVM      *vm.VM
	buildErr     error
	snapshotID   string
	snapshotErr  error
	destroyErr   error
	destroyedIDs []string
}

func (f *fakeImageProvider) CreateBuildVM(_ context.Context, _, _ string) (*vm.VM, error) {
	if f.buildErr != nil {
		return nil, f.buildErr
	}
	return f.buildVM, nil
}

func (f *fakeImageProvider) SnapshotVM(_ context.Context, vmID, _ string) (string, error) {
	if f.snapshotErr != nil {
		return "", f.snapshotErr
	}
	return f.snapshotID, nil
}

func (f *fakeImageProvider) DestroyBuildVM(_ context.Context, vmID string) error {
	f.destroyedIDs = append(f.destroyedIDs, vmID)
	return f.destroyErr
}

func testBuilder(t *testing.T, provider *fakeImageProvider) *Builder {
	t.Helper()
	vmDir := t.TempDir()
	imagesPath := filepath.Join(t.TempDir(), "images.json")
	providers := map[string]vm.ImageProvider{"hetzner": provider}
	b := New(vmDir, vm.NewImageStore(imagesPath), providers)
	b.provision = func(_ context.Context, _, _, _, _ string) error { return nil }
	return b
}

func TestBuild_StoresSnapshotID(t *testing.T) {
	provider := &fakeImageProvider{
		buildVM:    &vm.VM{ID: "build-1", IPAddress: "1.2.3.4", State: vm.VMStateRunning},
		snapshotID: "snap-42",
	}
	b := testBuilder(t, provider)

	profile := types.ProfileSpec{
		Name:           "my-profile",
		Infrastructure: types.InfrastructureConfig{Image: "ubuntu-24.04", Provider: "hetzner"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}

	got, err := b.Build(context.Background(), profile)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got != "snap-42" {
		t.Errorf("returned snapshotID: got %q, want %q", got, "snap-42")
	}

	stored, err := b.images.GetImageID(vm.ProviderHetzner, "my-profile")
	if err != nil {
		t.Fatalf("GetImageID: %v", err)
	}
	if stored != "snap-42" {
		t.Errorf("stored snapshotID: got %q, want %q", stored, "snap-42")
	}
}

func TestBuild_DestroysBuildVMOnSuccess(t *testing.T) {
	provider := &fakeImageProvider{
		buildVM:    &vm.VM{ID: "build-1", IPAddress: "1.2.3.4", State: vm.VMStateRunning},
		snapshotID: "snap-42",
	}
	b := testBuilder(t, provider)

	profile := types.ProfileSpec{
		Name:           "my-profile",
		Infrastructure: types.InfrastructureConfig{Image: "ubuntu-24.04", Provider: "hetzner"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}

	if _, err := b.Build(context.Background(), profile); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(provider.destroyedIDs) != 1 || provider.destroyedIDs[0] != "build-1" {
		t.Errorf("expected DestroyBuildVM(build-1), got: %v", provider.destroyedIDs)
	}
}

func TestBuild_CreateBuildVMFailure_ReturnsError(t *testing.T) {
	provider := &fakeImageProvider{buildErr: errors.New("quota exceeded")}
	b := testBuilder(t, provider)

	profile := types.ProfileSpec{
		Name:           "my-profile",
		Infrastructure: types.InfrastructureConfig{Image: "ubuntu-24.04", Provider: "hetzner"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}

	_, err := b.Build(context.Background(), profile)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "create build vm") {
		t.Errorf("expected 'create build vm' in error, got: %v", err)
	}
}

func TestBuild_ProvisionFailure_DestroysBuildVM(t *testing.T) {
	provider := &fakeImageProvider{
		buildVM: &vm.VM{ID: "build-1", IPAddress: "1.2.3.4", State: vm.VMStateRunning},
	}
	imagesPath := filepath.Join(t.TempDir(), "images.json")
	providers := map[string]vm.ImageProvider{"hetzner": provider}
	b := New(t.TempDir(), vm.NewImageStore(imagesPath), providers)
	b.provision = func(_ context.Context, _, _, _, _ string) error {
		return errors.New("ssh failed")
	}

	profile := types.ProfileSpec{
		Name:           "my-profile",
		Infrastructure: types.InfrastructureConfig{Image: "ubuntu-24.04", Provider: "hetzner"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}

	_, err := b.Build(context.Background(), profile)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(provider.destroyedIDs) != 1 {
		t.Errorf("expected DestroyBuildVM called once, got: %v", provider.destroyedIDs)
	}
}

func TestBuild_ProviderNotConfigured_ReturnsError(t *testing.T) {
	provider := &fakeImageProvider{
		buildVM:    &vm.VM{ID: "build-1", IPAddress: "1.2.3.4", State: vm.VMStateRunning},
		snapshotID: "snap-42",
	}
	b := testBuilder(t, provider)

	profile := types.ProfileSpec{
		Name:           "my-profile",
		Infrastructure: types.InfrastructureConfig{Image: "ubuntu-24.04", Provider: "unknown"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}

	_, err := b.Build(context.Background(), profile)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("expected 'not configured' in error, got: %v", err)
	}
}

func TestBuild_SSHAddr_UsesSSHPortWhenSet(t *testing.T) {
	provider := &fakeImageProvider{
		buildVM:    &vm.VM{ID: "build-1", IPAddress: "1.2.3.4", SSHPort: 12345, State: vm.VMStateRunning},
		snapshotID: "snap-42",
	}
	imagesPath := filepath.Join(t.TempDir(), "images.json")
	providers := map[string]vm.ImageProvider{"hetzner": provider}
	b := New(t.TempDir(), vm.NewImageStore(imagesPath), providers)

	var capturedAddr string
	b.provision = func(_ context.Context, addr, _, _, _ string) error {
		capturedAddr = addr
		return nil
	}

	profile := types.ProfileSpec{
		Name:           "my-profile",
		Infrastructure: types.InfrastructureConfig{Image: "ubuntu-24.04", Provider: "hetzner"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}

	if _, err := b.Build(context.Background(), profile); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if capturedAddr != "127.0.0.1:12345" {
		t.Errorf("SSH addr: got %q, want %q", capturedAddr, "127.0.0.1:12345")
	}
}

func TestBuild_SSHAddr_FallsBackToIPWhenNoSSHPort(t *testing.T) {
	provider := &fakeImageProvider{
		buildVM:    &vm.VM{ID: "build-1", IPAddress: "10.0.0.1", SSHPort: 0, State: vm.VMStateRunning},
		snapshotID: "snap-1",
	}
	b := testBuilder(t, provider)

	var capturedAddr string
	b.provision = func(_ context.Context, addr, _, _, _ string) error {
		capturedAddr = addr
		return nil
	}

	profile := types.ProfileSpec{
		Name:           "my-profile",
		Infrastructure: types.InfrastructureConfig{Image: "ubuntu-24.04", Provider: "hetzner"},
		Agent:          types.AgentConfig{Provider: "claude"},
	}

	if _, err := b.Build(context.Background(), profile); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if capturedAddr != "10.0.0.1:22" {
		t.Errorf("SSH addr: got %q, want %q", capturedAddr, "10.0.0.1:22")
	}
}

func TestComposeScripts_BaseOnly(t *testing.T) {
	profile := types.ProfileSpec{
		Infrastructure: types.InfrastructureConfig{Tooling: nil},
		Agent:          types.AgentConfig{Provider: "claude"},
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
		Infrastructure: types.InfrastructureConfig{Tooling: []string{"mise", "docker"}},
		Agent:          types.AgentConfig{Provider: "claude"},
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
	scriptPath, err := writeOrchestrationScript([]string{"/path/one", "/path/two"}, "claude")
	if err != nil {
		t.Fatalf("writeOrchestrationScript: %v", err)
	}
	defer os.Remove(scriptPath)

	data, _ := os.ReadFile(scriptPath)
	content := string(data)

	for _, want := range []string{
		"#!/bin/bash",
		`bash "/path/one"`,
		`bash "/path/two"`,
		"/usr/local/bin/entrypoint.sh",
		"/usr/local/bin/vault-sync.sh",
		"agents/claude/entrypoint.sh",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("script missing %q", want)
		}
	}
}
