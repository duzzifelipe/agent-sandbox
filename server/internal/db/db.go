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
	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return conn, nil
}

func migrate(conn *sql.DB) error {
	_, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS profiles (
			name       TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS sessions (
			id           TEXT PRIMARY KEY,
			profile_name TEXT NOT NULL,
			state        TEXT NOT NULL,
			ip_address   TEXT,
			created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (profile_name) REFERENCES profiles(name)
		);

		CREATE TABLE IF NOT EXISTS images (
			profile_name TEXT PRIMARY KEY,
			virtualbox   TEXT NOT NULL DEFAULT '',
			hetzner      TEXT NOT NULL DEFAULT '',
			built_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (profile_name) REFERENCES profiles(name)
		);
	`)
	return err
}
