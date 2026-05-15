package main

import (
	"github.com/duck-labs/agentsdx-cli/internal/client"
	"github.com/spf13/cobra"
)

func newCredentialsCmd(c *client.Client) *cobra.Command {
	return &cobra.Command{Use: "credentials", Short: "Manage credentials"}
}
