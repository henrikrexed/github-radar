package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hrexed/github-radar/internal/logging"
)

// SchemaVersionCurrent is the schema version this binary expects to operate on.
//
// History:
//   - "1": original repos schema (Story 10.x).
//   - "2": ISI-744 dropped `description` and `topics` columns (production
//     state on the WAL-replayed scanner.db; never merged to main).
//   - "3": ISI-714 taxonomy v2 — adds primary_subcategory + 6 sibling
//     classification columns, primary_category_legacy snapshot, the
//     repos_legacy_v1 compat view, and the (cat, subcat) composite index.
//     The v3 migration also folds in the "2" column-drop step so a v1 DB
//     can move directly to v3 in a single transaction (per PM decision on
//     [ISI-714](/ISI/issues/ISI-714)).
const SchemaVersionCurrent = "3"

// taxonomyV2Columns are the columns added to repos by the v3 taxonomy
// migration. The slice is also used by the idempotency check in
// addTaxonomyColumns so the migration can run twice without error.
var taxonomyV2Columns = []struct {
	Name string
	DDL  string
}{
	{"primary_subcategory", "ALTER TABLE repos ADD COLUMN primary_subcategory TEXT NOT NULL DEFAULT ''"},
	{"is_curated_list", "ALTER TABLE repos ADD COLUMN is_curated_list INTEGER NOT NULL DEFAULT 0"},
	{"needs_review", "ALTER TABLE repos ADD COLUMN needs_review INTEGER NOT NULL DEFAULT 0"},
	{"primary_category_legacy", "ALTER TABLE repos ADD COLUMN primary_category_legacy TEXT NOT NULL DEFAULT ''"},
	{"force_subcategory", "ALTER TABLE repos ADD COLUMN force_subcategory TEXT NOT NULL DEFAULT ''"},
	{"classification_override_reason", "ALTER TABLE repos ADD COLUMN classification_override_reason TEXT NOT NULL DEFAULT ''"},
	{"classification_refusal_reason", "ALTER TABLE repos ADD COLUMN classification_refusal_reason TEXT NOT NULL DEFAULT ''"},
}

// runSchemaMigrations brings the open database up to SchemaVersionCurrent.
// It is idempotent: if the DB is already at the target version, it's a no-op.
// Called from Open() after initSchema.
//
// Accepted starting points:
//
//   - ""         : freshly opened DB whose metadata row was just inserted with
//     value '1' by initSchema; treated identically to "1".
//   - "1"        : original schema with `description` + `topics` columns and
//     no taxonomy columns.
//   - "2"        : ISI-744 column-drop already applied (production WAL state)
//     but no taxonomy columns yet.
//   - SchemaVersionCurrent ("3"): no-op.
//
// Anything else is operator error and aborts startup.
func (d *DB) runSchemaMigrations() error {
	version, err := d.GetMetadata("schema_version")
	if err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}
	if version == SchemaVersionCurrent {
		return nil
	}
	if version != "" && version != "1" && version != "2" {
		return fmt.Errorf("unsupported schema version %q (expected 1, 2, or %s)", version, SchemaVersionCurrent)
	}

	if err := d.migrateToTaxonomyV3(); err != nil {
		return fmt.Errorf("taxonomy v3 migration: %w", err)
	}
	return nil
}

