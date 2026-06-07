package opencodecreds

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// VaultKey is the reserved secret name used to store the OpenCode config
// (~/.config/opencode/opencode.json).
const VaultKey = "AGENTSDX_OPENCODE_CONFIG"

// VaultKeyAuth is the reserved secret name used to store the OpenCode auth
// credentials (~/.local/share/opencode/auth.json) which contain API keys.
const VaultKeyAuth = "AGENTSDX_OPENCODE_AUTH"

// VaultKeyAccount is the reserved secret name used to store the OpenCode
// account file (~/.local/share/opencode/account.json).
const VaultKeyAccount = "AGENTSDX_OPENCODE_ACCOUNT"

// ConfigPath returns the default OpenCode config file path for the current user.
// OpenCode follows XDG_CONFIG_HOME: ~/.config/opencode/opencode.json (macOS and Linux).
func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".config", "opencode", "opencode.json"), nil
}

// AuthPath returns the default OpenCode auth file path for the current user.
// OpenCode follows XDG_DATA_HOME: ~/.local/share/opencode/auth.json (macOS and Linux).
func AuthPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "opencode", "auth.json"), nil
}

// AccountPath returns the default OpenCode account file path for the current user.
// OpenCode follows XDG_DATA_HOME: ~/.local/share/opencode/account.json (macOS and Linux).
func AccountPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "opencode", "account.json"), nil
}

// Extract reads the OpenCode config from the local machine (~/.config/opencode/opencode.json).
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

// ExtractAuth reads the OpenCode auth credentials from the local machine
// (~/.local/share/opencode/auth.json) containing provider API keys.
func ExtractAuth() (string, error) {
	path, err := AuthPath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// ExtractAccount reads the OpenCode account file from the local machine
// (~/.local/share/opencode/account.json).
func ExtractAccount() (string, error) {
	path, err := AccountPath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}
