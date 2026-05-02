package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// RepoRecord represents a repository row in the database.
//
// Note: description and topics are intentionally NOT persisted (ISI-744,
// folded into the v3 taxonomy migration). They are live-fetched from the
// GitHub API at classification time, so the scanner schema stays truthful
// and avoids carrying misleading empty strings. See docs/architecture.md.
type RepoRecord struct {
	ID                    int64
	FullName              string
	Owner                 string
	Name                  string
	Language              string
	Stars                 int
	StarsPrev             int
	Forks                 int
	OpenIssues            int
	OpenPRs               int
	Contributors          int
	ContributorsPrev      int
	GrowthScore           float64
	NormalizedGrowthScore float64
	StarVelocity          float64
	StarAcceleration      float64
	PRVelocity            float64
	IssueVelocity         float64
	ContributorGrowth     float64
	MergedPRs7d           int
	NewIssues7d           int
	LatestRelease         string
	LatestReleaseDate     string
	CreatedAt             string
	FirstSeenAt           string
	LastCollectedAt       string
	Status                string
	ETag                  string
	LastModified          string

	// Classification fields
	PrimaryCategory    string
	CategoryConfidence float64
	ReadmeHash         string
	ClassifiedAt       string
	ModelUsed          string
	ForceCategory      string
	Excluded           int

	// v3 taxonomy fields (ISI-714 / ISI-786). PrimarySubcategory holds the
	// 2nd-level token (e.g. "agents") under the closed (cat, sub) matrix in
	// taxonomy_map.go. PrimaryCategoryLegacy is the pre-migration flat slug
	// (e.g. "ai-agents") preserved for the 30-day backward-compat window.
	// ForceSubcategory pairs with ForceCategory when an admin pins both
	// halves of a (category, subcategory) pair.
	PrimarySubcategory    string
	PrimaryCategoryLegacy string
	ForceSubcategory      string
}

// GetRepo returns a repository by full_name. Returns nil if not found.
func (d *DB) GetRepo(fullName string) (*RepoRecord, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	r := &RepoRecord{}
	err := d.db.QueryRow(`
		SELECT id, full_name, owner, name, language,
			stars, stars_prev, forks, open_issues, open_prs,
			contributors, contributors_prev,
			growth_score, normalized_growth_score,
			star_velocity, star_acceleration,
			pr_velocity, issue_velocity, contributor_growth,
			merged_prs_7d, new_issues_7d,
			latest_release, latest_release_date,
			created_at, first_seen_at, last_collected_at,
			status, etag, last_modified,
			primary_category, category_confidence, readme_hash,
			classified_at, model_used, force_category, excluded,
			primary_subcategory, primary_category_legacy, force_subcategory
		FROM repos WHERE full_name = ?`, fullName).Scan(
		&r.ID, &r.FullName, &r.Owner, &r.Name, &r.Language,
		&r.Stars, &r.StarsPrev, &r.Forks, &r.OpenIssues, &r.OpenPRs,
		&r.Contributors, &r.ContributorsPrev,
		&r.GrowthScore, &r.NormalizedGrowthScore,
		&r.StarVelocity, &r.StarAcceleration,
		&r.PRVelocity, &r.IssueVelocity, &r.ContributorGrowth,
		&r.MergedPRs7d, &r.NewIssues7d,
		&r.LatestRelease, &r.LatestReleaseDate,
		&r.CreatedAt, &r.FirstSeenAt, &r.LastCollectedAt,
		&r.Status, &r.ETag, &r.LastModified,
		&r.PrimaryCategory, &r.CategoryConfidence, &r.ReadmeHash,
		&r.ClassifiedAt, &r.ModelUsed, &r.ForceCategory, &r.Excluded,
		&r.PrimarySubcategory, &r.PrimaryCategoryLegacy, &r.ForceSubcategory,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying repo %s: %w", fullName, err)
	}
	return r, nil
}

