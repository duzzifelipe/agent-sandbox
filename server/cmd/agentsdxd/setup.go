package main

import (
	"fmt"
	"os"
	"os/exec"
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

	if err := setupVboxnet0(); err != nil {
		return err
	}

	if err := setupPackerPlugin(vmDir); err != nil {
		return err
	}

	fmt.Println("\nSetup complete. You can now run: agentsdxd serve")
	return nil
}

func setupVboxnet0() error {
	fmt.Println("\n[1/2] Configuring VirtualBox host-only adapter (vboxnet0)...")

	if _, err := exec.LookPath("VBoxManage"); err != nil {
		return fmt.Errorf("VBoxManage not found in PATH — install VirtualBox first")
	}

	// Check if vboxnet0 already exists and is correctly configured.
	out, err := exec.Command("VBoxManage", "list", "hostonlyifs").Output()
	if err != nil {
		return fmt.Errorf("list hostonlyifs: %w", err)
	}

	if hasVboxnet0(string(out)) {
		fmt.Println("  vboxnet0 already exists — skipping creation.")
	} else {
		fmt.Println("  Creating vboxnet0...")
		if out, err := exec.Command("VBoxManage", "hostonlyif", "create").CombinedOutput(); err != nil {
			return fmt.Errorf("create hostonlyif: %w\n%s", err, out)
		}
	}

	fmt.Println("  Configuring vboxnet0 (192.168.56.1/24)...")
	out2, err := exec.Command("VBoxManage", "hostonlyif", "ipconfig", "vboxnet0",
		"--ip", "192.168.56.1", "--netmask", "255.255.255.0").CombinedOutput()
	if err != nil {
		return fmt.Errorf("configure vboxnet0: %w\n%s", err, out2)
	}

	fmt.Println("  vboxnet0 ready (192.168.56.1/24).")
	return nil
}

func setupPackerPlugin(vmDir string) error {
	fmt.Println("\n[2/2] Initialising Packer VirtualBox plugin...")

	if _, err := exec.LookPath("packer"); err != nil {
		return fmt.Errorf("packer not found in PATH — install Packer first (brew install packer)")
	}

	hclPath := vmDir + "/virtualbox.pkr.hcl"
	if _, err := os.Stat(hclPath); err != nil {
		return fmt.Errorf("Packer template not found at %s: %w", hclPath, err)
	}

	cmd := exec.Command("packer", "init", hclPath)
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
