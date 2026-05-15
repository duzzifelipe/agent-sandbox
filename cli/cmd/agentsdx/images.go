package main

import (
	"fmt"

	"github.com/duck-labs/agentsdx-cli/internal/client"
	"github.com/spf13/cobra"
)

func newImagesCmd(c *client.Client) *cobra.Command {
	parent := &cobra.Command{
		Use:   "images",
		Short: "Manage VM images",
	}
	parent.AddCommand(newImagesBuildCmd(c))
	return parent
}

func newImagesBuildCmd(c *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "build <profile>",
		Short: "Trigger a Packer image build for a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := args[0]
			fmt.Printf("Triggering image build for profile %q...\n", profile)
			if err := c.BuildImage(profile); err != nil {
				return fmt.Errorf("build image: %w", err)
			}
			fmt.Println("Image build started. Check server logs for progress.")
			return nil
		},
	}
}
