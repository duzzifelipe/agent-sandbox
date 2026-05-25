package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Verify Hetzner Cloud credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup()
		},
	}
}

func runSetup() error {
	fmt.Println("=== agentsdxd setup ===")

	// Check QEMU availability (always)
	fmt.Println("\n[1/2] Checking QEMU availability...")
	if _, err := exec.LookPath("qemu-system-aarch64"); err != nil {
		fmt.Println("  WARNING: qemu-system-aarch64 not found on PATH — local provider will not work")
	} else {
		fmt.Println("  qemu-system-aarch64 found.")
	}

	// Check Hetzner token (only if set)
	fmt.Println("\n[2/2] Checking Hetzner Cloud token...")
	token := os.Getenv("AGENTSDX_HCLOUD_TOKEN")
	if token == "" {
		fmt.Println("  AGENTSDX_HCLOUD_TOKEN not set — Hetzner provider will not be available.")
	} else {
		client := hcloud.NewClient(hcloud.WithToken(token))
		opts := hcloud.ServerListOpts{ListOpts: hcloud.ListOpts{PerPage: 1}}
		if _, _, err := client.Server.List(context.Background(), opts); err != nil {
			return fmt.Errorf("hcloud token invalid or API unreachable: %w", err)
		}
		fmt.Println("  Token valid.")
	}

	fmt.Println("\nSetup complete. You can now run: agentsdxd serve")
	return nil
}
