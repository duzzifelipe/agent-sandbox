//go:build e2e

package e2e_test

import (
	"encoding/json"
	"os"
	"testing"
)

func TestProfileLifecycle(t *testing.T) {
	specFile := writeProfileSpec(t, "e2e-lifecycle")
	t.Cleanup(func() { apiDELETE(t, "/profiles/e2e-lifecycle") })

	t.Run("create", func(t *testing.T) {
		stdout, stderr, code := runCLI("create", "--spec-file", specFile)
		assertExitCode(t, code, 0)
		assertContains(t, stdout+stderr, "e2e-lifecycle")
	})

	t.Run("list", func(t *testing.T) {
		stdout, _, code := runCLI("profiles")
		assertExitCode(t, code, 0)
		assertContains(t, stdout, "e2e-lifecycle")
	})

	t.Run("delete", func(t *testing.T) {
		status := apiDELETE(t, "/profiles/e2e-lifecycle")
		if status != 204 {
			t.Fatalf("DELETE /profiles/e2e-lifecycle: got %d, want 204", status)
		}
	})

	t.Run("list_after_delete", func(t *testing.T) {
		stdout, _, code := runCLI("profiles")
		assertExitCode(t, code, 0)
		assertNotContains(t, stdout, "e2e-lifecycle")
	})
}

func TestDuplicateProfileCreate(t *testing.T) {
	specFile := writeProfileSpec(t, "e2e-dup")
	t.Cleanup(func() { apiDELETE(t, "/profiles/e2e-dup") })

	_, _, code := runCLI("create", "--spec-file", specFile)
	assertExitCode(t, code, 0)

	_, _, code = runCLI("create", "--spec-file", specFile)
	if code == 0 {
		t.Error("expected non-zero exit when creating duplicate profile")
	}
}

// writeProfileSpec writes a minimal valid profile JSON to a temp file and returns its path.
func writeProfileSpec(t *testing.T, name string) string {
	t.Helper()
	spec := map[string]interface{}{
		"name": name,
		"infrastructure": map[string]interface{}{
			"provider": "virtualbox",
			"image":    "ubuntu-24.04",
			"tooling":  []string{},
		},
		"agent": map[string]interface{}{
			"provider": "claude",
			"skills":   []string{},
		},
		"projects": []interface{}{},
	}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	f, err := os.CreateTemp(t.TempDir(), "profile-*.json")
	if err != nil {
		t.Fatalf("create spec file: %v", err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatalf("write spec file: %v", err)
	}
	f.Close()
	return f.Name()
}