// migrateToTaxonomyV3 performs the v1/v2 → v3 schema + data migration per
// ISI-712 §2 + ISI-744 column-drop fold-in. Steps:
//
//  1. Snapshot the DB file to <path>.preTaxonomy.bak via `VACUUM INTO`
//     (consistent snapshot, WAL-safe) — the F1 rollback path.
//  2. In a single BEGIN IMMEDIATE transaction:
//     a. Drop `description` and `topics` columns if present (ISI-744 fold-in,
//     idempotent — a v2 starting state where they're already gone is a no-op
//     for this step).
//     b. Idempotently add the 7 taxonomy columns.
//     c. Create the (category, subcategory) composite index.
//     d. Snapshot pre-migration primary_category into primary_category_legacy.
//     e. Backfill primary_category + primary_subcategory from LegacyCategoryMap.
//     f. Flag rows landing in the "other" refusal sink with needs_review=1.
//     g. Abort (ROLLBACK) if any active repo still has empty cat/subcat.
//     h. Create the repos_legacy_v1 compat view.
//     i. Bump schema_version to 3.
//
// The migration is safe to run on a fresh empty database (no rows → no-op
// backfill) but still writes the .preTaxonomy.bak file for audit symmetry.
func (d *DB) migrateToTaxonomyV3() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Step 1 — write pre-transaction backup (F1 rollback path).
	// Only attempt the file backup when we have a real on-disk path; in-memory
	// databases (":memory:", "file::memory:?cache=shared") have no file to copy.
	if d.path != "" && !strings.HasPrefix(d.path, ":") && !strings.Contains(d.path, ":memory:") {
		backupPath := d.path + ".preTaxonomy.bak"
		if err := d.vacuumIntoBackup(backupPath); err != nil {
			return fmt.Errorf("writing pre-migration backup %s: %w", backupPath, err)
		}
		logging.Info("wrote taxonomy pre-migration backup", "backup_path", backupPath)
	}

	// Step 2 — run the migration transaction.
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// 2a — drop description/topics columns if present (ISI-744 fold-in,
	// idempotent). On a v2 starting state these are already absent.
	if err := dropDescriptionTopicsColumns(tx); err != nil {
		return fmt.Errorf("dropping description/topics columns: %w", err)
	}

	// 2b — additive column DDL (idempotent).
	if err := addTaxonomyColumns(tx); err != nil {
		return fmt.Errorf("adding columns: %w", err)
	}

	// 2c — composite index for cat/subcat aggregation queries.
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_repos_cat_subcat ON repos(primary_category, primary_subcategory)`); err != nil {
		return fmt.Errorf("creating idx_repos_cat_subcat: %w", err)
	}

	// 2d — snapshot legacy flat values (only when not already snapshotted).
	if _, err := tx.Exec(`UPDATE repos SET primary_category_legacy = primary_category WHERE primary_category_legacy = ''`); err != nil {
		return fmt.Errorf("snapshotting legacy values: %w", err)
	}

	// 2e + 2f — backfill (category, subcategory) via CASE derived from
	// LegacyCategoryMap. Only rows that haven't been backfilled yet (empty
	// primary_subcategory) are touched, so the migration is re-runnable.
	backfillSQL, err := buildBackfillSQL()
	if err != nil {
		return fmt.Errorf("building backfill SQL: %w", err)
	}
	if _, err := tx.Exec(backfillSQL); err != nil {
		return fmt.Errorf("backfill: %w", err)
	}

	// 2g — orphan guard. Any active repo without a (cat, subcat) pair means
	// we have a legacy value we did not cover; fail closed.
	var orphanCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM repos WHERE excluded = 0 AND (primary_category = '' OR primary_subcategory = '')`).Scan(&orphanCount); err != nil {
		return fmt.Errorf("orphan check: %w", err)
	}
	if orphanCount > 0 {
		return fmt.Errorf("orphan check failed: %d active repos have empty (category, subcategory) after backfill", orphanCount)
	}

	// 2h — legacy-compat view. See architecture §2.3: we use the preserved
	// column, not the joined form, so `WHERE legacy_category='frontend-ui'`
	// still round-trips correctly.
	if _, err := tx.Exec(`CREATE VIEW IF NOT EXISTS repos_legacy_v1 AS SELECT *, primary_category_legacy AS legacy_category FROM repos`); err != nil {
		return fmt.Errorf("creating repos_legacy_v1: %w", err)
	}

	// 2i — bump schema version.
	if _, err := tx.Exec(`INSERT INTO metadata (key, value) VALUES ('schema_version', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, SchemaVersionCurrent); err != nil {
		return fmt.Errorf("bumping schema_version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	committed = true

	// Post-migration row-count report — surfaced in logs for PM verification.
	totals, err := d.countsByCategory()
	if err == nil {
		logging.Info("taxonomy v3 migration complete", "category_totals", totals)
	}
	return nil
}

// vacuumIntoBackup writes a consistent copy of the live database to dst using
// SQLite's `VACUUM INTO`. Safe with WAL mode and concurrent readers. The dst
// file must not already exist.
func (d *DB) vacuumIntoBackup(dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	// VACUUM INTO refuses to overwrite. If an old bak exists from a prior
	// aborted attempt, remove it first — the file is regeneratable.
	if _, err := os.Stat(dst); err == nil {
		if err := os.Remove(dst); err != nil {
			return fmt.Errorf("removing stale backup %s: %w", dst, err)
		}
	}
	if _, err := d.db.Exec(`VACUUM INTO ?`, dst); err != nil {
		return fmt.Errorf("VACUUM INTO %s: %w", dst, err)
	}
	return nil
}

// dropDescriptionTopicsColumns removes `description` and `topics` from the
// repos table when present (ISI-744). SQLite 3.35+ supports
// ALTER TABLE ... DROP COLUMN, which modernc.org/sqlite ships. The function
// is idempotent: when the columns are already absent (ISI-744 already
// applied to a production v2 DB) it is a no-op.
func dropDescriptionTopicsColumns(tx *sql.Tx) error {
	existing, err := listRepoColumns(tx)
	if err != nil {
		return err
	}
	if _, present := existing["description"]; present {
		if _, err := tx.Exec(`ALTER TABLE repos DROP COLUMN description`); err != nil {
			return fmt.Errorf("dropping description: %w", err)
		}
	}
	if _, present := existing["topics"]; present {
		if _, err := tx.Exec(`ALTER TABLE repos DROP COLUMN topics`); err != nil {
			return fmt.Errorf("dropping topics: %w", err)
		}
	}
	return nil
}

// addTaxonomyColumns runs the 7 ALTER TABLE statements, skipping any column
// that already exists on the repos table. SQLite does not support
// "ADD COLUMN IF NOT EXISTS", so we query PRAGMA table_info first.
func addTaxonomyColumns(tx *sql.Tx) error {
	existing, err := listRepoColumns(tx)
	if err != nil {
		return err
	}
	for _, c := range taxonomyV2Columns {
		if _, present := existing[c.Name]; present {
			continue
		}
		if _, err := tx.Exec(c.DDL); err != nil {
			return fmt.Errorf("adding column %s: %w", c.Name, err)
		}
	}
	return nil
}

// listRepoColumns returns the set of column names currently on the repos table.
func listRepoColumns(tx *sql.Tx) (map[string]struct{}, error) {
	rows, err := tx.Query(`PRAGMA table_info(repos)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]struct{})
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notNull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		out[name] = struct{}{}
	}
	return out, rows.Err()
}

