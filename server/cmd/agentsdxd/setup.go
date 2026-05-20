package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	var vmDir string

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "One-time host setup: verify QEMU and initialise the Packer QEMU plugin",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(vmDir)
		},
	}

	cmd.Flags().StringVar(&vmDir, "vm-dir", envOrDefault("AGENTSDX_VM_DIR", "./vm"),
		"Path to the vm/ directory containing qemu.pkr.hcl")

	return cmd
}

func runSetup(vmDir string) error {
	fmt.Println("=== agentsdxd setup ===")

	if err := checkQEMU(); err != nil {
		return err
	}

	if err := setupPackerPlugin(vmDir); err != nil {
		return err
	}

	fmt.Println("\nSetup complete. You can now run: agentsdxd serve")
	return nil
}

func checkQEMU() error {
	fmt.Println("\n[1/2] Checking QEMU...")

	binary := qemuBinaryName()
	if _, err := exec.LookPath(binary); err != nil {
		return fmt.Errorf("%s not found in PATH — install QEMU first (brew install qemu)", binary)
	}

	if _, err := exec.LookPath("qemu-img"); err != nil {
		return fmt.Errorf("qemu-img not found in PATH — install QEMU first (brew install qemu)")
	}

	fmt.Printf("  %s and qemu-img found.\n", binary)
	return nil
}

func setupPackerPlugin(vmDir string) error {
	fmt.Println("\n[2/2] Initialising Packer QEMU plugin...")

	if _, err := exec.LookPath("packer"); err != nil {
		return fmt.Errorf("packer not found in PATH — install Packer first (brew install packer)")
	}

	hclPath := vmDir + "/qemu.pkr.hcl"
	if _, err := os.Stat(hclPath); err != nil {
		return fmt.Errorf("Packer template not found at %s: %w", hclPath, err)
	}

	cmd := exec.Command("packer", "init", hclPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("packer init: %w", err)
	}

	fmt.Println("  Packer QEMU plugin ready.")
	return nil
}

// qemuBinaryName returns the QEMU system binary for the current host architecture.
func qemuBinaryName() string {
	if runtime.GOARCH == "arm64" {
		return "qemu-system-aarch64"
	}
	return "qemu-system-x86_64"
}
