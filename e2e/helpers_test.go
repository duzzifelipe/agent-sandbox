//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// runCLI runs the agentsdx binary with args and the test server URL injected.
// HOME is set to homeDir so state files and credentials go to the temp dir.
func runCLI(args ...string) (stdout, stderr string, exitCode int) {
	cmd := exec.Command(cliPath, args...)
	cmd.Env = append(os.Environ(),
		"AGENTSDX_URL="+serverURL,
		"HOME="+homeDir,
	)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if ex, ok := err.(*exec.ExitError); ok {
			exitCode = ex.ExitCode()
		} else {
			exitCode = 1
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

func assertExitCode(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("exit code: got %d, want %d", got, want)
	}
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected output to contain %q\nactual: %q", substr, s)
	}
}

func assertNotContains(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Errorf("expected output NOT to contain %q\nactual: %q", substr, s)
	}
}

func assertJSONField(t *testing.T, body []byte, key, want string) {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("unmarshal JSON: %v\nbody: %s", err, body)
	}
	got := fmt.Sprintf("%v", m[key])
	if got != want {
		t.Errorf("JSON field %q: got %q, want %q", key, got, want)
	}
}

func apiGET(t *testing.T, path string) ([]byte, int) {
	t.Helper()
	resp, err := http.Get(serverURL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode
}

func apiPOST(t *testing.T, path string, body []byte) ([]byte, int) {
	t.Helper()
	var r io.Reader
	ct := "application/json"
	if body != nil {
		r = bytes.NewReader(body)
	}
	resp, err := http.Post(serverURL+path, ct, r)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode
}

func apiDELETE(t *testing.T, path string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodDelete, serverURL+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}