// buildBackfillSQL emits the CASE-based UPDATE derived from LegacyCategoryMap.
// Keeping generation in Go (rather than a hand-edited SQL literal) ensures the
// migration can never drift from the coverage test — if a legacy value is
// added to the map, the SQL picks it up automatically.
//
// The WHERE clause intentionally does NOT filter on
// primary_category_legacy != "" (empty string). Newly-discovered,
// never-classified rows have primary_category set to the empty string and
// therefore land with empty primary_category_legacy; the CASE branches'
// ELSE 'other' arms catch them and route them into the (other, other) refusal
// sink with needs_review=1, matching the architect-approved behavior.
// Idempotency is preserved by the primary_subcategory = "" predicate.
func buildBackfillSQL() (string, error) {
	legacyKeys := make([]string, 0, len(LegacyCategoryMap))
	for k := range LegacyCategoryMap {
		legacyKeys = append(legacyKeys, k)
	}
	sort.Strings(legacyKeys) // deterministic SQL output for diffs + tests

	var b strings.Builder
	b.WriteString(`UPDATE repos SET
  primary_category = CASE primary_category_legacy`)
	for _, k := range legacyKeys {
		p := LegacyCategoryMap[k]
		fmt.Fprintf(&b, "\n    WHEN %s THEN %s", sqlQuote(k), sqlQuote(p.Category))
	}
	b.WriteString("\n    ELSE 'other'\n  END,\n  primary_subcategory = CASE primary_category_legacy")
	for _, k := range legacyKeys {
		p := LegacyCategoryMap[k]
		fmt.Fprintf(&b, "\n    WHEN %s THEN %s", sqlQuote(k), sqlQuote(p.Subcategory))
	}
	b.WriteString(`
    ELSE 'other'
  END,
  needs_review = CASE WHEN primary_category_legacy IN ('other','') THEN 1 ELSE needs_review END,
  classification_refusal_reason =
    CASE WHEN primary_category_legacy IN ('other','')
      THEN 'backfill_legacy_other'
      ELSE classification_refusal_reason END
WHERE primary_subcategory = ''`)
	return b.String(), nil
}

// sqlQuote returns a SQL-safe single-quoted string literal. The map keys and
// values are controlled constants in this package; this is defense-in-depth
// rather than untrusted-input escaping.
func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// countsByCategory returns a summary of how many active repos landed in each
// (category, subcategory) pair after the migration. Only used for logging.
func (d *DB) countsByCategory() (map[string]int, error) {
	rows, err := d.db.Query(`
		SELECT primary_category, COUNT(*) FROM repos
		WHERE excluded = 0
		GROUP BY primary_category
		ORDER BY primary_category`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int)
	for rows.Next() {
		var cat string
		var n int
		if err := rows.Scan(&cat, &n); err != nil {
			return nil, err
		}
		out[cat] = n
	}
	return out, rows.Err()
}
