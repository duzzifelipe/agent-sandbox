//go:build e2e

package e2e_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialsSet(t *testing.T) {
	specFile := writeProfileSpec(t, "e2e-creds")
	_, _, code := runCLI("create", "--spec-file", specFile)
	assertExitCode(t, code, 0)
	t.Cleanup(func() { apiDELETE(t, "/profiles/e2e-creds") })

	// Populate fake ~/.claude and ~/.claude.json in homeDir.
	claudeDir := filepath.Join(homeDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{"theme":"dark"}`), 0644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".claude.json"), []byte(`{"oauthToken":"test-token"}`), 0644); err != nil {
		t.Fatalf("write .claude.json: %v", err)
	}

	stdout, stderr, code := runCLI("credentials", "set", "e2e-creds")
	assertExitCode(t, code, 0)
	assertContains(t, stdout+stderr, "e2e-creds")
}
