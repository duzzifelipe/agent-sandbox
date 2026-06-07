package profile_test

import (
	"testing"

	"github.com/duck-labs/agentsdx/internal/profile"
	"github.com/duck-labs/agentsdx/internal/types"
)

func TestCreateAndGet(t *testing.T) {
	dir := t.TempDir()
	s := profile.NewStore(dir)

	spec := types.ProfileSpec{
		Name: "test",
		Infrastructure: types.InfrastructureConfig{
			Provider: "local",
			Image:    "ubuntu-24.04",
			Tooling:  []string{"mise"},
		},
		Agent: types.AgentConfig{Providers: []string{"claude"}},
	}

	if err := s.Create(spec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Get("test")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "test" || got.Infrastructure.Provider != "local" {
		t.Errorf("unexpected spec: %+v", got)
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	s := profile.NewStore(dir)

	_ = s.Create(types.ProfileSpec{Name: "a", Infrastructure: types.InfrastructureConfig{Provider: "local"}, Agent: types.AgentConfig{Providers: []string{"claude"}}})
	_ = s.Create(types.ProfileSpec{Name: "b", Infrastructure: types.InfrastructureConfig{Provider: "hetzner"}, Agent: types.AgentConfig{Providers: []string{"claude"}}})

	specs, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(specs))
	}
}

func TestCreateDuplicate(t *testing.T) {
	dir := t.TempDir()
	s := profile.NewStore(dir)
	spec := types.ProfileSpec{Name: "dup", Infrastructure: types.InfrastructureConfig{Provider: "local"}, Agent: types.AgentConfig{Providers: []string{"claude"}}}
	_ = s.Create(spec)
	if err := s.Create(spec); err == nil {
		t.Error("expected error on duplicate create")
	}
}

func TestGetMissing(t *testing.T) {
	dir := t.TempDir()
	s := profile.NewStore(dir)
	if _, err := s.Get("missing"); err == nil {
		t.Error("expected error for missing profile")
	}
}

func TestAddProject(t *testing.T) {
	dir := t.TempDir()
	s := profile.NewStore(dir)
	_ = s.Create(types.ProfileSpec{Name: "p", Infrastructure: types.InfrastructureConfig{Provider: "local"}, Agent: types.AgentConfig{Providers: []string{"claude"}}})

	proj := types.ProjectConfig{Repo: "https://github.com/example/repo", Path: "~/repo"}
	if err := s.AddProject("p", proj); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	got, _ := s.Get("p")
	if len(got.Projects) != 1 || got.Projects[0].Repo != proj.Repo {
		t.Errorf("unexpected projects: %+v", got.Projects)
	}
}
