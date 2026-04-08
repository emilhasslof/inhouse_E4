package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB wraps the SQLite connection and exposes all query methods.
type DB struct {
	conn *sql.DB
}

// Open opens (or creates) the SQLite database at path and runs migrations.
// Parent directories are created automatically if they don't exist.
func Open(path string) (*DB, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Single connection: SQLite serialises all writes; WAL mode allows concurrent reads.
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)

	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// Seed inserts dev-only fake players (the 10 inhouse members with datagen tokens).
// Safe to call multiple times — uses INSERT OR IGNORE.
// Call only when APP_ENV=development.
func (db *DB) Seed() error {
	_, err := db.conn.Exec(seedSQL)
	return err
}

// SeedDevMatches inserts three completed fake matches with full player stats
// so the frontend has real-looking data to work with immediately.
// Safe to call multiple times — uses INSERT OR IGNORE.
// Call only when APP_ENV=development.
func (db *DB) SeedDevMatches() error {
	_, err := db.conn.Exec(devMatchSQL)
	return err
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) migrate() error {
	_, err := db.conn.Exec(schemaSQL)
	return err
}
