// Package profile provides a profile store backed by YAML files and SQLite.
package profile

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/duck-labs/agentsdx-shared/types"
	"gopkg.in/yaml.v3"
)

// Store manages profiles using YAML files as the source of truth and SQLite for listing.
type Store struct {
	db          *sql.DB
	profilesDir string
}

// NewStore creates a new Store.
func NewStore(db *sql.DB, profilesDir string) *Store {
	return &Store{db: db, profilesDir: profilesDir}
}

// profilePath returns the path to the YAML file for a profile.
func (s *Store) profilePath(name string) string {
	return filepath.Join(s.profilesDir, name+".yaml")
}

// Create marshals the spec to YAML, writes it to disk, then inserts a row into SQLite.
func (s *Store) Create(spec types.ProfileSpec) error {
	path := s.profilePath(spec.Name)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("profile %q already exists", spec.Name)
	}

	data, err := yaml.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshal profile spec: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write profile yaml: %w", err)
	}

	_, err = s.db.Exec(`INSERT INTO profiles (name) VALUES (?)`, spec.Name)
	if err != nil {
		// Best-effort cleanup of the YAML file.
		_ = os.Remove(path)
		return fmt.Errorf("insert profile into sqlite: %w", err)
	}

	return nil
}

// Get reads the YAML file for the named profile and unmarshals it into a ProfileSpec.
func (s *Store) Get(name string) (types.ProfileSpec, error) {
	data, err := os.ReadFile(s.profilePath(name))
	if err != nil {
		return types.ProfileSpec{}, fmt.Errorf("read profile yaml: %w", err)
	}

	var spec types.ProfileSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return types.ProfileSpec{}, fmt.Errorf("unmarshal profile spec: %w", err)
	}

	return spec, nil
}

// List queries SQLite for all profile names, then reads each YAML file.
func (s *Store) List() ([]types.ProfileSpec, error) {
	rows, err := s.db.Query(`SELECT name FROM profiles ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query profiles: %w", err)
	}
	defer rows.Close()

	specs := make([]types.ProfileSpec, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan profile name: %w", err)
		}
		spec, err := s.Get(name)
		if err != nil {
			return nil, fmt.Errorf("get profile %q: %w", name, err)
		}
		specs = append(specs, spec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate profiles: %w", err)
	}

	return specs, nil
}

// Delete removes the YAML file and SQLite row for the named profile.
func (s *Store) Delete(name string) error {
	if err := os.Remove(s.profilePath(name)); err != nil {
		return fmt.Errorf("delete profile yaml: %w", err)
	}

	if _, err := s.db.Exec("DELETE FROM profiles WHERE name = ?", name); err != nil {
		return fmt.Errorf("delete profile from db: %w", err)
	}

	return nil
}
