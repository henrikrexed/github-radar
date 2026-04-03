package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// RepoRecord represents a repository row in the database.
type RepoRecord struct {
	ID                   int64
	FullName             string
	Owner                string
	Name                 string
	Language             string
	Description          string
	Stars                int
	StarsPrev            int
	Forks                int
	OpenIssues           int
	OpenPRs              int
	Contributors         int
	ContributorsPrev     int
	GrowthScore          float64
	NormalizedGrowthScore float64
	StarVelocity         float64
	StarAcceleration     float64
	PRVelocity           float64
	IssueVelocity        float64
	ContributorGrowth    float64
	MergedPRs7d          int
	NewIssues7d          int
	LatestRelease        string
	LatestReleaseDate    string
	CreatedAt            string
	FirstSeenAt          string
	LastCollectedAt      string
	Topics               string // comma-separated
	Status               string
	ETag                 string
	LastModified         string

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
		SELECT id, full_name, owner, name, language, description,
			stars, stars_prev, forks, open_issues, open_prs,
			contributors, contributors_prev,
			growth_score, normalized_growth_score,
			star_velocity, star_acceleration,
			pr_velocity, issue_velocity, contributor_growth,
			merged_prs_7d, new_issues_7d,
			latest_release, latest_release_date,
			created_at, first_seen_at, last_collected_at,
			topics, status, etag, last_modified,
			primary_category, category_confidence, readme_hash,
			classified_at, model_used, force_category, excluded
		FROM repos WHERE full_name = ?`, fullName).Scan(
		&r.ID, &r.FullName, &r.Owner, &r.Name, &r.Language, &r.Description,
		&r.Stars, &r.StarsPrev, &r.Forks, &r.OpenIssues, &r.OpenPRs,
		&r.Contributors, &r.ContributorsPrev,
		&r.GrowthScore, &r.NormalizedGrowthScore,
		&r.StarVelocity, &r.StarAcceleration,
		&r.PRVelocity, &r.IssueVelocity, &r.ContributorGrowth,
		&r.MergedPRs7d, &r.NewIssues7d,
		&r.LatestRelease, &r.LatestReleaseDate,
		&r.CreatedAt, &r.FirstSeenAt, &r.LastCollectedAt,
		&r.Topics, &r.Status, &r.ETag, &r.LastModified,
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
			full_name, owner, name, language, description,
			stars, stars_prev, forks, open_issues, open_prs,
			contributors, contributors_prev,
			growth_score, normalized_growth_score,
			star_velocity, star_acceleration,
			pr_velocity, issue_velocity, contributor_growth,
			merged_prs_7d, new_issues_7d,
			latest_release, latest_release_date,
			created_at, first_seen_at, last_collected_at,
			topics, status, etag, last_modified,
			primary_category, category_confidence, readme_hash,
			classified_at, model_used, force_category, excluded
		) VALUES (
			?, ?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?,
			?, ?,
			?, ?,
			?, ?, ?,
			?, ?,
			?, ?,
			?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?,
			?, ?, ?, ?
		) ON CONFLICT(full_name) DO UPDATE SET
			owner = excluded.owner,
			name = excluded.name,
			language = excluded.language,
			description = excluded.description,
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
			topics = excluded.topics,
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
		r.FullName, r.Owner, r.Name, r.Language, r.Description,
		r.Stars, r.StarsPrev, r.Forks, r.OpenIssues, r.OpenPRs,
		r.Contributors, r.ContributorsPrev,
		r.GrowthScore, r.NormalizedGrowthScore,
		r.StarVelocity, r.StarAcceleration,
		r.PRVelocity, r.IssueVelocity, r.ContributorGrowth,
		r.MergedPRs7d, r.NewIssues7d,
		r.LatestRelease, r.LatestReleaseDate,
		r.CreatedAt, r.FirstSeenAt, r.LastCollectedAt,
		r.Topics, r.Status, r.ETag, r.LastModified,
		r.PrimaryCategory, r.CategoryConfidence, r.ReadmeHash,
		r.ClassifiedAt, r.ModelUsed, r.ForceCategory, r.Excluded,
	)
	if err != nil {
		return fmt.Errorf("upserting repo %s: %w", r.FullName, err)
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

// AllRepos returns all non-excluded repository records.
func (d *DB) AllRepos() ([]RepoRecord, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.queryRepos("SELECT * FROM repos WHERE excluded = 0 ORDER BY full_name")
}

// AllReposIncludeExcluded returns all repository records including excluded ones.
func (d *DB) AllReposIncludeExcluded() ([]RepoRecord, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.queryRepos("SELECT * FROM repos ORDER BY full_name")
}

// ReposByCategory returns repos matching a given primary_category.
func (d *DB) ReposByCategory(category string) ([]RepoRecord, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.queryRepos(
		"SELECT * FROM repos WHERE primary_category = ? AND excluded = 0 ORDER BY full_name",
		category,
	)
}

// ReposByStatus returns repos matching a given status.
func (d *DB) ReposByStatus(status string) ([]RepoRecord, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.queryRepos(
		"SELECT * FROM repos WHERE status = ? AND excluded = 0 ORDER BY full_name",
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
			&r.ID, &r.FullName, &r.Owner, &r.Name, &r.Language, &r.Description,
			&r.Stars, &r.StarsPrev, &r.Forks, &r.OpenIssues, &r.OpenPRs,
			&r.Contributors, &r.ContributorsPrev,
			&r.GrowthScore, &r.NormalizedGrowthScore,
			&r.StarVelocity, &r.StarAcceleration,
			&r.PRVelocity, &r.IssueVelocity, &r.ContributorGrowth,
			&r.MergedPRs7d, &r.NewIssues7d,
			&r.LatestRelease, &r.LatestReleaseDate,
			&r.CreatedAt, &r.FirstSeenAt, &r.LastCollectedAt,
			&r.Topics, &r.Status, &r.ETag, &r.LastModified,
			&r.PrimaryCategory, &r.CategoryConfidence, &r.ReadmeHash,
			&r.ClassifiedAt, &r.ModelUsed, &r.ForceCategory, &r.Excluded,
		); err != nil {
			return nil, fmt.Errorf("scanning repo row: %w", err)
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
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

// TopicsSlice parses the comma-separated topics string into a slice.
func (r *RepoRecord) TopicsSlice() []string {
	if r.Topics == "" {
		return nil
	}
	return strings.Split(r.Topics, ",")
}

// SetTopicsFromSlice joins a slice into the comma-separated topics string.
func (r *RepoRecord) SetTopicsFromSlice(topics []string) {
	r.Topics = strings.Join(topics, ",")
}
