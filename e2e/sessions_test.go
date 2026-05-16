//go:build e2e && vm

package e2e_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionLifecycle(t *testing.T) {
	// Create profile.
	specFile := writeProfileSpec(t, "e2e-session")
	_, _, code := runCLI("create", "--spec-file", specFile)
	assertExitCode(t, code, 0)
	t.Cleanup(func() { apiDELETE(t, "/profiles/e2e-session") })

	// Upload credentials (needed for vault bootstrap).
	claudeDir := filepath.Join(homeDir, ".claude")
	os.MkdirAll(claudeDir, 0755)
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{}`), 0644)
	os.WriteFile(filepath.Join(homeDir, ".claude.json"), []byte(`{"oauthToken":"test"}`), 0644)
	_, _, code = runCLI("credentials", "set", "e2e-session")
	assertExitCode(t, code, 0)

	// Build image and wait.
	_, _, code = runCLI("images", "build", "e2e-session")
	assertExitCode(t, code, 0)
	waitForImage(t, "e2e-session", 20*time.Minute)

	// Run session without SSH connect; waits until VM is running then exits.
	stdout, stderr, code := runCLI("run", "--no-connect", "e2e-session")
	assertExitCode(t, code, 0)
	assertContains(t, stdout+stderr, "running")

	// Stop session.
	stdout, stderr, code = runCLI("stop", "e2e-session")
	assertExitCode(t, code, 0)
	assertContains(t, stdout+stderr, "stopped")
}
