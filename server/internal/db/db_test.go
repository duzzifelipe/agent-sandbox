package db_test

import (
	"path/filepath"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/db"
)

func TestOpen_CreatesAllTables(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()

	for _, table := range []string{"profiles", "sessions", "images", "qemu_vms"} {
		var count int
		err := conn.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&count)
		if err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("table %s not created", table)
		}
	}
}

func TestOpen_MigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	for i := 0; i < 3; i++ {
		conn, err := db.Open(path)
		if err != nil {
			t.Fatalf("Open (run %d): %v", i+1, err)
		}
		conn.Close()
	}
}

func TestOpen_InMemory(t *testing.T) {
	conn, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open :memory:: %v", err)
	}
	defer conn.Close()

	for _, table := range []string{"profiles", "sessions", "images", "qemu_vms"} {
		var count int
		err := conn.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&count)
		if err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("table %s not created", table)
		}
	}
}
