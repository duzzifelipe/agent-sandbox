package main

import (
	"context"
	"fmt"

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

	token := mustEnv("AGENTSDX_HCLOUD_TOKEN")
	fmt.Println("\n[1/1] Verifying Hetzner Cloud token...")

	client := hcloud.NewClient(hcloud.WithToken(token))
	opts := hcloud.ServerListOpts{ListOpts: hcloud.ListOpts{PerPage: 1}}
	if _, _, err := client.Server.List(context.Background(), opts); err != nil {
		return fmt.Errorf("hcloud token invalid or API unreachable: %w", err)
	}

	fmt.Println("  Token valid. You can now run: agentsdxd serve")
	return nil
}
