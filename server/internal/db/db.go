// Package db opens and migrates the SQLite database for agentsdx-server.
package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

func Open(path string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return conn, nil
}

func migrate(conn *sql.DB) error {
	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS profiles (
        name TEXT PRIMARY KEY,
        spec TEXT NOT NULL DEFAULT '',
        created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
    )`); err != nil {
		return fmt.Errorf("create profiles table: %w", err)
	}
	// Add spec column to existing databases (safe no-op on new DBs).
	_, _ = tx.Exec(`ALTER TABLE profiles ADD COLUMN spec TEXT NOT NULL DEFAULT ''`)

	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS sessions (
        id TEXT PRIMARY KEY,
        profile_name TEXT NOT NULL,
        state TEXT NOT NULL,
        ip_address TEXT,
        ssh_port INTEGER NOT NULL DEFAULT 0,
        vm_id TEXT NOT NULL DEFAULT '',
        created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY (profile_name) REFERENCES profiles(name)
    )`); err != nil {
		return fmt.Errorf("create sessions table: %w", err)
	}
	// Add ssh_port to existing databases that predate this column (safe no-op on new DBs).
	_, _ = tx.Exec(`ALTER TABLE sessions ADD COLUMN ssh_port INTEGER NOT NULL DEFAULT 0`)

	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS images (
        profile_name TEXT PRIMARY KEY,
        hetzner TEXT NOT NULL DEFAULT '',
        built_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY (profile_name) REFERENCES profiles(name)
    )`); err != nil {
		return fmt.Errorf("create images table: %w", err)
	}

	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS qemu_vms (
        id            TEXT PRIMARY KEY,
        pid           INTEGER NOT NULL,
        ssh_port      INTEGER NOT NULL,
        overlay_path  TEXT NOT NULL,
        seed_iso_path TEXT NOT NULL,
        created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
    )`); err != nil {
		return fmt.Errorf("create qemu_vms table: %w", err)
	}

	return tx.Commit()
}
