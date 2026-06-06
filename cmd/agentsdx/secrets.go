package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/duck-labs/agentsdx/internal/claudecreds"
	"github.com/duck-labs/agentsdx/internal/datadir"
	"github.com/duck-labs/agentsdx/internal/vault"
)

func newSecretsCmd(vaultSecret string) *cobra.Command {
	parent := &cobra.Command{
		Use:   "secrets",
		Short: "Manage per-profile secrets",
	}
	parent.AddCommand(newSecretsSetCmd(vaultSecret))
	parent.AddCommand(newSecretsDeleteCmd(vaultSecret))
	parent.AddCommand(newSecretsListCmd(vaultSecret))
	parent.AddCommand(newSecretsImportFromClaudeCmd(vaultSecret))
	return parent
}

func newSecretsSetCmd(vaultSecret string) *cobra.Command {
	return &cobra.Command{
		Use:   "set <profile> <KEY>",
		Short: "Set or overwrite a secret for a profile",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName, key := args[0], args[1]
			fmt.Fprintf(os.Stderr, "Enter value for %q: ", key)
			valueBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return fmt.Errorf("read secret value: %w", err)
			}
			value := string(valueBytes)
			vd, err := loadOrInitVault(profileName, vaultSecret)
			if err != nil {
				return err
			}
			if vd.Secrets == nil {
				vd.Secrets = make(map[string]string)
			}
			vd.Secrets[key] = value
			if err := vault.StoreVaultData(datadir.VaultDir(), profileName, vaultSecret, vd); err != nil {
				return fmt.Errorf("store vault: %w", err)
			}
			fmt.Printf("Secret %q set for profile %q.\n", key, profileName)
			return nil
		},
	}
}

func newSecretsDeleteCmd(vaultSecret string) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <profile> <KEY>",
		Short: "Remove a secret from a profile",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName, key := args[0], args[1]
			if !vault.VaultExists(datadir.VaultDir(), profileName) {
				fmt.Printf("Secret %q deleted from profile %q.\n", key, profileName)
				return nil
			}
			vd, err := vault.LoadVaultData(datadir.VaultDir(), profileName, vaultSecret)
			if err != nil {
				return err
			}
			delete(vd.Secrets, key)
			if err := vault.StoreVaultData(datadir.VaultDir(), profileName, vaultSecret, vd); err != nil {
				return fmt.Errorf("store vault: %w", err)
			}
			fmt.Printf("Secret %q deleted from profile %q.\n", key, profileName)
			return nil
		},
	}
}

func newSecretsImportFromClaudeCmd(vaultSecret string) *cobra.Command {
	return &cobra.Command{
		Use:   "import-from-claude <profile>",
		Short: "Import Claude Code credentials from this machine into a profile vault",
		Long: `Reads Claude Code credentials from the local machine (macOS Keychain or
~/.claude/.credentials.json on Linux) and stores them under the reserved key
AGENTSDX_CLAUDE_CREDENTIALS in the profile vault. When a sandbox session starts,
these credentials are automatically injected so Claude Code skips login.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName := args[0]

			creds, err := claudecreds.Extract()
			if err != nil {
				return fmt.Errorf("extract Claude credentials: %w", err)
			}

			vd, err := loadOrInitVault(profileName, vaultSecret)
			if err != nil {
				return err
			}
			if vd.Secrets == nil {
				vd.Secrets = make(map[string]string)
			}
			vd.Secrets[claudecreds.VaultKey] = creds
			if err := vault.StoreVaultData(datadir.VaultDir(), profileName, vaultSecret, vd); err != nil {
				return fmt.Errorf("store vault: %w", err)
			}
			fmt.Printf("Claude Code credentials stored for profile %q.\n", profileName)
			return nil
		},
	}
}

func newSecretsListCmd(vaultSecret string) *cobra.Command {
	return &cobra.Command{
		Use:   "list <profile>",
		Short: "List secret key names for a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName := args[0]
			if !vault.VaultExists(datadir.VaultDir(), profileName) {
				fmt.Println("No secrets set.")
				return nil
			}
			vd, err := vault.LoadVaultData(datadir.VaultDir(), profileName, vaultSecret)
			if err != nil {
				return err
			}
			if len(vd.Secrets) == 0 {
				fmt.Println("No secrets set.")
				return nil
			}
			for k := range vd.Secrets {
				fmt.Println(k)
			}
			return nil
		},
	}
}
