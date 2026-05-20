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
        created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
    )`); err != nil {
		return fmt.Errorf("create profiles table: %w", err)
	}

	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS sessions (
        id TEXT PRIMARY KEY,
        profile_name TEXT NOT NULL,
        state TEXT NOT NULL,
        ip_address TEXT,
        vm_id TEXT NOT NULL DEFAULT '',
        created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY (profile_name) REFERENCES profiles(name)
    )`); err != nil {
		return fmt.Errorf("create sessions table: %w", err)
	}

	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS images (
        profile_name TEXT PRIMARY KEY,
        qemu TEXT NOT NULL DEFAULT '',
        hetzner TEXT NOT NULL DEFAULT '',
        built_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY (profile_name) REFERENCES profiles(name)
    )`); err != nil {
		return fmt.Errorf("create images table: %w", err)
	}

	return tx.Commit()
}
