package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	var vmDir string

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "One-time host setup: configure vboxnet0 and initialise the Packer plugin",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(vmDir)
		},
	}

	cmd.Flags().StringVar(&vmDir, "vm-dir", envOrDefault("AGENTSDX_VM_DIR", "./vm"),
		"Path to the vm/ directory containing virtualbox.pkr.hcl")

	return cmd
}

func runSetup(vmDir string) error {
	fmt.Println("=== agentsdxd setup ===")

	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		return runSetupDarwinARM64(vmDir)
	}

	if err := setupVboxnet0(); err != nil {
		return err
	}
	if err := setupPackerPlugin(vmDir, "virtualbox.pkr.hcl"); err != nil {
		return err
	}
	fmt.Println("\nSetup complete. You can now run: agentsdxd serve")
	return nil
}

func runSetupDarwinARM64(vmDir string) error {
	fmt.Println("\n[1/1] Initialising Packer QEMU plugin for Apple VZ...")

	packerPath, err := exec.LookPath("packer")
	if err != nil {
		fmt.Println("  Packer not found — installing via Homebrew...")
		if out, err := exec.Command("brew", "install", "packer").CombinedOutput(); err != nil {
			return fmt.Errorf("install packer: %w\n%s", err, out)
		}
		packerPath = "packer"
		fmt.Println("  Packer installed.")
	}

	hclPath := filepath.Join(vmDir, "applevz.pkr.hcl")
	if _, err := os.Stat(hclPath); err != nil {
		return fmt.Errorf("Packer template not found at %s: %w", hclPath, err)
	}

	cmd := exec.Command(packerPath, "init", hclPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("packer init: %w", err)
	}

	fmt.Println("  QEMU plugin ready.")
	fmt.Println("\nSetup complete.")
	fmt.Println("NOTE: agentsdxd serve requires sudo (or com.apple.vm.networking entitlement) for VM networking.")
	return nil
}

func setupVboxnet0() error {
	fmt.Println("\n[1/2] Configuring VirtualBox host-only adapter (vboxnet0)...")

	vboxManage, err := findVBoxManage()
	if err != nil {
		return fmt.Errorf("VirtualBox not found — install it with: brew install --cask virtualbox")
	}

	// Check if vboxnet0 already exists and is correctly configured.
	out, err := exec.Command(vboxManage, "list", "hostonlyifs").Output()
	if err != nil {
		return fmt.Errorf("list hostonlyifs: %w", err)
	}

	if hasVboxnet0(string(out)) {
		fmt.Println("  vboxnet0 already exists — skipping creation.")
	} else {
		fmt.Println("  Creating vboxnet0...")
		if out, err := exec.Command(vboxManage, "hostonlyif", "create").CombinedOutput(); err != nil {
			return fmt.Errorf("create hostonlyif: %w\n%s", err, out)
		}
	}

	fmt.Println("  Configuring vboxnet0 (192.168.56.1/24)...")
	out2, err := exec.Command(vboxManage, "hostonlyif", "ipconfig", "vboxnet0",
		"--ip", "192.168.56.1", "--netmask", "255.255.255.0").CombinedOutput()
	if err != nil {
		return fmt.Errorf("configure vboxnet0: %w\n%s", err, out2)
	}

	fmt.Println("  vboxnet0 ready (192.168.56.1/24).")
	return nil
}

func findVBoxManage() (string, error) {
	if path, err := exec.LookPath("VBoxManage"); err == nil {
		return path, nil
	}
	defaultPath := "/Applications/VirtualBox.app/Contents/MacOS/VBoxManage"
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath, nil
	}
	return "", fmt.Errorf("VBoxManage not found")
}

func setupPackerPlugin(vmDir, hclFile string) error {
	fmt.Println("\n[2/2] Initialising Packer VirtualBox plugin...")

	packerPath, err := exec.LookPath("packer")
	if err != nil {
		fmt.Println("  Packer not found — installing via Homebrew...")
		if out, err := exec.Command("brew", "install", "packer").CombinedOutput(); err != nil {
			return fmt.Errorf("install packer: %w\n%s", err, out)
		}
		packerPath = "packer"
		fmt.Println("  Packer installed successfully.")
	}

	hclPath := filepath.Join(vmDir, hclFile)
	if _, err := os.Stat(hclPath); err != nil {
		return fmt.Errorf("Packer template not found at %s: %w", hclPath, err)
	}

	cmd := exec.Command(packerPath, "init", hclPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("packer init: %w", err)
	}

	fmt.Println("  Packer VirtualBox plugin ready.")
	return nil
}

// hasVboxnet0 reports whether vboxnet0 appears in VBoxManage list hostonlyifs output.
func hasVboxnet0(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Name:") &&
			strings.Contains(line, "vboxnet0") {
			return true
		}
	}
	return false
}
