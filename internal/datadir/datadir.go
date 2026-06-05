package datadir

import (
	"os"
	"path/filepath"
)

func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agentsdx")
}

func ProfilesDir() string      { return filepath.Join(Dir(), "profiles") }
func VaultDir() string         { return filepath.Join(Dir(), "vault") }
func ImagesFile() string       { return filepath.Join(Dir(), "images.json") }
func QEMUDataDir() string      { return filepath.Join(Dir(), "qemu") }
