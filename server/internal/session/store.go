package session

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"github.com/duck-labs/agentsdx-shared/types"
)

// Store is a thin SQLite CRUD wrapper for the sessions table.
type Store struct {
	db *sql.DB
}

// NewStore creates a Store backed by the given database connection.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// DB exposes the underlying connection (used in tests to set up FK dependencies).
func (s *Store) DB() *sql.DB { return s.db }

// SessionRecord is a row from the sessions table.
type SessionRecord struct {
	ID        string
	Profile   string
	State     string
	IPAddress string
	SSHPort   int
	VMID      string
}

// Create inserts a new session in pending state and returns the generated ID.
func (s *Store) Create(profileName string) (string, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, profile_name, state) VALUES (?, ?, ?)`,
		id, profileName, types.SessionStatePending,
	)
	if err != nil {
		return "", fmt.Errorf("insert session: %w", err)
	}
	return id, nil
}

// Get returns the session record for the given ID.
func (s *Store) Get(id string) (SessionRecord, error) {
	var rec SessionRecord
	err := s.db.QueryRow(
		`SELECT id, profile_name, state, COALESCE(ip_address, ''), ssh_port, COALESCE(vm_id, '') FROM sessions WHERE id = ?`, id,
	).Scan(&rec.ID, &rec.Profile, &rec.State, &rec.IPAddress, &rec.SSHPort, &rec.VMID)
	if err == sql.ErrNoRows {
		return rec, fmt.Errorf("session %q not found", id)
	}
	if err != nil {
		return rec, fmt.Errorf("query session: %w", err)
	}
	return rec, nil
}

// UpdateVMID sets the vm_id of a session.
func (s *Store) UpdateVMID(id, vmID string) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET vm_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		vmID, id,
	)
	if err != nil {
		return fmt.Errorf("update session vm_id: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session %q not found", id)
	}
	return nil
}

// ListActive returns all session records in running or starting state.
func (s *Store) ListActive() ([]SessionRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, profile_name, state, COALESCE(ip_address, ''), ssh_port, COALESCE(vm_id, '') FROM sessions WHERE state IN ('running', 'starting')`,
	)
	if err != nil {
		return nil, fmt.Errorf("query active sessions: %w", err)
	}
	defer rows.Close()

	var recs []SessionRecord
	for rows.Next() {
		var rec SessionRecord
		if err := rows.Scan(&rec.ID, &rec.Profile, &rec.State, &rec.IPAddress, &rec.SSHPort, &rec.VMID); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		recs = append(recs, rec)
	}
	return recs, rows.Err()
}

// UpdateState sets the state, ip_address, and ssh_port of a session.
func (s *Store) UpdateState(id, state, ipAddress string, sshPort int) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET state = ?, ip_address = ?, ssh_port = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		state, ipAddress, sshPort, id,
	)
	if err != nil {
		return fmt.Errorf("update session state: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session %q not found", id)
	}
	return nil
}
