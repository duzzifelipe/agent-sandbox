package main

import (
	"fmt"

	"github.com/duck-labs/agentsdx-cli/internal/client"
	"github.com/duck-labs/agentsdx-cli/internal/state"
	"github.com/spf13/cobra"
)

func newStopCmd(c *client.Client, s *state.State) *cobra.Command {
	return &cobra.Command{
		Use:   "stop <profile>",
		Short: "Stop a running sandbox session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := args[0]

			sessionID, ok := s.Get(profile)
			if !ok {
				return fmt.Errorf("no active session found for profile %q", profile)
			}

			fmt.Printf("Stopping session %s...\n", sessionID)
			if err := c.StopSession(sessionID); err != nil {
				return fmt.Errorf("stop session: %w", err)
			}

			s.Delete(profile)
			fmt.Println("Session stopped.")
			return nil
		},
	}
}
