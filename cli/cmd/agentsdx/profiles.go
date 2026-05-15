package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/duck-labs/agentsdx-cli/internal/client"
)

func newProfilesCmd(c *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "profiles",
		Short: "List sandbox profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			profiles, err := c.ListProfiles()
			if err != nil {
				return err
			}
			if len(profiles) == 0 {
				fmt.Println("No profiles found. Run 'agentsdx create' to create one.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tPROVIDER\tAGENT\tPROJECTS")
			for _, p := range profiles {
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", p.Name, p.Infrastructure.Provider, p.Agent.Provider, len(p.Projects))
			}
			return w.Flush()
		},
	}
}
