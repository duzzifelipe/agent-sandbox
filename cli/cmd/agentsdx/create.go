package main

import (
	"github.com/duck-labs/agentsdx-cli/internal/client"
	"github.com/spf13/cobra"
)

func newCreateCmd(c *client.Client) *cobra.Command {
	return &cobra.Command{Use: "create", Short: "Create a sandbox profile", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
}
