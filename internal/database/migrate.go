package database

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hrexed/github-radar/internal/logging"
)

// jsonState mirrors the JSON state file structure for migration.
type jsonState struct {
	Version   int                      `json:"version"`
	LastScan  time.Time                `json:"last_scan"`
	Repos     map[string]jsonRepoState `json:"repos"`
	Discovery jsonDiscoveryState       `json:"discovery"`
}

type jsonRepoState struct {
	Owner            string    `json:"owner"`
	Name             string    `json:"name"`
	Stars            int       `json:"stars"`
	StarsPrev        int       `json:"stars_prev"`
	Forks            int       `json:"forks"`
	Contributors     int       `json:"contributors"`
	ContributorsPrev int       `json:"contributors_prev"`
	LastCollected    time.Time `json:"last_collected"`

	StarVelocity      float64 `json:"star_velocity"`
	StarAcceleration  float64 `json:"star_acceleration"`
	PRVelocity        float64 `json:"pr_velocity"`
	IssueVelocity     float64 `json:"issue_velocity"`
	ContributorGrowth float64 `json:"contributor_growth"`

	MergedPRs7d int `json:"merged_prs_7d"`
	NewIssues7d int `json:"new_issues_7d"`

	GrowthScore           float64 `json:"growth_score"`
	NormalizedGrowthScore float64 `json:"normalized_growth_score"`

	ETag         string `json:"etag"`
	LastModified string `json:"last_modified"`
}

type jsonDiscoveryState struct {
	LastScan   time.Time            `json:"last_scan"`
	KnownRepos map[string]bool      `json:"known_repos"`
	TopicScans map[string]time.Time `json:"topic_scans"`
}

// MigrateFromJSON detects state.json and migrates data to SQLite.
// If migration succeeds, the JSON file is renamed to state.json.migrated.
// Returns true if a migration was performed.
func MigrateFromJSON(jsonPath string, db *DB) (bool, error) {
	// Check if JSON state file exists
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // No JSON to migrate
		}
		return false, fmt.Errorf("reading state file: %w", err)
	}

	var state jsonState
	if err := json.Unmarshal(data, &state); err != nil {
		return false, fmt.Errorf("parsing state file: %w", err)
	}

	logging.Info("migrating JSON state to SQLite",
		"repos", len(state.Repos),
		"json_path", jsonPath,
		"db_path", db.Path(),
	)

	// Migrate repos
	migrated := 0
	for fullName, rs := range state.Repos {
		lastCollected := ""
		if !rs.LastCollected.IsZero() {
			lastCollected = rs.LastCollected.Format(time.RFC3339)
		}

		record := &RepoRecord{
			FullName:              fullName,
			Owner:                 rs.Owner,
			Name:                  rs.Name,
			Stars:                 rs.Stars,
			StarsPrev:             rs.StarsPrev,
			Forks:                 rs.Forks,
			Contributors:          rs.Contributors,
			ContributorsPrev:      rs.ContributorsPrev,
			LastCollectedAt:       lastCollected,
			StarVelocity:         rs.StarVelocity,
			StarAcceleration:     rs.StarAcceleration,
			PRVelocity:           rs.PRVelocity,
			IssueVelocity:        rs.IssueVelocity,
			ContributorGrowth:    rs.ContributorGrowth,
			MergedPRs7d:          rs.MergedPRs7d,
			NewIssues7d:          rs.NewIssues7d,
			GrowthScore:          rs.GrowthScore,
			NormalizedGrowthScore: rs.NormalizedGrowthScore,
			ETag:                 rs.ETag,
			LastModified:         rs.LastModified,
			Status:               "pending",
			FirstSeenAt:          time.Now().Format(time.RFC3339),
		}

		if err := db.UpsertRepo(record); err != nil {
			return false, fmt.Errorf("migrating repo %s: %w", fullName, err)
		}
		migrated++
	}

	// Migrate last scan time
	if !state.LastScan.IsZero() {
		if err := db.SetLastScan(state.LastScan); err != nil {
			return false, fmt.Errorf("migrating last_scan: %w", err)
		}
	}

	// Migrate discovery state
	if !state.Discovery.LastScan.IsZero() {
		if err := db.SetDiscoveryLastScan(state.Discovery.LastScan); err != nil {
			return false, fmt.Errorf("migrating discovery last_scan: %w", err)
		}
	}

	for name := range state.Discovery.KnownRepos {
		if err := db.MarkKnownRepo(name); err != nil {
			return false, fmt.Errorf("migrating known repo %s: %w", name, err)
		}
	}

	for topic, scannedAt := range state.Discovery.TopicScans {
		if err := db.SetTopicScan(topic, scannedAt); err != nil {
			return false, fmt.Errorf("migrating topic scan %s: %w", topic, err)
		}
	}

	// Rename JSON file to .migrated
	migratedPath := jsonPath + ".migrated"
	if err := os.Rename(jsonPath, migratedPath); err != nil {
		return false, fmt.Errorf("renaming state file: %w", err)
	}

	logging.Info("JSON state migration complete",
		"repos_migrated", migrated,
		"original", jsonPath,
		"renamed_to", filepath.Base(migratedPath),
	)

	return true, nil
}

// DetectAndMigrate is a convenience function that checks for a JSON state
// file adjacent to the database path and migrates if found.
// The JSON state file is expected at the same directory as the DB with
// the name "state.json".
func DetectAndMigrate(dbPath string) (*DB, error) {
	db, err := Open(dbPath)
	if err != nil {
		return nil, err
	}

	// Look for state.json in the same directory as the database
	dir := filepath.Dir(dbPath)
	jsonPath := filepath.Join(dir, "state.json")

	migrated, err := MigrateFromJSON(jsonPath, db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	if migrated {
		logging.Info("database ready after migration", "path", dbPath)
	}

	return db, nil
}
