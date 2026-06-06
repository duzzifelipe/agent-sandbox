package opencodecreds

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// VaultKey is the reserved secret name used to store the OpenCode config.
// The AGENTSDX_ prefix marks it as a system-managed key.
const VaultKey = "AGENTSDX_OPENCODE_CONFIG"

// ConfigPath returns the default OpenCode config file path for the current user.
func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	// OpenCode follows XDG: ~/.config/opencode/config.json
	return filepath.Join(home, ".config", "opencode", "config.json"), nil
}

// Extract reads the OpenCode config from the local machine (~/.config/opencode/config.json).
func Extract() (string, error) {
	path, err := ConfigPath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}
