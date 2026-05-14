package types_test

import (
	"testing"

	"github.com/duck-labs/agentsdx-shared/types"
	"gopkg.in/yaml.v3"
)

func TestProfileSpec_YAMLRoundtrip(t *testing.T) {
	input := `
name: work-backend
infrastructure:
  provider: virtualbox
  image: ubuntu-24.04
  tooling:
    - mise
    - docker
    - gh
projects:
  - repo: git@github.com:org/api.git
    path: ~/projects/api
agent:
  provider: claude
  skills:
    - superpowers/brainstorming
`
	var got types.ProfileSpec
	if err := yaml.Unmarshal([]byte(input), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Name != "work-backend" {
		t.Errorf("Name: got %q, want %q", got.Name, "work-backend")
	}
	if got.Infrastructure.Provider != "virtualbox" {
		t.Errorf("Infrastructure.Provider: got %q, want %q", got.Infrastructure.Provider, "virtualbox")
	}
	if got.Infrastructure.Image != "ubuntu-24.04" {
		t.Errorf("Infrastructure.Image: got %q, want %q", got.Infrastructure.Image, "ubuntu-24.04")
	}
	if len(got.Infrastructure.Tooling) != 3 {
		t.Errorf("Infrastructure.Tooling: got %d items, want 3", len(got.Infrastructure.Tooling))
	}
	if len(got.Projects) != 1 || got.Projects[0].Repo != "git@github.com:org/api.git" {
		t.Errorf("Projects: got %+v", got.Projects)
	}
	if got.Agent.Provider != "claude" {
		t.Errorf("Agent.Provider: got %q, want %q", got.Agent.Provider, "claude")
	}
	if len(got.Agent.Skills) != 1 {
		t.Errorf("Agent.Skills: got %d items, want 1", len(got.Agent.Skills))
	}

	// Marshal back and unmarshal again to verify roundtrip
	out, err := yaml.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got2 types.ProfileSpec
	if err := yaml.Unmarshal(out, &got2); err != nil {
		t.Fatalf("second unmarshal: %v", err)
	}
	if got2.Name != got.Name {
		t.Errorf("roundtrip Name mismatch: %q vs %q", got2.Name, got.Name)
	}
}
