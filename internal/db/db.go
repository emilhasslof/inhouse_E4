package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// DB wraps the SQLite connection and exposes all query methods.
type DB struct {
	conn *sql.DB
}

// Open opens (or creates) the SQLite database at path and runs migrations.
// If archivePath is non-empty, a second SQLite file is opened, migrated to the
// same schema, then ATTACHed to the main connection as `arc`. Cross-database
// statements (e.g. `INSERT INTO arc.matches SELECT * FROM matches WHERE …`)
// can then be issued via the same *DB. Pass an empty archivePath in tests or
// when no archive is required.
// Parent directories are created automatically if they don't exist.
func Open(path, archivePath string) (*DB, error) {
	// Migrate the archive file standalone first. This keeps the schema bootstrap
	// for both files in one place and avoids ambiguity from running CREATE TABLE
	// against an attached database.
	if archivePath != "" {
		arc, err := openSingle(archivePath)
		if err != nil {
			return nil, fmt.Errorf("open archive db: %w", err)
		}
		if err := arc.migrate(); err != nil {
			arc.Close()
			return nil, fmt.Errorf("migrate archive db: %w", err)
		}
		arc.Close()
	}

	main, err := openSingle(path)
	if err != nil {
		return nil, err
	}
	if err := main.migrate(); err != nil {
		main.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	if archivePath != "" {
		if _, err := main.conn.Exec(`ATTACH DATABASE ? AS arc`, archivePath); err != nil {
			main.Close()
			return nil, fmt.Errorf("attach archive db: %w", err)
		}
	}
	return main, nil
}

// openSingle opens one SQLite file with the connection pragmas we always want.
// Does not run migrations — the caller decides when (and whether) to migrate.
func openSingle(path string) (*DB, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

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
	return &DB{conn: conn}, nil
}

// HasArchive reports whether this DB has an attached archive database.
func (db *DB) HasArchive() bool {
	var name string
	err := db.conn.QueryRow(`SELECT name FROM pragma_database_list WHERE name = 'arc'`).Scan(&name)
	return err == nil
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
	if _, err := db.conn.Exec(schemaSQL); err != nil {
		return err
	}
	// Additive column migrations — safe to re-run (ignored if column already exists).
	additiveMigrations := []string{
		`ALTER TABLE matches ADD COLUMN win_team TEXT NOT NULL DEFAULT ''`,
	}
	for _, stmt := range additiveMigrations {
		if _, err := db.conn.Exec(stmt); err != nil {
			// SQLite returns an error when the column already exists; ignore it.
			if !isDuplicateColumnError(err) {
				return fmt.Errorf("migration %q: %w", stmt, err)
			}
		}
	}
	return nil
}

// isDuplicateColumnError reports whether err is SQLite's "duplicate column name" error,
// which is the expected result when an ALTER TABLE ADD COLUMN migration has already run.
func isDuplicateColumnError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column name")
}
