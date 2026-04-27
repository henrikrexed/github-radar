package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// buildV1DB creates a v1-shaped scanner.db on disk: a repos table with the
// pre-migration columns, a metadata table with schema_version='1', and the
// caller-provided rows. This simulates an existing production DB that must
// be migrated forward.
func buildV1DB(t *testing.T, path string, legacyRows []struct {
	fullName        string
	primaryCategory string
	excluded        int
}) {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	// The v1 schema is a minimal subset of what initSchema() writes — we
	// only need the columns the migration touches plus enough to satisfy
	// NOT NULL constraints.
	if _, err := raw.Exec(`
		CREATE TABLE repos (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			full_name           TEXT    NOT NULL UNIQUE,
			owner               TEXT    NOT NULL DEFAULT '',
			name                TEXT    NOT NULL DEFAULT '',
			language            TEXT    NOT NULL DEFAULT '',
			description         TEXT    NOT NULL DEFAULT '',
			stars               INTEGER NOT NULL DEFAULT 0,
			stars_prev          INTEGER NOT NULL DEFAULT 0,
			forks               INTEGER NOT NULL DEFAULT 0,
			open_issues         INTEGER NOT NULL DEFAULT 0,
			open_prs            INTEGER NOT NULL DEFAULT 0,
			contributors        INTEGER NOT NULL DEFAULT 0,
			contributors_prev   INTEGER NOT NULL DEFAULT 0,
			growth_score        REAL    NOT NULL DEFAULT 0,
			normalized_growth_score REAL NOT NULL DEFAULT 0,
			star_velocity       REAL    NOT NULL DEFAULT 0,
			star_acceleration   REAL    NOT NULL DEFAULT 0,
			pr_velocity         REAL    NOT NULL DEFAULT 0,
			issue_velocity      REAL    NOT NULL DEFAULT 0,
			contributor_growth  REAL    NOT NULL DEFAULT 0,
			merged_prs_7d       INTEGER NOT NULL DEFAULT 0,
			new_issues_7d       INTEGER NOT NULL DEFAULT 0,
			latest_release      TEXT    NOT NULL DEFAULT '',
			latest_release_date TEXT    NOT NULL DEFAULT '',
			created_at          TEXT    NOT NULL DEFAULT '',
			first_seen_at       TEXT    NOT NULL DEFAULT (datetime('now')),
			last_collected_at   TEXT    NOT NULL DEFAULT '',
			topics              TEXT    NOT NULL DEFAULT '',
			status              TEXT    NOT NULL DEFAULT 'pending',
			etag                TEXT    NOT NULL DEFAULT '',
			last_modified       TEXT    NOT NULL DEFAULT '',
			primary_category    TEXT    NOT NULL DEFAULT '',
			category_confidence REAL    NOT NULL DEFAULT 0,
			readme_hash         TEXT    NOT NULL DEFAULT '',
			classified_at       TEXT    NOT NULL DEFAULT '',
			model_used          TEXT    NOT NULL DEFAULT '',
			force_category      TEXT    NOT NULL DEFAULT '',
			excluded            INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE metadata (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		INSERT INTO metadata (key, value) VALUES ('schema_version', '1');
	`); err != nil {
		t.Fatalf("create v1 schema: %v", err)
	}
	for _, r := range legacyRows {
		if _, err := raw.Exec(
			`INSERT INTO repos (full_name, owner, name, primary_category, excluded) VALUES (?, ?, ?, ?, ?)`,
			r.fullName, "owner", r.fullName, r.primaryCategory, r.excluded,
		); err != nil {
			t.Fatalf("insert row %s: %v", r.fullName, err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}
}

func TestMigrateToTaxonomyV3_FreshDB_BumpsVersion(t *testing.T) {
	db := mustOpen(t) // Open() runs initSchema + migrations on a fresh DB.
	got, err := db.GetMetadata("schema_version")
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if got != SchemaVersionCurrent {
		t.Errorf("schema_version after fresh Open = %q, want %q", got, SchemaVersionCurrent)
	}
}

func TestMigrateToTaxonomyV3_FreshDB_AddsColumns(t *testing.T) {
	db := mustOpen(t)
	rows, err := db.db.Query(`PRAGMA table_info(repos)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()
	cols := make(map[string]struct{})
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
			t.Fatalf("scan: %v", err)
		}
		cols[name] = struct{}{}
	}
	for _, want := range []string{
		"primary_subcategory",
		"is_curated_list",
		"needs_review",
		"primary_category_legacy",
		"force_subcategory",
		"classification_override_reason",
		"classification_refusal_reason",
	} {
		if _, ok := cols[want]; !ok {
			t.Errorf("column %q missing after migration", want)
		}
	}
	// description and topics must be gone (ISI-744, folded into v3).
	for _, gone := range []string{"description", "topics"} {
		if _, present := cols[gone]; present {
			t.Errorf("column %q should have been dropped by v3 migration", gone)
		}
	}
}

func TestMigrateToTaxonomyV3_FreshDB_CreatesLegacyView(t *testing.T) {
	db := mustOpen(t)
	var name string
	err := db.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'view' AND name = 'repos_legacy_v1'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("repos_legacy_v1 view not found: %v", err)
	}
}

func TestMigrateToTaxonomyV3_V1DB_BackfillsCategoryAndSubcategory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scanner.db")

	// Two known-good mappings + one legacy value that must land in the
	// refusal sink with needs_review=1.
	buildV1DB(t, path, []struct {
		fullName        string
		primaryCategory string
		excluded        int
	}{
		{"owner/ai-repo", "ai-agents", 0},
		{"owner/k8s-repo", "kubernetes", 0},
		{"owner/orphan", "other", 0},
		{"owner/excluded-repo", "cybersecurity", 1}, // excluded row
	})

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open after building v1 DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Version must have moved to 2.
	version, err := db.GetMetadata("schema_version")
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if version != SchemaVersionCurrent {
		t.Fatalf("schema_version after migration = %q, want %q", version, SchemaVersionCurrent)
	}

	type row struct {
		cat, sub, legacy, refusal string
		needsReview               int
	}
	queryRow := func(fullName string) row {
		t.Helper()
		var r row
		err := db.db.QueryRow(
			`SELECT primary_category, primary_subcategory, primary_category_legacy,
			        classification_refusal_reason, needs_review
			 FROM repos WHERE full_name = ?`,
			fullName,
		).Scan(&r.cat, &r.sub, &r.legacy, &r.refusal, &r.needsReview)
		if err != nil {
			t.Fatalf("query %s: %v", fullName, err)
		}
		return r
	}

	// Known mapping: ai-agents → (ai, agents), needs_review unchanged (0).
	got := queryRow("owner/ai-repo")
	if got.cat != "ai" || got.sub != "agents" {
		t.Errorf("ai-agents backfill = (%s, %s), want (ai, agents)", got.cat, got.sub)
	}
	if got.legacy != "ai-agents" {
		t.Errorf("ai-agents legacy snapshot = %q, want ai-agents", got.legacy)
	}
	if got.needsReview != 0 {
		t.Errorf("ai-agents needs_review = %d, want 0", got.needsReview)
	}

	// Known mapping: kubernetes → (cloud-native, kubernetes).
	got = queryRow("owner/k8s-repo")
	if got.cat != "cloud-native" || got.sub != "kubernetes" {
		t.Errorf("kubernetes backfill = (%s, %s), want (cloud-native, kubernetes)", got.cat, got.sub)
	}

	// "other" flat value lands in the refusal sink with needs_review=1.
	got = queryRow("owner/orphan")
	if got.cat != "other" || got.sub != "other" {
		t.Errorf("other backfill = (%s, %s), want (other, other)", got.cat, got.sub)
	}
	if got.needsReview != 1 {
		t.Errorf("other needs_review = %d, want 1", got.needsReview)
	}
	if got.refusal != "backfill_legacy_other" {
		t.Errorf("other refusal_reason = %q, want backfill_legacy_other", got.refusal)
	}

	// Excluded rows still get backfilled (mapping is deterministic from
	// legacy value; excluded just means the row is hidden from scoring).
	got = queryRow("owner/excluded-repo")
	if got.cat != "security" || got.sub != "cybersecurity" {
		t.Errorf("excluded row backfill = (%s, %s), want (security, cybersecurity)", got.cat, got.sub)
	}
}

func TestMigrateToTaxonomyV3_V1DB_WritesBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scanner.db")
	buildV1DB(t, path, []struct {
		fullName        string
		primaryCategory string
		excluded        int
	}{
		{"owner/repo-a", "ai-agents", 0},
	})

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	backup := path + ".preTaxonomy.bak"
	info, err := os.Stat(backup)
	if err != nil {
		t.Fatalf("backup not written: %v", err)
	}
	if info.Size() == 0 {
		t.Errorf("backup file is empty")
	}
}

func TestMigrateToTaxonomyV3_LegacyView_RoundtripsOriginalValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scanner.db")
	// frontend-ui is the canonical round-trip counter-example from
	// architecture §2.3 — `web-frontend-ui` joined form would NOT match
	// `WHERE legacy_category='frontend-ui'`.
	buildV1DB(t, path, []struct {
		fullName        string
		primaryCategory string
		excluded        int
	}{
		{"owner/web-app", "frontend-ui", 0},
	})
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	var legacy string
	if err := db.db.QueryRow(
		`SELECT legacy_category FROM repos_legacy_v1 WHERE full_name = 'owner/web-app'`,
	).Scan(&legacy); err != nil {
		t.Fatalf("query legacy view: %v", err)
	}
	if legacy != "frontend-ui" {
		t.Errorf("legacy_category via repos_legacy_v1 = %q, want frontend-ui", legacy)
	}
}

func TestMigrateToTaxonomyV3_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scanner.db")
	buildV1DB(t, path, []struct {
		fullName        string
		primaryCategory string
		excluded        int
	}{
		{"owner/a", "ai-agents", 0},
	})
	// First open performs the migration.
	db, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	db.Close()
	// Second open must be a no-op (schema version already at current).
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	t.Cleanup(func() { db2.Close() })

	v, _ := db2.GetMetadata("schema_version")
	if v != SchemaVersionCurrent {
		t.Errorf("schema_version = %q, want %q", v, SchemaVersionCurrent)
	}

	// Row still correctly migrated.
	var cat, sub string
	if err := db2.db.QueryRow(
		`SELECT primary_category, primary_subcategory FROM repos WHERE full_name = 'owner/a'`,
	).Scan(&cat, &sub); err != nil {
		t.Fatalf("query: %v", err)
	}
	if cat != "ai" || sub != "agents" {
		t.Errorf("after second open: (%s, %s), want (ai, agents)", cat, sub)
	}
}

func TestMigrateToTaxonomyV3_CompositeIndexExists(t *testing.T) {
	db := mustOpen(t)
	var name string
	if err := db.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'idx_repos_cat_subcat'`,
	).Scan(&name); err != nil {
		t.Fatalf("idx_repos_cat_subcat not created: %v", err)
	}
}

func TestBuildBackfillSQL_DeterministicAndReferencesAllLegacyValues(t *testing.T) {
	sql1, err := buildBackfillSQL()
	if err != nil {
		t.Fatalf("buildBackfillSQL: %v", err)
	}
	sql2, err := buildBackfillSQL()
	if err != nil {
		t.Fatalf("buildBackfillSQL second call: %v", err)
	}
	if sql1 != sql2 {
		t.Errorf("buildBackfillSQL is non-deterministic between calls")
	}
	for legacy := range LegacyCategoryMap {
		if !containsQuoted(sql1, legacy) {
			t.Errorf("SQL missing WHEN clause for legacy value %q", legacy)
		}
	}
}

func containsQuoted(haystack, needle string) bool {
	q := "'" + needle + "'"
	for i := 0; i+len(q) <= len(haystack); i++ {
		if haystack[i:i+len(q)] == q {
			return true
		}
	}
	return false
}

// buildV2DB writes a v2-shaped scanner.db on disk: the post-ISI-744 layout
// with `description` and `topics` already absent and schema_version='2',
// but no taxonomy v2 columns yet. This is the actual production WAL-replayed
// state the v3 migration must recover from.
func buildV2DB(t *testing.T, path string, legacyRows []struct {
	fullName        string
	primaryCategory string
	excluded        int
}) {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	if _, err := raw.Exec(`
		CREATE TABLE repos (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			full_name           TEXT    NOT NULL UNIQUE,
			owner               TEXT    NOT NULL DEFAULT '',
			name                TEXT    NOT NULL DEFAULT '',
			language            TEXT    NOT NULL DEFAULT '',
			stars               INTEGER NOT NULL DEFAULT 0,
			stars_prev          INTEGER NOT NULL DEFAULT 0,
			forks               INTEGER NOT NULL DEFAULT 0,
			open_issues         INTEGER NOT NULL DEFAULT 0,
			open_prs            INTEGER NOT NULL DEFAULT 0,
			contributors        INTEGER NOT NULL DEFAULT 0,
			contributors_prev   INTEGER NOT NULL DEFAULT 0,
			growth_score        REAL    NOT NULL DEFAULT 0,
			normalized_growth_score REAL NOT NULL DEFAULT 0,
			star_velocity       REAL    NOT NULL DEFAULT 0,
			star_acceleration   REAL    NOT NULL DEFAULT 0,
			pr_velocity         REAL    NOT NULL DEFAULT 0,
			issue_velocity      REAL    NOT NULL DEFAULT 0,
			contributor_growth  REAL    NOT NULL DEFAULT 0,
			merged_prs_7d       INTEGER NOT NULL DEFAULT 0,
			new_issues_7d       INTEGER NOT NULL DEFAULT 0,
			latest_release      TEXT    NOT NULL DEFAULT '',
			latest_release_date TEXT    NOT NULL DEFAULT '',
			created_at          TEXT    NOT NULL DEFAULT '',
			first_seen_at       TEXT    NOT NULL DEFAULT (datetime('now')),
			last_collected_at   TEXT    NOT NULL DEFAULT '',
			status              TEXT    NOT NULL DEFAULT 'pending',
			etag                TEXT    NOT NULL DEFAULT '',
			last_modified       TEXT    NOT NULL DEFAULT '',
			primary_category    TEXT    NOT NULL DEFAULT '',
			category_confidence REAL    NOT NULL DEFAULT 0,
			readme_hash         TEXT    NOT NULL DEFAULT '',
			classified_at       TEXT    NOT NULL DEFAULT '',
			model_used          TEXT    NOT NULL DEFAULT '',
			force_category      TEXT    NOT NULL DEFAULT '',
			excluded            INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE metadata (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		INSERT INTO metadata (key, value) VALUES ('schema_version', '2');
	`); err != nil {
		t.Fatalf("create v2 schema: %v", err)
	}
	for _, r := range legacyRows {
		if _, err := raw.Exec(
			`INSERT INTO repos (full_name, owner, name, primary_category, excluded) VALUES (?, ?, ?, ?, ?)`,
			r.fullName, "owner", r.fullName, r.primaryCategory, r.excluded,
		); err != nil {
			t.Fatalf("insert row %s: %v", r.fullName, err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}
}

// TestMigrateToTaxonomyV3_V2DB_StartingPoint exercises the production
// WAL-replayed scenario: schema_version='2', description+topics already
// dropped, no taxonomy v2 columns yet. The v3 migration must skip the
// column-drop step and proceed directly with adding taxonomy columns and
// backfilling.
func TestMigrateToTaxonomyV3_V2DB_StartingPoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scanner.db")

	buildV2DB(t, path, []struct {
		fullName        string
		primaryCategory string
		excluded        int
	}{
		{"owner/ai-repo", "ai-agents", 0},
		{"owner/k8s-repo", "kubernetes", 0},
		{"owner/web-app", "frontend-ui", 0},
	})

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open after building v2 DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	version, err := db.GetMetadata("schema_version")
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if version != SchemaVersionCurrent {
		t.Fatalf("schema_version after v2→v3 migration = %q, want %q", version, SchemaVersionCurrent)
	}

	// description + topics must NOT have been re-added by the migration.
	rows, err := db.db.Query(`PRAGMA table_info(repos)`)
	if err != nil {
		t.Fatalf("PRAGMA: %v", err)
	}
	defer rows.Close()
	cols := make(map[string]struct{})
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
			t.Fatalf("scan: %v", err)
		}
		cols[name] = struct{}{}
	}
	for _, gone := range []string{"description", "topics"} {
		if _, present := cols[gone]; present {
			t.Errorf("column %q should remain absent on v2→v3 migration", gone)
		}
	}
	for _, want := range []string{
		"primary_subcategory",
		"primary_category_legacy",
		"is_curated_list",
		"needs_review",
		"classification_refusal_reason",
	} {
		if _, ok := cols[want]; !ok {
			t.Errorf("column %q missing after v2→v3 migration", want)
		}
	}

	// Backfill must still produce correct (cat, subcat) pairs.
	var cat, sub, legacy string
	if err := db.db.QueryRow(
		`SELECT primary_category, primary_subcategory, primary_category_legacy
		 FROM repos WHERE full_name = 'owner/ai-repo'`,
	).Scan(&cat, &sub, &legacy); err != nil {
		t.Fatalf("query ai-repo: %v", err)
	}
	if cat != "ai" || sub != "agents" {
		t.Errorf("v2→v3 ai-agents backfill = (%s, %s), want (ai, agents)", cat, sub)
	}
	if legacy != "ai-agents" {
		t.Errorf("v2→v3 ai-agents legacy snapshot = %q, want ai-agents", legacy)
	}

	// Legacy view must round-trip through preserved column.
	var rt string
	if err := db.db.QueryRow(
		`SELECT legacy_category FROM repos_legacy_v1 WHERE full_name = 'owner/web-app'`,
	).Scan(&rt); err != nil {
		t.Fatalf("query legacy view: %v", err)
	}
	if rt != "frontend-ui" {
		t.Errorf("v2→v3 legacy round-trip = %q, want frontend-ui", rt)
	}
}

// TestMigrateToTaxonomyV3_V1DB_UnclassifiedRowsLandInRefusalSink verifies
// the Blocker B fix: a v1 production row whose classifier never ran (empty
// primary_category) is no longer skipped by the backfill WHERE clause.
// It must land in (other, other) with needs_review=1 and
// classification_refusal_reason='backfill_legacy_other'. Without the fix
// the orphan guard would fail closed and block the migration.
func TestMigrateToTaxonomyV3_V1DB_UnclassifiedRowsLandInRefusalSink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scanner.db")

	buildV1DB(t, path, []struct {
		fullName        string
		primaryCategory string
		excluded        int
	}{
		{"owner/never-classified", "", 0}, // mirrors prod: empty primary_category
		{"owner/excluded-empty", "", 1},   // excluded should still backfill safely
		{"owner/known", "ai-agents", 0},   // sanity: classified row still lands cleanly
	})

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open after v1 build: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	type row struct {
		cat, sub, refusal string
		needsReview       int
	}
	queryRow := func(fullName string) row {
		t.Helper()
		var r row
		if err := db.db.QueryRow(
			`SELECT primary_category, primary_subcategory,
			        classification_refusal_reason, needs_review
			 FROM repos WHERE full_name = ?`,
			fullName,
		).Scan(&r.cat, &r.sub, &r.refusal, &r.needsReview); err != nil {
			t.Fatalf("query %s: %v", fullName, err)
		}
		return r
	}

	got := queryRow("owner/never-classified")
	if got.cat != "other" || got.sub != "other" {
		t.Errorf("never-classified backfill = (%s, %s), want (other, other)", got.cat, got.sub)
	}
	if got.needsReview != 1 {
		t.Errorf("never-classified needs_review = %d, want 1", got.needsReview)
	}
	if got.refusal != "backfill_legacy_other" {
		t.Errorf("never-classified refusal_reason = %q, want backfill_legacy_other", got.refusal)
	}

	got = queryRow("owner/excluded-empty")
	if got.cat != "other" || got.sub != "other" {
		t.Errorf("excluded-empty backfill = (%s, %s), want (other, other)", got.cat, got.sub)
	}

	got = queryRow("owner/known")
	if got.cat != "ai" || got.sub != "agents" {
		t.Errorf("known classified backfill = (%s, %s), want (ai, agents)", got.cat, got.sub)
	}
	if got.needsReview != 0 {
		t.Errorf("known classified needs_review = %d, want 0", got.needsReview)
	}
}