// UpsertRepo inserts or updates a repository record.
func (d *DB) UpsertRepo(r *RepoRecord) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO repos (
			full_name, owner, name, language,
			stars, stars_prev, forks, open_issues, open_prs,
			contributors, contributors_prev,
			growth_score, normalized_growth_score,
			star_velocity, star_acceleration,
			pr_velocity, issue_velocity, contributor_growth,
			merged_prs_7d, new_issues_7d,
			latest_release, latest_release_date,
			created_at, first_seen_at, last_collected_at,
			status, etag, last_modified,
			primary_category, category_confidence, readme_hash,
			classified_at, model_used, force_category, excluded,
			primary_subcategory, primary_category_legacy, force_subcategory
		) VALUES (
			?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?,
			?, ?,
			?, ?,
			?, ?, ?,
			?, ?,
			?, ?,
			?, ?, ?,
			?, ?, ?,
			?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?
		) ON CONFLICT(full_name) DO UPDATE SET
			owner = excluded.owner,
			name = excluded.name,
			language = excluded.language,
			stars = excluded.stars,
			stars_prev = excluded.stars_prev,
			forks = excluded.forks,
			open_issues = excluded.open_issues,
			open_prs = excluded.open_prs,
			contributors = excluded.contributors,
			contributors_prev = excluded.contributors_prev,
			growth_score = excluded.growth_score,
			normalized_growth_score = excluded.normalized_growth_score,
			star_velocity = excluded.star_velocity,
			star_acceleration = excluded.star_acceleration,
			pr_velocity = excluded.pr_velocity,
			issue_velocity = excluded.issue_velocity,
			contributor_growth = excluded.contributor_growth,
			merged_prs_7d = excluded.merged_prs_7d,
			new_issues_7d = excluded.new_issues_7d,
			latest_release = excluded.latest_release,
			latest_release_date = excluded.latest_release_date,
			created_at = excluded.created_at,
			last_collected_at = excluded.last_collected_at,
			status = excluded.status,
			etag = excluded.etag,
			last_modified = excluded.last_modified,
			primary_category = excluded.primary_category,
			category_confidence = excluded.category_confidence,
			readme_hash = excluded.readme_hash,
			classified_at = excluded.classified_at,
			model_used = excluded.model_used,
			force_category = excluded.force_category,
			excluded = excluded.excluded,
			primary_subcategory = excluded.primary_subcategory,
			primary_category_legacy = excluded.primary_category_legacy,
			force_subcategory = excluded.force_subcategory`,
		r.FullName, r.Owner, r.Name, r.Language,
		r.Stars, r.StarsPrev, r.Forks, r.OpenIssues, r.OpenPRs,
		r.Contributors, r.ContributorsPrev,
		r.GrowthScore, r.NormalizedGrowthScore,
		r.StarVelocity, r.StarAcceleration,
		r.PRVelocity, r.IssueVelocity, r.ContributorGrowth,
		r.MergedPRs7d, r.NewIssues7d,
		r.LatestRelease, r.LatestReleaseDate,
		r.CreatedAt, r.FirstSeenAt, r.LastCollectedAt,
		r.Status, r.ETag, r.LastModified,
		r.PrimaryCategory, r.CategoryConfidence, r.ReadmeHash,
		r.ClassifiedAt, r.ModelUsed, r.ForceCategory, r.Excluded,
		r.PrimarySubcategory, r.PrimaryCategoryLegacy, r.ForceSubcategory,
	)
	if err != nil {
		return fmt.Errorf("upserting repo %s: %w", r.FullName, err)
	}
	return nil
}

// SyncScanData inserts a new repo or updates only scan-related fields for an existing one.
// Classification fields (primary_category, category_confidence, readme_hash, etc.) are preserved.
func (d *DB) SyncScanData(r *RepoRecord) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO repos (
			full_name, owner, name, language,
			stars, stars_prev, forks, open_issues, open_prs,
			contributors, contributors_prev,
			growth_score, normalized_growth_score,
			star_velocity, star_acceleration,
			pr_velocity, issue_velocity, contributor_growth,
			merged_prs_7d, new_issues_7d,
			latest_release, latest_release_date,
			created_at, first_seen_at, last_collected_at,
			status, etag, last_modified
		) VALUES (
			?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?,
			?, ?,
			?, ?,
			?, ?, ?,
			?, ?,
			?, ?,
			?, ?, ?,
			?, ?, ?
		) ON CONFLICT(full_name) DO UPDATE SET
			stars = excluded.stars,
			stars_prev = excluded.stars_prev,
			forks = excluded.forks,
			open_issues = excluded.open_issues,
			open_prs = excluded.open_prs,
			contributors = excluded.contributors,
			contributors_prev = excluded.contributors_prev,
			growth_score = excluded.growth_score,
			normalized_growth_score = excluded.normalized_growth_score,
			star_velocity = excluded.star_velocity,
			star_acceleration = excluded.star_acceleration,
			pr_velocity = excluded.pr_velocity,
			issue_velocity = excluded.issue_velocity,
			contributor_growth = excluded.contributor_growth,
			merged_prs_7d = excluded.merged_prs_7d,
			new_issues_7d = excluded.new_issues_7d,
			last_collected_at = excluded.last_collected_at,
			etag = excluded.etag,
			last_modified = excluded.last_modified`,
		r.FullName, r.Owner, r.Name, r.Language,
		r.Stars, r.StarsPrev, r.Forks, r.OpenIssues, r.OpenPRs,
		r.Contributors, r.ContributorsPrev,
		r.GrowthScore, r.NormalizedGrowthScore,
		r.StarVelocity, r.StarAcceleration,
		r.PRVelocity, r.IssueVelocity, r.ContributorGrowth,
		r.MergedPRs7d, r.NewIssues7d,
		r.LatestRelease, r.LatestReleaseDate,
		r.CreatedAt, r.FirstSeenAt, r.LastCollectedAt,
		r.Status, r.ETag, r.LastModified,
	)
	if err != nil {
		return fmt.Errorf("syncing scan data for %s: %w", r.FullName, err)
	}
	return nil
}

