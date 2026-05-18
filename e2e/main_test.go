//go:build e2e

package e2e_test

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

var (
	serverURL  string
	cliPath    string
	serverPath string
	tmpDir     string
	homeDir    string
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	var err error
	tmpDir, err = os.MkdirTemp("", "agentsdx-e2e-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "mktemp: %v\n", err)
		return 1
	}
	defer os.RemoveAll(tmpDir)

	homeDir = filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(homeDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir home: %v\n", err)
		return 1
	}

	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir bin: %v\n", err)
		return 1
	}

	serverPath = filepath.Join(binDir, "agentsdxd")
	cliPath = filepath.Join(binDir, "agentsdx")

	repoRoot, err := filepath.Abs("..")
	if err != nil {
		fmt.Fprintf(os.Stderr, "abs repo root: %v\n", err)
		return 1
	}

	for _, build := range []struct {
		out string
		dir string
		pkg string
	}{
		{serverPath, filepath.Join(repoRoot, "server"), "./cmd/agentsdxd"},
		{cliPath, filepath.Join(repoRoot, "cli"), "./cmd/agentsdx"},
	} {
		cmd := exec.Command("go", "build", "-o", build.out, build.pkg)
		cmd.Dir = build.dir
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "build %s: %v\n%s\n", build.pkg, err, out)
			return 1
		}
	}

	port, err := freePort()
	if err != nil {
		fmt.Fprintf(os.Stderr, "free port: %v\n", err)
		return 1
	}
	serverURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	dataDir := filepath.Join(tmpDir, "data")
	vmDir := filepath.Join(repoRoot, "vm")

	// On darwin/arm64, setup initialises the QEMU plugin (not vboxnet0).
	// On linux, setup configures vboxnet0 and the VirtualBox plugin.
	setup := exec.Command(serverPath, "setup", "--vm-dir="+vmDir)
	setup.Env = append(os.Environ(), "HOME="+homeDir)
	setup.Stdout = os.Stderr
	setup.Stderr = os.Stderr
	if err := setup.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "setup: %v\n", err)
		if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
			return 1
		}
		// On darwin/arm64, setup failure (e.g. packer not installed) is non-fatal
		// for tests that don't exercise VM image building.
		fmt.Fprintf(os.Stderr, "warning: setup failed on darwin/arm64, continuing\n")
	}

	// Keep a stable Packer ISO cache across test runs so large ISOs are not
	// re-downloaded every time.
	isoCache := filepath.Join(os.TempDir(), "agentsdx-e2e-packer-cache")
	_ = os.MkdirAll(isoCache, 0755)

	srv := exec.Command(serverPath, "serve")
	srv.Env = append(os.Environ(),
		"AGENTSDX_VAULT_SECRET=e2e-test-secret-do-not-use-in-prod",
		"AGENTSDX_SERVER_URL="+serverURL,
		fmt.Sprintf("AGENTSDX_ADDR=:%d", port),
		"AGENTSDX_DATA_DIR="+dataDir,
		"AGENTSDX_VM_DIR="+vmDir,
		"HOME="+homeDir,
		"PACKER_CACHE_DIR="+isoCache,
	)
	srv.Stdout = os.Stderr
	srv.Stderr = os.Stderr
	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start server: %v\n", err)
		return 1
	}
	defer func() {
		srv.Process.Kill()
		srv.Wait()
	}()

	if err := waitReady(serverURL+"/profiles", 10*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "server not ready: %v\n", err)
		return 1
	}

	return m.Run()
}

// freePort picks an available TCP port. There is a small TOCTOU window between
// closing the listener here and the server binding the port — acceptable for local use.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

func waitReady(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out after %s waiting for %s", timeout, url)
}
