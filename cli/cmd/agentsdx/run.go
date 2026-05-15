package main

import (
	"github.com/duck-labs/agentsdx-cli/internal/client"
	"github.com/duck-labs/agentsdx-cli/internal/state"
	"github.com/spf13/cobra"
)

func newRunCmd(c *client.Client, s *state.State) *cobra.Command {
	return &cobra.Command{Use: "run <profile>", Short: "Start a sandbox session", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
}