// DeleteRepo removes a repository by full_name.
func (d *DB) DeleteRepo(fullName string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec("DELETE FROM repos WHERE full_name = ?", fullName)
	if err != nil {
		return fmt.Errorf("deleting repo %s: %w", fullName, err)
	}
	return nil
}

// repoSelectColumns is the explicit column list used by queryRepos. It pins
// the ordering to what the Scan in queryRepos expects, which means schema
// additions (e.g. taxonomy v2 columns) do not silently break existing
// SELECT * call sites.
const repoSelectColumns = `id, full_name, owner, name, language,
		stars, stars_prev, forks, open_issues, open_prs,
		contributors, contributors_prev,
		growth_score, normalized_growth_score,
		star_velocity, star_acceleration,
		pr_velocity, issue_velocity, contributor_growth,
		merged_prs_7d, new_issues_7d,
		latest_release, latest_release_date,
		created_at, first_seen_at, last_collected_at,
		status, etag, last_modified,
		primary_category, category_confidence, readme_hash,
		classified_at, model_used, force_category, excluded,
		primary_subcategory, primary_category_legacy, force_subcategory`

// AllRepos returns all non-excluded repository records.
func (d *DB) AllRepos() ([]RepoRecord, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.queryRepos("SELECT " + repoSelectColumns + " FROM repos WHERE excluded = 0 ORDER BY full_name")
}

// AllReposIncludeExcluded returns all repository records including excluded ones.
func (d *DB) AllReposIncludeExcluded() ([]RepoRecord, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.queryRepos("SELECT " + repoSelectColumns + " FROM repos ORDER BY full_name")
}

// ReposByCategory returns repos matching a given primary_category.
//
// Deprecated: post-v3 taxonomy migration (ISI-714), primary_category holds the
// top-level domain (e.g. "ai") rather than the legacy flat value (e.g.
// "ai-agents"). Callers passing legacy flat strings will get zero rows. Prefer:
//
//   - ReposByCategoryPair(category, subcategory) for the new (cat, sub) tuple.
//   - ReposByLegacyCategory(legacy) when the caller still has a flat legacy
//     value (e.g. "ai-agents", "frontend-ui") — selects via the
//     repos_legacy_v1 view so round-trip is preserved.
//
// Kept for backward compatibility with callers that already pass top-level
// values (e.g. "observability", "ai"); semantically equivalent to
// `ReposByLegacyCategory(category)` for the bijective subset where a top-level
// value happens to also be a legacy flat value.
func (d *DB) ReposByCategory(category string) ([]RepoRecord, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.queryRepos(
		"SELECT "+repoSelectColumns+" FROM repos WHERE primary_category = ? AND excluded = 0 ORDER BY full_name",
		category,
	)
}

// ReposByCategoryPair returns non-excluded repos matching the given
// (primary_category, primary_subcategory) tuple under the v3 taxonomy schema
// (ISI-714). This is the preferred read path for code that knows the new
// 2-level taxonomy.
//
// Both arguments are matched exactly; pass the top-level domain (e.g. "ai")
// and the subcategory token (e.g. "agents"). To enumerate repos under a
// top-level domain regardless of subcategory, use ReposByCategory.
func (d *DB) ReposByCategoryPair(category, subcategory string) ([]RepoRecord, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.queryRepos(
		"SELECT "+repoSelectColumns+" FROM repos WHERE primary_category = ? AND primary_subcategory = ? AND excluded = 0 ORDER BY full_name",
		category,
		subcategory,
	)
}

