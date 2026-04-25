package database

import (
	"database/sql"
	"fmt"
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
			classified_at, model_used, force_category, excluded
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
			classified_at, model_used, force_category, excluded
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
			?, ?, ?, ?
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
			excluded = excluded.excluded`,
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
		classified_at, model_used, force_category, excluded`

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
		); err != nil {
			return nil, fmt.Errorf("scanning repo row: %w", err)
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

// ReposNeedingClassification returns repos that are not excluded and have no
// force_category override, and either have no category yet, or have status
// 'needs_reclassify' or 'needs_review' (low-confidence results eligible for re-run).
func (d *DB) ReposNeedingClassification() ([]RepoRecord, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.queryRepos(`
		SELECT ` + repoSelectColumns + ` FROM repos
		WHERE excluded = 0
		  AND force_category = ''
		  AND (primary_category = '' OR status IN ('needs_reclassify', 'needs_review'))
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
