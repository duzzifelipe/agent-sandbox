package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/AlecAivazis/survey/v2"
	"github.com/spf13/cobra"

	"github.com/duck-labs/agentsdx/internal/builder"
	"github.com/duck-labs/agentsdx/internal/datadir"
	"github.com/duck-labs/agentsdx/internal/profile"
	"github.com/duck-labs/agentsdx/internal/session"
	"github.com/duck-labs/agentsdx/internal/types"
	"github.com/duck-labs/agentsdx/internal/vault"
	"github.com/duck-labs/agentsdx/internal/vm"
)

func newProfilesCmd(
	profiles *profile.Store,
	images *vm.ImageStore,
	vmProviders map[string]vm.VMProvider,
	imageProviders map[string]vm.ImageProvider,
	vaultSecret string,
) *cobra.Command {
	parent := &cobra.Command{
		Use:   "profiles",
		Short: "Manage sandbox profiles",
	}
	parent.AddCommand(newProfilesListCmd(profiles))
	parent.AddCommand(newProfilesCreateCmd(profiles))
	parent.AddCommand(newProfilesRunCmd(profiles, images, vmProviders, vaultSecret))
	parent.AddCommand(newProfilesBuildCmd(profiles, images, imageProviders, vaultSecret))
	parent.AddCommand(newProfilesRepoCmd(profiles))
	return parent
}

func newProfilesListCmd(profiles *profile.Store) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List sandbox profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			specs, err := profiles.List()
			if err != nil {
				return err
			}
			if len(specs) == 0 {
				fmt.Println("No profiles found. Run 'agentsdx profiles create' to create one.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tPROVIDER\tAGENT\tPROJECTS")
			for _, p := range specs {
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", p.Name, p.Infrastructure.Provider, p.Agent.Provider, len(p.Projects))
			}
			return w.Flush()
		},
	}
}

func newProfilesCreateCmd(profiles *profile.Store) *cobra.Command {
	return &cobra.Command{
		Use:   "create",
		Short: "Create a new sandbox profile interactively",
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := runWizard()
			if err != nil {
				return err
			}
			if err := profiles.Create(spec); err != nil {
				return fmt.Errorf("create profile: %w", err)
			}
			fmt.Printf("Profile %q created.\n", spec.Name)
			return nil
		},
	}
}

func newProfilesRunCmd(profiles *profile.Store, images *vm.ImageStore, vmProviders map[string]vm.VMProvider, vaultSecret string) *cobra.Command {
	return &cobra.Command{
		Use:   "run <profile>",
		Short: "Start a sandbox session and open an SSH connection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			spec, err := profiles.Get(name)
			if err != nil {
				return err
			}

			provider, ok := vmProviders[spec.Infrastructure.Provider]
			if !ok {
				return fmt.Errorf("provider %q not configured — check environment variables", spec.Infrastructure.Provider)
			}

			vaultData, err := loadOrInitVault(name, vaultSecret)
			if err != nil {
				return err
			}

			return session.Run(context.Background(), spec, vaultData, provider, images)
		},
	}
}

func newProfilesBuildCmd(profiles *profile.Store, images *vm.ImageStore, imageProviders map[string]vm.ImageProvider, vaultSecret string) *cobra.Command {
	return &cobra.Command{
		Use:   "build <profile>",
		Short: "Build a VM image for a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			spec, err := profiles.Get(name)
			if err != nil {
				return err
			}

			provider, ok := imageProviders[spec.Infrastructure.Provider]
			if !ok {
				return fmt.Errorf("image provider %q not configured — check environment variables", spec.Infrastructure.Provider)
			}

			vmDir := "vm"
			b := builder.New(vmDir, images, map[string]vm.ImageProvider{spec.Infrastructure.Provider: provider})

			fmt.Printf("Building image for profile %q...\n", name)
			snapshotID, err := b.Build(context.Background(), spec)
			if err != nil {
				return fmt.Errorf("build: %w", err)
			}
			fmt.Printf("Build complete: %s\n", snapshotID)
			return nil
		},
	}
}

func newProfilesRepoCmd(profiles *profile.Store) *cobra.Command {
	parent := &cobra.Command{
		Use:   "repo",
		Short: "Manage repositories in a profile",
	}
	parent.AddCommand(newProfilesRepoAddCmd(profiles))
	return parent
}

