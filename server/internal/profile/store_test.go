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
	return profile.NewStore(conn, t.TempDir())
}

func TestStore_Create_WritesYAMLAndSQLite(t *testing.T) {
	s := newStore(t)
	spec := sampleSpec("test-profile")

	if err := s.Create(spec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify YAML file exists by calling Get.
	got, err := s.Get(spec.Name)
	if err != nil {
		t.Fatalf("Get after Create: %v", err)
	}
	if got.Name != spec.Name {
		t.Errorf("name: got %q, want %q", got.Name, spec.Name)
	}

	// Verify SQLite row exists by listing.
	all, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 || all[0].Name != spec.Name {
		t.Errorf("list: expected 1 profile %q, got %v", spec.Name, all)
	}
}

func TestStore_Get_ReadsFromYAML(t *testing.T) {
	s := newStore(t)
	spec := sampleSpec("test-profile")

	if err := s.Create(spec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Get(spec.Name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if !reflect.DeepEqual(spec, got) {
		t.Errorf("Get returned\n%+v\nwant\n%+v", got, spec)
	}
}

func TestStore_List_ReturnsAllProfiles(t *testing.T) {
	s := newStore(t)

	spec1 := sampleSpec("test-profile")
	spec2 := sampleSpec("second-profile")

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
	if !names[spec1.Name] {
		t.Errorf("missing profile %q in list", spec1.Name)
	}
	if !names[spec2.Name] {
		t.Errorf("missing profile %q in list", spec2.Name)
	}
}

func TestStore_Delete_RemovesYAMLAndSQLite(t *testing.T) {
	s := newStore(t)
	spec := sampleSpec("test-profile")

	if err := s.Create(spec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.Delete(spec.Name); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// YAML file should be gone.
	// We use Get which reads the file — it should fail.
	_, err := s.Get(spec.Name)
	if err == nil {
		t.Error("Get after Delete: expected error, got nil")
	}

	// Also confirm the file is truly absent via os.Stat.
	// We can't access profilePath directly, but Get failing is sufficient.
	// Additional check: List should return empty.
	all, err := s.List()
	if err != nil {
		t.Fatalf("List after Delete: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected empty list after delete, got %v", all)
	}
}

func TestStore_Create_FailsOnDuplicate(t *testing.T) {
	store := newStore(t)
	spec := sampleSpec("dup-profile")

	if err := store.Create(spec); err != nil {
		t.Fatalf("first Create failed: %v", err)
	}

	err := store.Create(spec)
	if err == nil {
		t.Fatal("expected error on duplicate Create, got nil")
	}

	// Original profile must still be intact
	got, err := store.Get(spec.Name)
	if err != nil {
		t.Fatalf("Get after failed duplicate Create: %v", err)
	}
	if !reflect.DeepEqual(spec, got) {
		t.Errorf("original profile corrupted after duplicate Create attempt")
	}
}