// ReposByLegacyCategory returns non-excluded repos whose pre-v3 flat category
// matched the given legacy value (e.g. "ai-agents", "frontend-ui"). It selects
// via the repos_legacy_v1 compatibility view, which exposes the snapshot
// column primary_category_legacy as legacy_category — see
// internal/database/schema_migration.go for the view definition and the
// round-trip rationale.
//
// Use this when a caller still has a flat legacy string and you don't want to
// re-derive (cat, subcat) from the lookup table. Prefer ReposByCategoryPair
// for new code.
//
// Note: the returned RepoRecord.PrimaryCategory holds the *new* top-level
// domain (e.g. "ai"), not the legacy flat value passed in. If the caller needs
// the legacy form on the row, read it from primary_category_legacy directly
// (added to RepoRecord by T3 WS2).
func (d *DB) ReposByLegacyCategory(legacy string) ([]RepoRecord, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.queryRepos(
		"SELECT "+repoSelectColumns+" FROM repos_legacy_v1 WHERE legacy_category = ? AND excluded = 0 ORDER BY full_name",
		legacy,
	)
}

// ReposByStatus returns repos matching a given status.
func (d *DB) ReposByStatus(status string) ([]RepoRecord, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.queryRepos(
		"SELECT "+repoSelectColumns+" FROM repos WHERE status = ? AND excluded = 0 ORDER BY full_name",
		status,
	)
}

// RepoCount returns the number of non-excluded repositories.
func (d *DB) RepoCount() (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM repos WHERE excluded = 0").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting repos: %w", err)
	}
	return count, nil
}

// SetExcluded marks or unmarks a repo as excluded (Story 10.4).
func (d *DB) SetExcluded(fullName string, excluded bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	val := 0
	if excluded {
		val = 1
	}
	_, err := d.db.Exec("UPDATE repos SET excluded = ? WHERE full_name = ?", val, fullName)
	if err != nil {
		return fmt.Errorf("setting excluded for %s: %w", fullName, err)
	}
	return nil
}