func newProfilesRepoAddCmd(profiles *profile.Store) *cobra.Command {
	var authTokenEnv string
	cmd := &cobra.Command{
		Use:   "add <profile> <repo-url> [path]",
		Short: "Add a repository to a profile",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName := args[0]
			repoURL := args[1]

			var mountPath string
			if len(args) == 3 {
				mountPath = args[2]
			} else {
				name, err := repoNameFromURL(repoURL)
				if err != nil {
					return fmt.Errorf("cannot derive path from %q: %w — provide an explicit path", repoURL, err)
				}
				mountPath = "~/" + name
			}

			proj := types.ProjectConfig{
				Repo:         repoURL,
				Path:         mountPath,
				AuthTokenEnv: authTokenEnv,
			}
			if err := profiles.AddProject(profileName, proj); err != nil {
				return fmt.Errorf("add project: %w", err)
			}
			fmt.Printf("Repository %q added to profile %q (path: %s).\n", repoURL, profileName, mountPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&authTokenEnv, "auth-token-env", "", "Name of the secret whose value is used as git auth token")
	return cmd
}

func runWizard() (types.ProfileSpec, error) {
	var spec types.ProfileSpec

	if err := survey.AskOne(&survey.Input{
		Message: "Profile name:",
		Help:    "Unique name for this sandbox (e.g. work-backend)",
	}, &spec.Name, survey.WithValidator(survey.Required)); err != nil {
		return spec, err
	}

	if err := survey.AskOne(&survey.Select{
		Message: "VM provider:",
		Options: []string{"local", "hetzner"},
		Default: "local",
	}, &spec.Infrastructure.Provider); err != nil {
		return spec, err
	}

	if err := survey.AskOne(&survey.Select{
		Message: "Base OS image:",
		Options: []string{"ubuntu-24.04"},
		Default: "ubuntu-24.04",
	}, &spec.Infrastructure.Image); err != nil {
		return spec, err
	}

	if err := survey.AskOne(&survey.MultiSelect{
		Message: "Tooling to install:",
		Options: []string{"mise", "docker", "docker-compose", "gh"},
	}, &spec.Infrastructure.Tooling); err != nil {
		return spec, err
	}

	if err := survey.AskOne(&survey.Select{
		Message: "Agent:",
		Options: []string{"claude", "opencode", "hermes"},
		Default: "claude",
	}, &spec.Agent.Provider); err != nil {
		return spec, err
	}

	var skillsInput string
	if err := survey.AskOne(&survey.Input{
		Message: "Skills (comma-separated, optional):",
		Help:    "e.g. superpowers/brainstorming,superpowers/tdd",
	}, &skillsInput); err != nil {
		return spec, err
	}
	if skillsInput != "" {
		for _, s := range strings.Split(skillsInput, ",") {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				spec.Agent.Skills = append(spec.Agent.Skills, trimmed)
			}
		}
	}

	return spec, nil
}

func loadOrInitVault(profileName, vaultSecret string) (types.VaultData, error) {
	vaultDir := datadir.VaultDir()
	if vault.VaultExists(vaultDir, profileName) {
		return vault.LoadVaultData(vaultDir, profileName, vaultSecret)
	}

	vmPriv, vmPub, err := vault.GenerateKeyPair()
	if err != nil {
		return types.VaultData{}, fmt.Errorf("generate vm key pair: %w", err)
	}
	gitPriv, gitPub, err := vault.GenerateKeyPair()
	if err != nil {
		return types.VaultData{}, fmt.Errorf("generate git key pair: %w", err)
	}
	vd := types.DefaultVaultData()
	vd.VMAccessPrivateKey = vmPriv
	vd.VMAccessPublicKey = vmPub
	vd.GitPrivateKey = gitPriv
	vd.GitPublicKey = gitPub
	if err := vault.StoreVaultData(vaultDir, profileName, vaultSecret, vd); err != nil {
		return types.VaultData{}, fmt.Errorf("init vault: %w", err)
	}
	return vd, nil
}

func repoNameFromURL(rawURL string) (string, error) {
	rawURL = strings.TrimSuffix(rawURL, "/")
	var segment string
	if idx := strings.LastIndex(rawURL, "/"); idx >= 0 {
		segment = rawURL[idx+1:]
	} else {
		segment = rawURL
	}
	name := strings.TrimSuffix(segment, ".git")
	if name == "" {
		return "", fmt.Errorf("could not extract repo name from URL")
	}
	return name, nil
}
