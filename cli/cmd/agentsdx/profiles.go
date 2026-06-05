package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/AlecAivazis/survey/v2"

	"github.com/duck-labs/agentsdx-cli/internal/client"
	"github.com/duck-labs/agentsdx-shared/types"
)

func newProfilesCmd(c *client.Client) *cobra.Command {
	parent := &cobra.Command{
		Use:   "profiles",
		Short: "Interact with profiles",
	}
	parent.AddCommand(newProfilesListCmd(c))
	parent.AddCommand(newProfilesBuildCmd(c))
	parent.AddCommand(newCreateProfileCmd(c))
	parent.AddCommand(newProfilesRepoCmd(c))

	return parent
}

func newCreateProfileCmd(c *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "create",
		Short: "Create a new sandbox profile interactively",
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := runWizard()
			if err != nil {
				return err
			}
			if err := c.CreateProfile(spec); err != nil {
				return fmt.Errorf("create profile: %w", err)
			}
			fmt.Printf("Profile %q created.\n", spec.Name)
			return nil
		},
	}
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


func newProfilesBuildCmd(c *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "build <profile>",
		Short: "Trigger an image build for a profile",
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

func newProfilesListCmd(c *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
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

func newProfilesRepoCmd(c *client.Client) *cobra.Command {
	parent := &cobra.Command{
		Use:   "repo",
		Short: "Manage repositories in a profile",
	}
	parent.AddCommand(newProfilesRepoAddCmd(c))
	return parent
}

func newProfilesRepoAddCmd(c *client.Client) *cobra.Command {
	var authTokenEnv string
	cmd := &cobra.Command{
		Use:   "add <profile> <repo-url> [path]",
		Short: "Add a repository to a profile",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := args[0]
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
			if err := c.AddProject(profile, proj); err != nil {
				return fmt.Errorf("add project: %w", err)
			}
			fmt.Printf("Repository %q added to profile %q (path: %s).\n", repoURL, profile, mountPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&authTokenEnv, "auth-token-env", "", "Name of the secret whose value is used as git auth token")
	return cmd
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
