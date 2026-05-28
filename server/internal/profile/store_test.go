package profile_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/db"
	"github.com/duck-labs/agentsdx-server/internal/profile"
	"github.com/duck-labs/agentsdx-shared/types"
)

func sampleSpec(name string) types.ProfileSpec {
	return types.ProfileSpec{
		Name: name,
		Infrastructure: types.InfrastructureConfig{
			Provider: "hetzner",
			Image:    "ubuntu-24.04",
			Tooling:  []string{"git", "node"},
		},
		Projects: []types.ProjectConfig{
			{Repo: "https://github.com/example/repo", Path: "/workspace/repo"},
		},
		Agent: types.AgentConfig{
			Provider: "claude",
			Skills:   []string{"coding"},
		},
	}
}

func newStore(t *testing.T) *profile.Store {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return profile.NewStore(conn, "")
}

func TestStore_CreateAndGet(t *testing.T) {
	s := newStore(t)
	spec := sampleSpec("test-profile")

	if err := s.Create(spec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Get(spec.Name)
	if err != nil {
		t.Fatalf("Get after Create: %v", err)
	}
	if !reflect.DeepEqual(spec, got) {
		t.Errorf("Get returned\n%+v\nwant\n%+v", got, spec)
	}
}

func TestStore_List_ReturnsAllProfiles(t *testing.T) {
	s := newStore(t)

	spec1 := sampleSpec("alpha")
	spec2 := sampleSpec("beta")

	if err := s.Create(spec1); err != nil {
		t.Fatalf("Create spec1: %v", err)
	}
	if err := s.Create(spec2); err != nil {
		t.Fatalf("Create spec2: %v", err)
	}

	all, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(all))
	}
	names := map[string]bool{}
	for _, p := range all {
		names[p.Name] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("missing profiles in list: %v", names)
	}
}

func TestStore_Delete_RemovesProfile(t *testing.T) {
	s := newStore(t)
	spec := sampleSpec("to-delete")

	if err := s.Create(spec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Delete(spec.Name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(spec.Name); err == nil {
		t.Error("Get after Delete: expected error, got nil")
	}
	all, err := s.List()
	if err != nil {
		t.Fatalf("List after Delete: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected empty list after delete, got %v", all)
	}
}

func TestStore_Create_FailsOnDuplicate(t *testing.T) {
	s := newStore(t)
	spec := sampleSpec("dup-profile")

	if err := s.Create(spec); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if err := s.Create(spec); err == nil {
		t.Fatal("expected error on duplicate Create, got nil")
	}

	got, err := s.Get(spec.Name)
	if err != nil {
		t.Fatalf("Get after failed duplicate: %v", err)
	}
	if !reflect.DeepEqual(spec, got) {
		t.Errorf("original profile corrupted")
	}
}

func TestStore_Get_NotFound(t *testing.T) {
	s := newStore(t)
	if _, err := s.Get("no-such-profile"); err == nil {
		t.Error("expected error for missing profile, got nil")
	}
}