// queryRepos is a helper that scans multiple repo rows.
func (d *DB) queryRepos(query string, args ...interface{}) ([]RepoRecord, error) {
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying repos: %w", err)
	}
	defer rows.Close()

	var repos []RepoRecord
	for rows.Next() {
		var r RepoRecord
		if err := rows.Scan(
			&r.ID, &r.FullName, &r.Owner, &r.Name, &r.Language,
			&r.Stars, &r.StarsPrev, &r.Forks, &r.OpenIssues, &r.OpenPRs,
			&r.Contributors, &r.ContributorsPrev,
			&r.GrowthScore, &r.NormalizedGrowthScore,
			&r.StarVelocity, &r.StarAcceleration,
			&r.PRVelocity, &r.IssueVelocity, &r.ContributorGrowth,
			&r.MergedPRs7d, &r.NewIssues7d,
			&r.LatestRelease, &r.LatestReleaseDate,
			&r.CreatedAt, &r.FirstSeenAt, &r.LastCollectedAt,
			&r.Status, &r.ETag, &r.LastModified,
			&r.PrimaryCategory, &r.CategoryConfidence, &r.ReadmeHash,
			&r.ClassifiedAt, &r.ModelUsed, &r.ForceCategory, &r.Excluded,
			&r.PrimarySubcategory, &r.PrimaryCategoryLegacy, &r.ForceSubcategory,
		); err != nil {
			return nil, fmt.Errorf("scanning repo row: %w", err)
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

// ReposNeedingClassification returns repos that are not excluded and have no
// force_category override, and that need classification by one of:
//   - no category yet (newly scanned), or
//   - status in ('needs_reclassify','needs_review'), or
//   - never-classified rows the v3 taxonomy backfill stamped with primary_category='other'
//     (classified_at=” AND status='pending'); without this clause those rows are
//     permanent zombies because both other branches miss them (ISI-787).
func (d *DB) ReposNeedingClassification() ([]RepoRecord, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.queryRepos(`
		SELECT ` + repoSelectColumns + ` FROM repos
		WHERE excluded = 0
		  AND force_category = ''
		  AND (
		    primary_category = ''
		    OR status IN ('needs_reclassify', 'needs_review')
		    OR (status = 'pending' AND classified_at = '')
		  )
		ORDER BY full_name`)
}

// UpdateClassification stores LLM classification results for a repo.
// If confidence is below minConfidence, the repo status is set to 'needs_review'
// instead of being marked as fully classified.
func (d *DB) UpdateClassification(fullName, category string, confidence float64, readmeHash, modelUsed string, minConfidence float64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	newStatus := "active"
	if confidence < minConfidence {
		newStatus = "needs_review"
	}

	_, err := d.db.Exec(`
		UPDATE repos SET
			primary_category = ?,
			category_confidence = ?,
			readme_hash = ?,
			model_used = ?,
			classified_at = datetime('now'),
			status = ?
		WHERE full_name = ?`,
		category, confidence, readmeHash, modelUsed, newStatus, fullName)
	if err != nil {
		return fmt.Errorf("updating classification for %s: %w", fullName, err)
	}
	return nil
}

// ClassifiedRepos returns repos that have been classified (non-empty primary_category),
// are not excluded, and have no force_category override. These are candidates for
// README hash change detection.
func (d *DB) ClassifiedRepos() ([]RepoRecord, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.queryRepos(`
		SELECT ` + repoSelectColumns + ` FROM repos
		WHERE excluded = 0
		  AND force_category = ''
		  AND primary_category != ''
		  AND status = 'active'
		ORDER BY full_name`)
}

// UpdateReadmeHash updates readme_hash and marks the repo as needs_reclassify if the hash changed.
// Returns true if the hash changed.
func (d *DB) UpdateReadmeHash(fullName, newHash string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	result, err := d.db.Exec(`
		UPDATE repos SET
			readme_hash = ?,
			status = 'needs_reclassify'
		WHERE full_name = ? AND readme_hash != ? AND readme_hash != ''`,
		newHash, fullName, newHash)
	if err != nil {
		return false, fmt.Errorf("updating readme hash for %s: %w", fullName, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

// MarkAllNeedsReclassify sets all classified repos to needs_reclassify (e.g. after model change).
// Returns the number of repos affected.
func (d *DB) MarkAllNeedsReclassify() (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	result, err := d.db.Exec(`
		UPDATE repos SET status = 'needs_reclassify'
		WHERE status = 'active' AND excluded = 0 AND force_category = ''`)
	if err != nil {
		return 0, fmt.Errorf("marking repos needs_reclassify: %w", err)
	}
	return result.RowsAffected()
}

// NeedsReclassifyCount returns the number of non-excluded repos currently in
// status='needs_reclassify'. Surfaced as the `repos_needs_reclassify_total`
// gauge on metrics flush + daemon shutdown so operators can see when the
// drain backlog is growing (ISI-773 regression guard).
func (d *DB) NeedsReclassifyCount() (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var n int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM repos WHERE excluded = 0 AND status = 'needs_reclassify'`,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("counting needs_reclassify repos: %w", err)
	}
	return n, nil
}

// DrainReport is the structured output of DrainNeedsReclassify. It captures
// what was examined, what was drained, and what was held — sufficient for
// the admin CLI to render dry-run preview and post-run report.
type DrainReport struct {
	// Examined is the count of rows in status='needs_reclassify' (excluded=0)
	// before any update. Equals Drained + HeldInSink + HeldOther.
	Examined int
	// Drained is the count of rows flipped status='needs_reclassify' →
	// 'active' (or in dry-run mode, the count that would be flipped).
	Drained int
	// HeldInSink is the count of rows left in needs_reclassify because they
	// landed in the (other,*) refusal sink (needs_review=1). These are the
	// rows the v3 backfill flagged for human triage; the drain leaves them
	// alone.
	HeldInSink int
	// HeldOther is any unexpected residual: rows still in needs_reclassify
	// that don't match either the drainable predicate or the refusal-sink
	// predicate. Should be 0 in steady state. Operations should investigate
	// if non-zero (e.g. legacy rows missing primary_subcategory).
	HeldOther int
	// ByCategory groups Drained by primary_category for the report header.
	ByCategory map[string]int
	// DryRun is true when the report was produced by a dry-run pass (no
	// rows mutated, no backup written).
	DryRun bool
	// BackupPath is the absolute path to the pre-drain backup file. Empty
	// in dry-run mode.
	BackupPath string
}

// DrainNeedsReclassify performs the deterministic drain of repos stuck in
// status='needs_reclassify' that already have valid post-v3 (category,
// subcategory) tuples and are not in the refusal sink. See
// [ISI-773 plan](/ISI/issues/ISI-773#document-plan) for the rationale.
//
// Mechanism: a single UPDATE under BEGIN IMMEDIATE, preceded by a VACUUM
// INTO backup at <db_path>.preDrain.bak. Drainable predicate:
//
//	excluded = 0
//	AND status = 'needs_reclassify'
//	AND primary_category != ''
//	AND primary_subcategory != ''
//	AND primary_category != 'other'
//	AND needs_review = 0
//
// Rows that don't match (i.e. (other,*) with needs_review=1) stay flagged
// for human triage. The function is safe to re-run: a second pass drains 0.
//
// In dry-run mode (dryRun=true) no backup is written and no rows are
// mutated; the report's Drained / HeldInSink / HeldOther counts come from
// SELECT-based previews so the operator can sanity-check the plan before
// the for-real run.
//
// limit, when > 0, caps the number of drained rows in the UPDATE (used by
// ops for staged drains). When limit <= 0 the drain is unrestricted.
func (d *DB) DrainNeedsReclassify(dryRun bool, limit int) (DrainReport, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	report := DrainReport{
		DryRun:     dryRun,
		ByCategory: make(map[string]int),
	}

	// Examined: total in needs_reclassify (excluded=0).
	if err := d.db.QueryRow(
		`SELECT COUNT(*) FROM repos WHERE excluded = 0 AND status = 'needs_reclassify'`,
	).Scan(&report.Examined); err != nil {
		return report, fmt.Errorf("examined count: %w", err)
	}

	// HeldInSink: in refusal sink, left alone.
	if err := d.db.QueryRow(`
		SELECT COUNT(*) FROM repos
		WHERE excluded = 0 AND status = 'needs_reclassify'
		  AND primary_category = 'other'
		  AND needs_review = 1`,
	).Scan(&report.HeldInSink); err != nil {
		return report, fmt.Errorf("held-in-sink count: %w", err)
	}

	// Drainable preview: rows that match the drain predicate.
	drainPredicate := `
		excluded = 0
		AND status = 'needs_reclassify'
		AND primary_category != ''
		AND primary_subcategory != ''
		AND primary_category != 'other'
		AND needs_review = 0`

	var previewDrain int
	if err := d.db.QueryRow(
		`SELECT COUNT(*) FROM repos WHERE` + drainPredicate,
	).Scan(&previewDrain); err != nil {
		return report, fmt.Errorf("drainable preview count: %w", err)
	}
	if limit > 0 && previewDrain > limit {
		previewDrain = limit
	}

	// Held-other: any residual we didn't account for.
	report.HeldOther = report.Examined - previewDrain - report.HeldInSink
	if report.HeldOther < 0 {
		// Shouldn't happen — sets are disjoint by construction. Treat as 0
		// and proceed; the per-category aggregation will still be honest.
		report.HeldOther = 0
	}

	// ByCategory aggregation for drainable preview (used by both dry-run
	// and live runs to render the report header).
	byCatRows, err := d.db.Query(`
		SELECT primary_category, COUNT(*) FROM repos
		WHERE` + drainPredicate + `
		GROUP BY primary_category
		ORDER BY primary_category`)
	if err != nil {
		return report, fmt.Errorf("by-category preview: %w", err)
	}
	defer byCatRows.Close()
	for byCatRows.Next() {
		var cat string
		var n int
		if err := byCatRows.Scan(&cat, &n); err != nil {
			return report, fmt.Errorf("scanning by-category row: %w", err)
		}
		report.ByCategory[cat] = n
	}
	if err := byCatRows.Err(); err != nil {
		return report, fmt.Errorf("iterating by-category rows: %w", err)
	}

	if dryRun {
		report.Drained = previewDrain
		return report, nil
	}

	// Real run: take pre-tx backup (file-backed DBs only). Reuse the same
	// VACUUM INTO helper that the v3 taxonomy migration uses
	// (schema_migration.go), so the drain inherits the same WAL-safe
	// snapshot semantics + stale-file handling.
	if d.path != "" && !strings.HasPrefix(d.path, ":") && !strings.Contains(d.path, ":memory:") {
		backupPath := d.path + ".preDrain.bak"
		if err := d.vacuumIntoBackup(backupPath); err != nil {
			return report, fmt.Errorf("writing pre-drain backup %s: %w", backupPath, err)
		}
		report.BackupPath = backupPath
	}

	// Run the UPDATE in a transaction. Use a positional limit when set; the
	// LIMIT clause on UPDATE is supported by SQLite when SQLITE_ENABLE_UPDATE_DELETE_LIMIT
	// is compiled in. modernc.org/sqlite ships with it enabled.
	tx, err := d.db.Begin()
	if err != nil {
		return report, fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// modernc.org/sqlite does not enable SQLITE_ENABLE_UPDATE_DELETE_LIMIT,
	// so `UPDATE ... LIMIT N` is a syntax error. We get the same effect by
	// gating on a primary-key subquery whose row set is bounded by LIMIT.
	var result sql.Result
	if limit > 0 {
		result, err = tx.Exec(
			`UPDATE repos SET status = 'active' WHERE id IN (
				SELECT id FROM repos WHERE`+drainPredicate+`
				ORDER BY id LIMIT ?)`,
			limit,
		)
	} else {
		result, err = tx.Exec(`UPDATE repos SET status = 'active' WHERE` + drainPredicate)
	}
	if err != nil {
		return report, fmt.Errorf("drain UPDATE: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return report, fmt.Errorf("rows affected: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return report, fmt.Errorf("commit: %w", err)
	}
	committed = true

	report.Drained = int(rows)
	return report, nil
}

// CountByStatus returns counts grouped by status for non-excluded repos. The
// keys are the raw status strings (e.g. "active", "pending", "needs_review",
// "needs_reclassify"). Statuses with zero rows are not present in the map.
//
// Surfaced on the daemon cycle-summary log line and the OTel pending gauge so
// a future SQL Scan / classification regression shows up within one cycle
// instead of going silent for 26h+ (ISI-775).
func (d *DB) CountByStatus() (map[string]int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(
		`SELECT status, COUNT(*) FROM repos WHERE excluded = 0 GROUP BY status`,
	)
	if err != nil {
		return nil, fmt.Errorf("counting repos by status: %w", err)
	}
	defer rows.Close()

	out := make(map[string]int)
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, fmt.Errorf("scanning status row: %w", err)
		}
		out[status] = n
	}
	return out, rows.Err()
}

// PendingBreakdown is one bucket of the pending-status gauge, split by the
// two dimensions that matter operationally: whether the row is excluded
// (excluded=1 rows are skipped by the classifier and so genuinely "pending"
// is meaningful only for excluded=0), and whether a force_category override
// is configured (force_category_set rows are config-managed and should drain
// the moment the next classification cycle picks them up — they're transient
// new-discovery state, not stuck rows).
//
// Operators care about the (Excluded=false, ForceCategorySet=false) bucket:
// >5 for >24h means classification is not draining and something is wrong.
type PendingBreakdown struct {
	Excluded         bool
	ForceCategorySet bool
	Count            int
}

// PendingCountsByDimension returns pending repo counts split by the
// (excluded, force_category_set) tuple. Used by the OTel `radar.repos.pending`
// gauge so dashboards can split transient new-discovery state from genuinely
// stuck rows (ISI-775).
//
// All four dimension tuples are returned even when the count is zero so the
// downstream gauge shape is stable.
func (d *DB) PendingCountsByDimension() ([]PendingBreakdown, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT excluded, CASE WHEN force_category != '' THEN 1 ELSE 0 END AS force_set, COUNT(*)
		FROM repos
		WHERE status = 'pending'
		GROUP BY excluded, force_set`)
	if err != nil {
		return nil, fmt.Errorf("counting pending repos by dimension: %w", err)
	}
	defer rows.Close()

	type key struct {
		excluded bool
		forceSet bool
	}
	counts := make(map[key]int, 4)
	for rows.Next() {
		var excluded, forceSet int
		var n int
		if err := rows.Scan(&excluded, &forceSet, &n); err != nil {
			return nil, fmt.Errorf("scanning pending row: %w", err)
		}
		counts[key{excluded == 1, forceSet == 1}] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]PendingBreakdown, 0, 4)
	for _, excluded := range []bool{false, true} {
		for _, forceSet := range []bool{false, true} {
			out = append(out, PendingBreakdown{
				Excluded:         excluded,
				ForceCategorySet: forceSet,
				Count:            counts[key{excluded, forceSet}],
			})
		}
	}
	return out, nil
}

// LastClassifiedAt returns the most recent classified_at timestamp across
// non-excluded repos as a string in the same format used in the column
// (datetime('now') / RFC3339-ish). Returns the empty string when no repo has
// been classified yet. Surfaced on the cycle-summary log line so operators
// can see when classification has stopped advancing (ISI-775).
func (d *DB) LastClassifiedAt() (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var ts sql.NullString
	err := d.db.QueryRow(
		`SELECT MAX(classified_at) FROM repos WHERE excluded = 0 AND classified_at != ''`,
	).Scan(&ts)
	if err != nil {
		return "", fmt.Errorf("max(classified_at): %w", err)
	}
	if !ts.Valid {
		return "", nil
	}
	return ts.String, nil
}

// Metadata operations

// GetMetadata returns a metadata value by key.
func (d *DB) GetMetadata(key string) (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var value string
	err := d.db.QueryRow("SELECT value FROM metadata WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("getting metadata %s: %w", key, err)
	}
	return value, nil
}

// SetMetadata sets a metadata key-value pair.
func (d *DB) SetMetadata(key, value string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(
		"INSERT INTO metadata (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	)
	if err != nil {
		return fmt.Errorf("setting metadata %s: %w", key, err)
	}
	return nil
}

// GetLastScan returns the last scan timestamp from metadata.
func (d *DB) GetLastScan() (time.Time, error) {
	val, err := d.GetMetadata("last_scan")
	if err != nil || val == "" {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, val)
}

// SetLastScan stores the last scan timestamp in metadata.
func (d *DB) SetLastScan(t time.Time) error {
	return d.SetMetadata("last_scan", t.Format(time.RFC3339))
}

// Discovery operations

// MarkKnownRepo marks a repository as known for discovery.
func (d *DB) MarkKnownRepo(fullName string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(
		"INSERT OR IGNORE INTO discovery_known_repos (full_name) VALUES (?)",
		fullName,
	)
	return err
}

// IsKnownRepo checks if a repository is known for discovery.
func (d *DB) IsKnownRepo(fullName string) (bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var count int
	err := d.db.QueryRow(
		"SELECT COUNT(*) FROM discovery_known_repos WHERE full_name = ?",
		fullName,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// SetTopicScan records when a topic was last scanned.
func (d *DB) SetTopicScan(topic string, t time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(
		`INSERT INTO discovery_topic_scans (topic, scanned_at) VALUES (?, ?)
		 ON CONFLICT(topic) DO UPDATE SET scanned_at = excluded.scanned_at`,
		topic, t.Format(time.RFC3339),
	)
	return err
}

// GetTopicScan returns when a topic was last scanned.
func (d *DB) GetTopicScan(topic string) (time.Time, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var val string
	err := d.db.QueryRow(
		"SELECT scanned_at FROM discovery_topic_scans WHERE topic = ?",
		topic,
	).Scan(&val)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, val)
}

// GetDiscoveryLastScan returns the last discovery scan time.
func (d *DB) GetDiscoveryLastScan() (time.Time, error) {
	val, err := d.GetMetadata("discovery_last_scan")
	if err != nil || val == "" {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, val)
}

// SetDiscoveryLastScan stores the last discovery scan time.
func (d *DB) SetDiscoveryLastScan(t time.Time) error {
	return d.SetMetadata("discovery_last_scan", t.Format(time.RFC3339))
}

// AllKnownRepos returns all known discovery repos as a set.
func (d *DB) AllKnownRepos() (map[string]bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query("SELECT full_name FROM discovery_known_repos")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		result[name] = true
	}
	return result, rows.Err()
}

// AllRepoStatesMap returns all repos as a map keyed by full_name,
// for compatibility with code that used the JSON state store's AllRepoStates().
func (d *DB) AllRepoStatesMap() (map[string]RepoRecord, error) {
	repos, err := d.AllReposIncludeExcluded()
	if err != nil {
		return nil, err
	}
	result := make(map[string]RepoRecord, len(repos))
	for _, r := range repos {
		result[r.FullName] = r
	}
	return result, nil
}

// Topics + description are no longer persisted (ISI-744, folded into the
// taxonomy v3 migration). The classifier live-fetches them from the GitHub
// API via Pipeline.ClassifySingle; consumers that previously read
// repo.Topics / repo.Description should follow the same pattern.

// AuditOtherCandidate is the minimal projection used by the monthly
// `<cat>/other` drift audit (ISI-751). Topics are not in this struct
// because they are live-fetched from GitHub at audit time per ISI-743.
type AuditOtherCandidate struct {
	FullName        string
	PrimaryCategory string
	Confidence      float64
}

// AuditOtherDriftCandidates returns rows where the v3-taxonomy
// classifier has parked the repo in `<cat>/other`, scoped per the
// audit denominator rules (plan §2 + §7): non-curated, currently
// active. Returned in deterministic order by full_name.
//
// Excludes are NOT explicitly filtered — the schema's `excluded` flag
// applies elsewhere (e.g. taxonomy-rule classification overrides) and
// curated/inactive repos are excluded from BOTH numerator and
// denominator via the existing `is_curated_list` and `status` columns.
func (d *DB) AuditOtherDriftCandidates() ([]AuditOtherCandidate, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT full_name, primary_category, category_confidence
		FROM repos
		WHERE primary_subcategory = 'other'
		  AND is_curated_list = 0
		  AND status = 'active'
		ORDER BY full_name`)
	if err != nil {
		return nil, fmt.Errorf("querying audit other-drift candidates: %w", err)
	}
	defer rows.Close()

	var out []AuditOtherCandidate
	for rows.Next() {
		var c AuditOtherCandidate
		if err := rows.Scan(&c.FullName, &c.PrimaryCategory, &c.Confidence); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AuditActiveNonCuratedCount returns the audit denominator (plan §7):
// `is_curated_list = 0 AND status = 'active'`. Curated lists and
// inactive/archived repos are excluded.
func (d *DB) AuditActiveNonCuratedCount() (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM repos WHERE is_curated_list = 0 AND status = 'active'`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("querying active+non-curated count: %w", err)
	}
	return n, nil
}
