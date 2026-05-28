package main

import (
	"fmt"

	"github.com/duck-labs/agentsdx-cli/internal/client"
	"github.com/spf13/cobra"
)

func newSecretsCmd(c *client.Client) *cobra.Command {
	parent := &cobra.Command{
		Use:   "secrets",
		Short: "Manage per-profile secrets",
	}
	parent.AddCommand(newSecretsSetCmd(c))
	parent.AddCommand(newSecretsDeleteCmd(c))
	parent.AddCommand(newSecretsListCmd(c))
	return parent
}

func newSecretsSetCmd(c *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "set <profile> <KEY> <VALUE>",
		Short: "Set or overwrite a secret for a profile",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile, key, value := args[0], args[1], args[2]
			if err := c.SetSecret(profile, key, value); err != nil {
				return fmt.Errorf("set secret: %w", err)
			}
			fmt.Printf("Secret %q set for profile %q.\n", key, profile)
			return nil
		},
	}
}

func newSecretsDeleteCmd(c *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <profile> <KEY>",
		Short: "Remove a secret from a profile",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile, key := args[0], args[1]
			if err := c.DeleteSecret(profile, key); err != nil {
				return fmt.Errorf("delete secret: %w", err)
			}
			fmt.Printf("Secret %q deleted from profile %q.\n", key, profile)
			return nil
		},
	}
}

func newSecretsListCmd(c *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "list <profile>",
		Short: "List secret key names for a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keys, err := c.ListSecrets(args[0])
			if err != nil {
				return fmt.Errorf("list secrets: %w", err)
			}
			if len(keys) == 0 {
				fmt.Println("No secrets set.")
				return nil
			}
			for _, k := range keys {
				fmt.Println(k)
			}
			return nil
		},
	}
}
