// Package profile provides a profile store backed by SQLite.
package profile

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/duck-labs/agentsdx-shared/types"
)

// Store manages profiles using SQLite as the single source of truth.
type Store struct {
	db *sql.DB
}

// NewStore creates a new Store. The second argument is accepted but ignored for
// backwards compatibility with existing call sites that pass a profiles directory.
func NewStore(db *sql.DB, _ string) *Store {
	return &Store{db: db}
}

func (s *Store) Create(spec types.ProfileSpec) error {
	data, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshal profile spec: %w", err)
	}
	_, err = s.db.Exec(
		`INSERT INTO profiles (name, spec) VALUES (?, ?)`,
		spec.Name, string(data),
	)
	if err != nil {
		return fmt.Errorf("insert profile: %w", err)
	}
	return nil
}

func (s *Store) Get(name string) (types.ProfileSpec, error) {
	var raw string
	err := s.db.QueryRow(`SELECT spec FROM profiles WHERE name = ?`, name).Scan(&raw)
	if err == sql.ErrNoRows {
		return types.ProfileSpec{}, fmt.Errorf("profile %q not found", name)
	}
	if err != nil {
		return types.ProfileSpec{}, fmt.Errorf("query profile: %w", err)
	}
	var spec types.ProfileSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return types.ProfileSpec{}, fmt.Errorf("unmarshal profile spec: %w", err)
	}
	return spec, nil
}

func (s *Store) List() ([]types.ProfileSpec, error) {
	rows, err := s.db.Query(`SELECT spec FROM profiles ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query profiles: %w", err)
	}
	defer rows.Close()

	specs := make([]types.ProfileSpec, 0)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan profile spec: %w", err)
		}
		var spec types.ProfileSpec
		if err := json.Unmarshal([]byte(raw), &spec); err != nil {
			return nil, fmt.Errorf("unmarshal profile spec: %w", err)
		}
		specs = append(specs, spec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate profiles: %w", err)
	}
	return specs, nil
}

func (s *Store) AddProject(name string, proj types.ProjectConfig) error {
	spec, err := s.Get(name)
	if err != nil {
		return err
	}
	spec.Projects = append(spec.Projects, proj)
	data, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshal profile spec: %w", err)
	}
	_, err = s.db.Exec(`UPDATE profiles SET spec = ? WHERE name = ?`, string(data), name)
	if err != nil {
		return fmt.Errorf("update profile: %w", err)
	}
	return nil
}
