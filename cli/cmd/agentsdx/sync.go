package main

import (
	"github.com/duck-labs/agentsdx-cli/internal/client"
	"github.com/duck-labs/agentsdx-cli/internal/state"
	"github.com/spf13/cobra"
)

func newSyncCmd(c *client.Client, s *state.State) *cobra.Command {
	return &cobra.Command{Use: "sync <profile> <file>", Short: "Push a file into a running VM", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
}
