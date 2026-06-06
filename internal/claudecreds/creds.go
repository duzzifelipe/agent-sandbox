package claudecreds

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// VaultKey is the reserved secret name used to store Claude Code credentials.
// The AGENTSDX_ prefix marks it as a system-managed key.
const VaultKey = "AGENTSDX_CLAUDE_CREDENTIALS"

const keychainService = "Claude Code-credentials"

// Extract reads Claude Code credentials from the local machine.
// On macOS it reads from the system Keychain; on Linux from ~/.claude/.credentials.json.
func Extract() (string, error) {
	if runtime.GOOS == "darwin" {
		return extractMacOS()
	}
	return extractLinux()
}

func extractMacOS() (string, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", keychainService, "-w").Output()
	if err != nil {
		return "", fmt.Errorf("keychain lookup for %q: %w", keychainService, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func extractLinux() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	path := filepath.Join(home, ".claude", ".credentials.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}
