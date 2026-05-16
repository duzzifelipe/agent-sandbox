//go:build e2e && vm

package e2e_test

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBuildImage(t *testing.T) {
	specFile := writeProfileSpec(t, "e2e-image")
	_, _, code := runCLI("create", "--spec-file", specFile)
	assertExitCode(t, code, 0)
	t.Cleanup(func() { apiDELETE(t, "/profiles/e2e-image") })

	stdout, stderr, code := runCLI("images", "build", "e2e-image")
	assertExitCode(t, code, 0)
	assertContains(t, stdout+stderr, "build started")

	waitForImage(t, "e2e-image", 20*time.Minute)
}

// waitForImage polls GET /images until the named profile has a non-empty virtualbox path.
// Defined here (e2e && vm) because sessions_test.go (e2e && vm) depends on it.
// Do not move to helpers_test.go (e2e only) — it would be excluded from the vm build.
func waitForImage(t *testing.T, profile string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		body, status := apiGET(t, "/images")
		if status != 200 {
			t.Fatalf("GET /images: got %d\n%s", status, body)
		}
		var entries []map[string]interface{}
		if err := json.Unmarshal(body, &entries); err != nil {
			t.Fatalf("unmarshal images: %v\nbody: %s", err, body)
		}
		for _, e := range entries {
			if e["profile_name"] == profile && e["virtualbox"] != "" {
				return
			}
		}
		time.Sleep(30 * time.Second)
	}
	t.Fatalf("image for profile %q not ready after %s", profile, timeout)
}
