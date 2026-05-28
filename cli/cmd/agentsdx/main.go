package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/duck-labs/agentsdx-cli/internal/client"
	"github.com/duck-labs/agentsdx-cli/internal/state"
)

func main() {
	serverURL := os.Getenv("AGENTSDX_URL")
	if serverURL == "" {
		fmt.Fprintln(os.Stderr, "error: AGENTSDX_URL environment variable is not set")
		os.Exit(1)
	}

	c := client.New(serverURL)
	s := state.New(state.DefaultPath())

	root := &cobra.Command{
		Use:   "agentsdx",
		Short: "Manage remote agent sandboxes",
	}

	root.AddCommand(
		newProfilesCmd(c),
		newCreateCmd(c),
		newRunCmd(c, s),
		newStopCmd(c, s),
		newCredentialsCmd(c),
		newSyncCmd(c, s),
		newImagesCmd(c),
		newSecretsCmd(c),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
