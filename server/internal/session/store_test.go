package session_test

import (
	"path/filepath"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/db"
	"github.com/duck-labs/agentsdx-server/internal/session"
	"github.com/duck-labs/agentsdx-shared/types"
)

func newStore(t *testing.T) *session.Store {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return session.NewStore(conn)
}

func TestStore_CreateAndGet(t *testing.T) {
	store := newStore(t)
	store.DB().Exec("INSERT INTO profiles (name) VALUES (?)", "my-profile")

	id, err := store.Create("my-profile")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty session ID")
	}

	s, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if s.Profile != "my-profile" {
		t.Errorf("Profile: got %q, want %q", s.Profile, "my-profile")
	}
	if s.State != types.SessionStatePending {
		t.Errorf("State: got %q, want %q", s.State, types.SessionStatePending)
	}
}

func TestStore_UpdateState(t *testing.T) {
	store := newStore(t)
	store.DB().Exec("INSERT INTO profiles (name) VALUES (?)", "p1")

	id, _ := store.Create("p1")
	if err := store.UpdateState(id, types.SessionStateRunning, "192.168.56.10"); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}

	s, _ := store.Get(id)
	if s.State != types.SessionStateRunning {
		t.Errorf("State: got %q, want %q", s.State, types.SessionStateRunning)
	}
	if s.IPAddress != "192.168.56.10" {
		t.Errorf("IPAddress: got %q, want %q", s.IPAddress, "192.168.56.10")
	}
}

func TestStore_Get_NotFound(t *testing.T) {
	store := newStore(t)
	_, err := store.Get("nonexistent-id")
	if err == nil {
		t.Fatal("expected error for missing session")
	}
}

func TestStore_UpdateIP(t *testing.T) {
	store := newStore(t)
	store.DB().Exec("INSERT INTO profiles (name) VALUES (?)", "p1")

	id, _ := store.Create("p1")
	if err := store.UpdateIP(id, "192.168.64.5"); err != nil {
		t.Fatalf("UpdateIP: %v", err)
	}

	s, _ := store.Get(id)
	if s.IPAddress != "192.168.64.5" {
		t.Errorf("IPAddress: got %q, want %q", s.IPAddress, "192.168.64.5")
	}
}

func TestStore_UpdateIP_NotFound(t *testing.T) {
	store := newStore(t)
	err := store.UpdateIP("nonexistent", "192.168.64.5")
	if err == nil {
		t.Fatal("expected error for missing session")
	}
}
