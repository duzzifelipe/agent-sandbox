package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/duck-labs/agentsdx/internal/types"
)

// Store manages profiles as YAML files in a directory.
type Store struct {
	dir string
}

// NewStore creates a Store backed by dir.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) profilePath(name string) string {
	return filepath.Join(s.dir, name+".yaml")
}

func (s *Store) Create(spec types.ProfileSpec) error {
	if _, err := os.Stat(s.profilePath(spec.Name)); err == nil {
		return fmt.Errorf("profile %q already exists", spec.Name)
	}
	return s.write(spec)
}

func (s *Store) Get(name string) (types.ProfileSpec, error) {
	data, err := os.ReadFile(s.profilePath(name))
	if err != nil {
		if os.IsNotExist(err) {
			return types.ProfileSpec{}, fmt.Errorf("profile %q not found", name)
		}
		return types.ProfileSpec{}, fmt.Errorf("read profile: %w", err)
	}
	var spec types.ProfileSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return types.ProfileSpec{}, fmt.Errorf("unmarshal profile: %w", err)
	}
	return spec, nil
}

func (s *Store) List() ([]types.ProfileSpec, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read profiles dir: %w", err)
	}
	var specs []types.ProfileSpec
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		spec, err := s.Get(name)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func (s *Store) Delete(name string) error {
	if err := os.Remove(s.profilePath(name)); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("profile %q not found", name)
		}
		return fmt.Errorf("remove profile: %w", err)
	}
	return nil
}

func (s *Store) AddProject(name string, proj types.ProjectConfig) error {
	spec, err := s.Get(name)
	if err != nil {
		return err
	}
	spec.Projects = append(spec.Projects, proj)
	return s.write(spec)
}

func (s *Store) write(spec types.ProfileSpec) error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("create profiles dir: %w", err)
	}
	data, err := yaml.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}
	return os.WriteFile(s.profilePath(spec.Name), data, 0600)
}
