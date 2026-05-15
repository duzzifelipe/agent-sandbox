package main

import (
	"fmt"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/duck-labs/agentsdx-cli/internal/client"
	"github.com/duck-labs/agentsdx-shared/types"
	"github.com/spf13/cobra"
)

func newCreateCmd(c *client.Client) *cobra.Command {
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
		Options: []string{"virtualbox", "hetzner"},
		Default: "virtualbox",
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

	for {
		var addProject bool
		if err := survey.AskOne(&survey.Confirm{
			Message: "Add a project repository?",
			Default: false,
		}, &addProject); err != nil {
			return spec, err
		}
		if !addProject {
			break
		}
		var proj types.ProjectConfig
		if err := survey.Ask([]*survey.Question{
			{
				Name:     "repo",
				Prompt:   &survey.Input{Message: "Repo SSH URL (e.g. git@github.com:org/repo.git):"},
				Validate: survey.Required,
			},
			{
				Name:     "path",
				Prompt:   &survey.Input{Message: "Mount path in VM (e.g. ~/projects/api):"},
				Validate: survey.Required,
			},
		}, &proj); err != nil {
			return spec, err
		}
		spec.Projects = append(spec.Projects, proj)
	}

	return spec, nil
}
