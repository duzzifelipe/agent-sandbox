package main

import (
	"github.com/duck-labs/agentsdx-cli/internal/client"
	"github.com/spf13/cobra"
)

func newImagesCmd(c *client.Client) *cobra.Command {
	return &cobra.Command{Use: "images", Short: "Manage VM images"}
}
