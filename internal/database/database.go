// Package database provides SQLite persistence for github-radar.
// It replaces the JSON state file with a relational database using
// modernc.org/sqlite (pure Go, no CGo).
package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

// DefaultDBPath is the default path for the SQLite database.
var DefaultDBPath = defaultDBPath()

func defaultDBPath() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "github-radar", "scanner.db")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "data/scanner.db"
	}
	return filepath.Join(home, ".local", "share", "github-radar", "scanner.db")
}

// DB wraps a SQLite database connection with thread-safe operations.
type DB struct {
	mu   sync.RWMutex
	db   *sql.DB
	path string
}

// Open opens or creates a SQLite database at the given path.
// It creates the directory, enables WAL mode, and initializes the schema.
func Open(path string) (*DB, error) {
	if path == "" {
		path = DefaultDBPath
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating database directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Enable WAL mode for concurrent reader support
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling WAL mode: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	d := &DB{db: db, path: path}

	if err := d.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("initializing schema: %w", err)
	}

	return d, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// Path returns the database file path.
func (d *DB) Path() string {
	return d.path
}

// initSchema creates the repos table and indexes if they don't exist, then
// runs any forward-only migrations.
// Base schema: Story 10.1 (repos table) + Story 10.2 (classification columns).
// Migration v2 (ISI-744): drops empty persisted description/topics columns; the
// classifier live-fetches those from the GitHub API at classification time.
func (d *DB) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS repos (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		full_name       TEXT    NOT NULL UNIQUE,
		owner           TEXT    NOT NULL,
		name            TEXT    NOT NULL,
		language        TEXT    NOT NULL DEFAULT '',
		stars           INTEGER NOT NULL DEFAULT 0,
		stars_prev      INTEGER NOT NULL DEFAULT 0,
		forks           INTEGER NOT NULL DEFAULT 0,
		open_issues     INTEGER NOT NULL DEFAULT 0,
		open_prs        INTEGER NOT NULL DEFAULT 0,
		contributors    INTEGER NOT NULL DEFAULT 0,
		contributors_prev INTEGER NOT NULL DEFAULT 0,
		growth_score    REAL    NOT NULL DEFAULT 0,
		normalized_growth_score REAL NOT NULL DEFAULT 0,
		star_velocity   REAL    NOT NULL DEFAULT 0,
		star_acceleration REAL  NOT NULL DEFAULT 0,
		pr_velocity     REAL    NOT NULL DEFAULT 0,
		issue_velocity  REAL    NOT NULL DEFAULT 0,
		contributor_growth REAL NOT NULL DEFAULT 0,
		merged_prs_7d   INTEGER NOT NULL DEFAULT 0,
		new_issues_7d   INTEGER NOT NULL DEFAULT 0,
		latest_release  TEXT    NOT NULL DEFAULT '',
		latest_release_date TEXT NOT NULL DEFAULT '',
		created_at      TEXT    NOT NULL DEFAULT '',
		first_seen_at   TEXT    NOT NULL DEFAULT (datetime('now')),
		last_collected_at TEXT  NOT NULL DEFAULT '',
		status          TEXT    NOT NULL DEFAULT 'pending',
		etag            TEXT    NOT NULL DEFAULT '',
		last_modified   TEXT    NOT NULL DEFAULT '',

		-- Classification columns (Story 10.2)
		primary_category    TEXT    NOT NULL DEFAULT '',
		category_confidence REAL    NOT NULL DEFAULT 0,
		readme_hash         TEXT    NOT NULL DEFAULT '',
		classified_at       TEXT    NOT NULL DEFAULT '',
		model_used          TEXT    NOT NULL DEFAULT '',
		force_category      TEXT    NOT NULL DEFAULT '',
		excluded            INTEGER NOT NULL DEFAULT 0
	);

	-- Indexes for efficient filtering (Story 10.2)
	CREATE INDEX IF NOT EXISTS idx_repos_primary_category ON repos(primary_category);
	CREATE INDEX IF NOT EXISTS idx_repos_status ON repos(status);
	CREATE INDEX IF NOT EXISTS idx_repos_excluded ON repos(excluded);

	-- Metadata table for schema version and scan state
	CREATE TABLE IF NOT EXISTS metadata (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	-- Discovery state
	CREATE TABLE IF NOT EXISTS discovery_known_repos (
		full_name TEXT PRIMARY KEY
	);

	CREATE TABLE IF NOT EXISTS discovery_topic_scans (
		topic    TEXT PRIMARY KEY,
		scanned_at TEXT NOT NULL
	);
	`

	if _, err := d.db.Exec(schema); err != nil {
		return fmt.Errorf("creating schema: %w", err)
	}

	// Set schema version if not present
	_, err := d.db.Exec(
		`INSERT OR IGNORE INTO metadata (key, value) VALUES ('schema_version', '1')`,
	)
	if err != nil {
		return fmt.Errorf("setting schema version: %w", err)
	}

	if err := d.migrateDropDescriptionTopics(); err != nil {
		return fmt.Errorf("migration (drop description/topics): %w", err)
	}

	return nil
}

// migrateDropDescriptionTopics drops the empty `description` and `topics`
// columns from an existing `repos` table (ISI-744). A live audit of the
// production scanner.db found these were empty strings for 100% of 559 active
// repos — the classifier already consumes live values from the GitHub API, so
// persisting them added nothing but misleading shape. Forward-only: once a DB
// is at schema_version >= 2 the migration is a no-op.
//
// SQLite 3.35+ supports `ALTER TABLE ... DROP COLUMN` (modernc.org/sqlite
// v1.48.1 bundles SQLite 3.45+), so this runs as a single transaction.
func (d *DB) migrateDropDescriptionTopics() error {
	var version string
	err := d.db.QueryRow(
		`SELECT value FROM metadata WHERE key = 'schema_version'`,
	).Scan(&version)
	if err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}
	if version != "" && version != "1" {
		return nil // already migrated
	}

	has, err := d.columnsExist("repos", "description", "topics")
	if err != nil {
		return err
	}

	if has["description"] || has["topics"] {
		tx, err := d.db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration: %w", err)
		}
		if has["description"] {
			if _, err := tx.Exec(`ALTER TABLE repos DROP COLUMN description`); err != nil {
				tx.Rollback()
				return fmt.Errorf("dropping description: %w", err)
			}
		}
		if has["topics"] {
			if _, err := tx.Exec(`ALTER TABLE repos DROP COLUMN topics`); err != nil {
				tx.Rollback()
				return fmt.Errorf("dropping topics: %w", err)
			}
		}
		if _, err := tx.Exec(
			`UPDATE metadata SET value = '2' WHERE key = 'schema_version'`,
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("bumping schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration: %w", err)
		}
		return nil
	}

	// Columns already absent (fresh DB): bump version outside a tx.
	if _, err := d.db.Exec(
		`UPDATE metadata SET value = '2' WHERE key = 'schema_version'`,
	); err != nil {
		return fmt.Errorf("bumping schema version: %w", err)
	}
	return nil
}

// columnsExist returns a map of column name → present for the given table.
func (d *DB) columnsExist(table string, names ...string) (map[string]bool, error) {
	rows, err := d.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, fmt.Errorf("pragma table_info(%s): %w", table, err)
	}
	defer rows.Close()

	present := make(map[string]bool, len(names))
	for _, n := range names {
		present[n] = false
	}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("scanning table_info row: %w", err)
		}
		if _, ok := present[name]; ok {
			present[name] = true
		}
	}
	return present, rows.Err()
}
