package database

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func tempDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}

func mustOpen(t *testing.T) *DB {
	t.Helper()
	db, err := Open(tempDBPath(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// Story 10.1: Schema initialization

func TestOpen_CreatesDatabase(t *testing.T) {
	path := tempDBPath(t)
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Verify file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("database file was not created")
	}
}

func TestOpen_WALMode(t *testing.T) {
	db := mustOpen(t)

	var mode string
	err := db.db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want %q", mode, "wal")
	}
}

func TestOpen_SchemaVersion(t *testing.T) {
	db := mustOpen(t)

	version, err := db.GetMetadata("schema_version")
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	// Open() runs initSchema + runSchemaMigrations, so a fresh DB lands at
	// the current schema version (bumped when the taxonomy v2 migration
	// was introduced).
	if version != SchemaVersionCurrent {
		t.Errorf("schema_version = %q, want %q", version, SchemaVersionCurrent)
	}
}

func TestOpen_ReposTableExists(t *testing.T) {
	db := mustOpen(t)

	var name string
	err := db.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='repos'",
	).Scan(&name)
	if err != nil {
		t.Fatalf("repos table not found: %v", err)
	}
}

func TestOpen_IndexesExist(t *testing.T) {
	db := mustOpen(t)

	indexes := []string{
		"idx_repos_primary_category",
		"idx_repos_status",
		"idx_repos_excluded",
	}
	for _, idx := range indexes {
		var name string
		err := db.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx,
		).Scan(&name)
		if err != nil {
			t.Errorf("index %s not found: %v", idx, err)
		}
	}
}

func TestOpen_Idempotent(t *testing.T) {
	path := tempDBPath(t)

	// Open twice - should not error
	db1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	db1.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	db2.Close()
}

// Story 10.2: CRUD operations

func TestUpsertAndGetRepo(t *testing.T) {
	db := mustOpen(t)

	repo := &RepoRecord{
		FullName:  "cilium/cilium",
		Owner:     "cilium",
		Name:      "cilium",
		Stars:     20000,
		StarsPrev: 19500,
		Forks:     5000,
		Language:  "Go",
		Status:    "pending",
	}

	if err := db.UpsertRepo(repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	got, err := db.GetRepo("cilium/cilium")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got == nil {
		t.Fatal("GetRepo returned nil")
	}
	if got.Stars != 20000 {
		t.Errorf("Stars = %d, want 20000", got.Stars)
	}
	if got.Language != "Go" {
		t.Errorf("Language = %q, want %q", got.Language, "Go")
	}
	if got.ID == 0 {
		t.Error("ID should be auto-assigned")
	}
}

func TestUpsertRepo_Update(t *testing.T) {
	db := mustOpen(t)

	repo := &RepoRecord{
		FullName: "test/repo",
		Owner:    "test",
		Name:     "repo",
		Stars:    100,
		Status:   "pending",
	}
	if err := db.UpsertRepo(repo); err != nil {
		t.Fatalf("first UpsertRepo: %v", err)
	}

	// Update stars
	repo.Stars = 200
	if err := db.UpsertRepo(repo); err != nil {
		t.Fatalf("second UpsertRepo: %v", err)
	}

	got, err := db.GetRepo("test/repo")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.Stars != 200 {
		t.Errorf("Stars = %d, want 200", got.Stars)
	}
}

func TestGetRepo_NotFound(t *testing.T) {
	db := mustOpen(t)

	got, err := db.GetRepo("nonexistent/repo")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing repo, got %+v", got)
	}
}

func TestDeleteRepo(t *testing.T) {
	db := mustOpen(t)

	repo := &RepoRecord{
		FullName: "test/repo",
		Owner:    "test",
		Name:     "repo",
		Status:   "pending",
	}
	if err := db.UpsertRepo(repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	if err := db.DeleteRepo("test/repo"); err != nil {
		t.Fatalf("DeleteRepo: %v", err)
	}

	got, err := db.GetRepo("test/repo")
	if err != nil {
		t.Fatalf("GetRepo after delete: %v", err)
	}
	if got != nil {
		t.Error("repo should be deleted")
	}
}

func TestAllRepos(t *testing.T) {
	db := mustOpen(t)

	repos := []*RepoRecord{
		{FullName: "a/a", Owner: "a", Name: "a", Status: "pending"},
		{FullName: "b/b", Owner: "b", Name: "b", Status: "pending"},
		{FullName: "c/c", Owner: "c", Name: "c", Status: "pending", Excluded: 1},
	}
	for _, r := range repos {
		if err := db.UpsertRepo(r); err != nil {
			t.Fatalf("UpsertRepo: %v", err)
		}
	}

	// AllRepos excludes excluded repos
	all, err := db.AllRepos()
	if err != nil {
		t.Fatalf("AllRepos: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("AllRepos returned %d repos, want 2", len(all))
	}

	// AllReposIncludeExcluded includes all
	allInc, err := db.AllReposIncludeExcluded()
	if err != nil {
		t.Fatalf("AllReposIncludeExcluded: %v", err)
	}
	if len(allInc) != 3 {
		t.Errorf("AllReposIncludeExcluded returned %d repos, want 3", len(allInc))
	}
}

func TestReposByCategory(t *testing.T) {
	db := mustOpen(t)

	repos := []*RepoRecord{
		{FullName: "a/a", Owner: "a", Name: "a", Status: "pending", PrimaryCategory: "observability"},
		{FullName: "b/b", Owner: "b", Name: "b", Status: "pending", PrimaryCategory: "networking"},
		{FullName: "c/c", Owner: "c", Name: "c", Status: "pending", PrimaryCategory: "observability"},
	}
	for _, r := range repos {
		if err := db.UpsertRepo(r); err != nil {
			t.Fatalf("UpsertRepo: %v", err)
		}
	}

	obs, err := db.ReposByCategory("observability")
	if err != nil {
		t.Fatalf("ReposByCategory: %v", err)
	}
	if len(obs) != 2 {
		t.Errorf("ReposByCategory(observability) = %d, want 2", len(obs))
	}
}

func TestRepoCount(t *testing.T) {
	db := mustOpen(t)

	repos := []*RepoRecord{
		{FullName: "a/a", Owner: "a", Name: "a", Status: "pending"},
		{FullName: "b/b", Owner: "b", Name: "b", Status: "pending", Excluded: 1},
	}
	for _, r := range repos {
		if err := db.UpsertRepo(r); err != nil {
			t.Fatalf("UpsertRepo: %v", err)
		}
	}

	count, err := db.RepoCount()
	if err != nil {
		t.Fatalf("RepoCount: %v", err)
	}
	if count != 1 {
		t.Errorf("RepoCount = %d, want 1 (excluded should not count)", count)
	}
}

// Story 10.4: Excluded repos

func TestSetExcluded(t *testing.T) {
	db := mustOpen(t)

	repo := &RepoRecord{
		FullName: "test/repo",
		Owner:    "test",
		Name:     "repo",
		Stars:    100,
		Status:   "pending",
	}
	if err := db.UpsertRepo(repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	// Exclude
	if err := db.SetExcluded("test/repo", true); err != nil {
		t.Fatalf("SetExcluded(true): %v", err)
	}

	got, err := db.GetRepo("test/repo")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.Excluded != 1 {
		t.Error("repo should be excluded")
	}

	// Data preserved
	if got.Stars != 100 {
		t.Errorf("Stars = %d, want 100 (data should be preserved)", got.Stars)
	}

	// AllRepos skips excluded
	all, err := db.AllRepos()
	if err != nil {
		t.Fatalf("AllRepos: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("AllRepos should skip excluded, got %d", len(all))
	}

	// Un-exclude
	if err := db.SetExcluded("test/repo", false); err != nil {
		t.Fatalf("SetExcluded(false): %v", err)
	}

	all, err = db.AllRepos()
	if err != nil {
		t.Fatalf("AllRepos after un-exclude: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("AllRepos after un-exclude = %d, want 1", len(all))
	}
}

// Metadata and discovery

func TestMetadata(t *testing.T) {
	db := mustOpen(t)

	if err := db.SetMetadata("test_key", "test_value"); err != nil {
		t.Fatalf("SetMetadata: %v", err)
	}

	val, err := db.GetMetadata("test_key")
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if val != "test_value" {
		t.Errorf("GetMetadata = %q, want %q", val, "test_value")
	}

	// Missing key
	val, err = db.GetMetadata("missing")
	if err != nil {
		t.Fatalf("GetMetadata(missing): %v", err)
	}
	if val != "" {
		t.Errorf("GetMetadata(missing) = %q, want empty", val)
	}
}

func TestLastScan(t *testing.T) {
	db := mustOpen(t)

	now := time.Now().Truncate(time.Second)
	if err := db.SetLastScan(now); err != nil {
		t.Fatalf("SetLastScan: %v", err)
	}

	got, err := db.GetLastScan()
	if err != nil {
		t.Fatalf("GetLastScan: %v", err)
	}
	if !got.Equal(now) {
		t.Errorf("GetLastScan = %v, want %v", got, now)
	}
}

func TestDiscoveryOps(t *testing.T) {
	db := mustOpen(t)

	// Mark known repo
	if err := db.MarkKnownRepo("test/repo"); err != nil {
		t.Fatalf("MarkKnownRepo: %v", err)
	}

	known, err := db.IsKnownRepo("test/repo")
	if err != nil {
		t.Fatalf("IsKnownRepo: %v", err)
	}
	if !known {
		t.Error("repo should be known")
	}

	unknown, err := db.IsKnownRepo("other/repo")
	if err != nil {
		t.Fatalf("IsKnownRepo: %v", err)
	}
	if unknown {
		t.Error("other/repo should not be known")
	}

	// Topic scan
	now := time.Now().Truncate(time.Second)
	if err := db.SetTopicScan("kubernetes", now); err != nil {
		t.Fatalf("SetTopicScan: %v", err)
	}

	got, err := db.GetTopicScan("kubernetes")
	if err != nil {
		t.Fatalf("GetTopicScan: %v", err)
	}
	if !got.Equal(now) {
		t.Errorf("GetTopicScan = %v, want %v", got, now)
	}

	// Missing topic
	missing, err := db.GetTopicScan("nonexistent")
	if err != nil {
		t.Fatalf("GetTopicScan(missing): %v", err)
	}
	if !missing.IsZero() {
		t.Errorf("GetTopicScan(missing) should be zero, got %v", missing)
	}
}

// Story 10.3: Migration

func TestMigrateFromJSON(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "state.json")
	dbPath := filepath.Join(dir, "scanner.db")

	// Create a JSON state file
	state := jsonState{
		Version:  1,
		LastScan: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
		Repos: map[string]jsonRepoState{
			"cilium/cilium": {
				Owner:            "cilium",
				Name:             "cilium",
				Stars:            20000,
				StarsPrev:        19500,
				Forks:            5000,
				Contributors:     800,
				ContributorsPrev: 780,
				LastCollected:    time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
				StarVelocity:     15.5,
				StarAcceleration: 0.3,
				GrowthScore:      85.2,
				ETag:             "abc123",
			},
			"grafana/grafana": {
				Owner: "grafana",
				Name:  "grafana",
				Stars: 65000,
				Forks: 12000,
			},
		},
		Discovery: jsonDiscoveryState{
			LastScan:   time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC),
			KnownRepos: map[string]bool{"cilium/cilium": true, "grafana/grafana": true},
			TopicScans: map[string]time.Time{
				"kubernetes":    time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC),
				"observability": time.Date(2026, 2, 27, 0, 0, 0, 0, time.UTC),
			},
		},
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(jsonPath, data, 0644); err != nil {
		t.Fatalf("write json: %v", err)
	}

	// Open DB and migrate
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	migrated, err := MigrateFromJSON(jsonPath, db)
	if err != nil {
		t.Fatalf("MigrateFromJSON: %v", err)
	}
	if !migrated {
		t.Fatal("expected migration to occur")
	}

	// Verify repos migrated
	cilium, err := db.GetRepo("cilium/cilium")
	if err != nil {
		t.Fatalf("GetRepo(cilium): %v", err)
	}
	if cilium == nil {
		t.Fatal("cilium/cilium not found after migration")
	}
	if cilium.Stars != 20000 {
		t.Errorf("cilium Stars = %d, want 20000", cilium.Stars)
	}
	if cilium.StarVelocity != 15.5 {
		t.Errorf("cilium StarVelocity = %f, want 15.5", cilium.StarVelocity)
	}
	if cilium.ETag != "abc123" {
		t.Errorf("cilium ETag = %q, want %q", cilium.ETag, "abc123")
	}

	grafana, err := db.GetRepo("grafana/grafana")
	if err != nil {
		t.Fatalf("GetRepo(grafana): %v", err)
	}
	if grafana == nil {
		t.Fatal("grafana/grafana not found after migration")
	}
	if grafana.Stars != 65000 {
		t.Errorf("grafana Stars = %d, want 65000", grafana.Stars)
	}

	// Verify repo count
	count, err := db.RepoCount()
	if err != nil {
		t.Fatalf("RepoCount: %v", err)
	}
	if count != 2 {
		t.Errorf("RepoCount = %d, want 2", count)
	}

	// Verify last scan migrated
	lastScan, err := db.GetLastScan()
	if err != nil {
		t.Fatalf("GetLastScan: %v", err)
	}
	if !lastScan.Equal(state.LastScan) {
		t.Errorf("LastScan = %v, want %v", lastScan, state.LastScan)
	}

	// Verify discovery state migrated
	known, err := db.IsKnownRepo("cilium/cilium")
	if err != nil {
		t.Fatalf("IsKnownRepo: %v", err)
	}
	if !known {
		t.Error("cilium/cilium should be known after migration")
	}

	topicScan, err := db.GetTopicScan("kubernetes")
	if err != nil {
		t.Fatalf("GetTopicScan: %v", err)
	}
	expected := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)
	if !topicScan.Equal(expected) {
		t.Errorf("kubernetes topic scan = %v, want %v", topicScan, expected)
	}

	// Verify JSON file renamed
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Error("state.json should have been renamed")
	}
	migratedPath := jsonPath + ".migrated"
	if _, err := os.Stat(migratedPath); os.IsNotExist(err) {
		t.Error("state.json.migrated should exist")
	}
}

func TestMigrateFromJSON_NoFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "scanner.db")
	jsonPath := filepath.Join(dir, "state.json")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	migrated, err := MigrateFromJSON(jsonPath, db)
	if err != nil {
		t.Fatalf("MigrateFromJSON: %v", err)
	}
	if migrated {
		t.Error("should not migrate when no JSON file exists")
	}
}

// Concurrent access (WAL mode)

func TestConcurrentAccess(t *testing.T) {
	db := mustOpen(t)

	// Seed some data
	for i := 0; i < 10; i++ {
		repo := &RepoRecord{
			FullName: fmt.Sprintf("org/repo-%d", i),
			Owner:    "org",
			Name:     fmt.Sprintf("repo-%d", i),
			Stars:    i * 100,
			Status:   "pending",
		}
		if err := db.UpsertRepo(repo); err != nil {
			t.Fatalf("seed UpsertRepo: %v", err)
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	// Concurrent writers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				repo := &RepoRecord{
					FullName: fmt.Sprintf("org/repo-%d", n),
					Owner:    "org",
					Name:     fmt.Sprintf("repo-%d", n),
					Stars:    (n + 1) * 100 * (j + 1),
					Status:   "pending",
				}
				if err := db.UpsertRepo(repo); err != nil {
					errCh <- err
					return
				}
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, err := db.AllRepos()
				if err != nil {
					errCh <- err
					return
				}
				_, err = db.RepoCount()
				if err != nil {
					errCh <- err
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent error: %v", err)
	}
}

// Topics + description are no longer persisted (ISI-744, folded into the
// taxonomy v3 migration). The RepoRecord fields and TopicsSlice helper are
// gone; the classifier live-fetches them from the GitHub API.

// Story 11.7: Classification and reclassification triggers

func TestClassifiedRepos(t *testing.T) {
	db := mustOpen(t)

	repos := []*RepoRecord{
		{FullName: "a/active", Owner: "a", Name: "active", Status: "active", PrimaryCategory: "observability", ReadmeHash: "abc123"},
		{FullName: "b/pending", Owner: "b", Name: "pending", Status: "pending", PrimaryCategory: ""},
		{FullName: "c/review", Owner: "c", Name: "review", Status: "needs_review", PrimaryCategory: "networking"},
		{FullName: "d/excluded", Owner: "d", Name: "excluded", Status: "active", PrimaryCategory: "kubernetes", Excluded: 1},
		{FullName: "e/forced", Owner: "e", Name: "forced", Status: "active", PrimaryCategory: "ai-agents", ForceCategory: "ai-agents"},
		{FullName: "f/active2", Owner: "f", Name: "active2", Status: "active", PrimaryCategory: "llm-tooling", ReadmeHash: "def456"},
	}
	for _, r := range repos {
		if err := db.UpsertRepo(r); err != nil {
			t.Fatalf("UpsertRepo(%s): %v", r.FullName, err)
		}
	}

	got, err := db.ClassifiedRepos()
	if err != nil {
		t.Fatalf("ClassifiedRepos: %v", err)
	}
	// Should only return active repos that are not excluded and have no force_category
	if len(got) != 2 {
		t.Errorf("ClassifiedRepos returned %d repos, want 2", len(got))
		for _, r := range got {
			t.Logf("  got: %s (status=%s, category=%s)", r.FullName, r.Status, r.PrimaryCategory)
		}
	}
}

func TestUpdateReadmeHash_Changed(t *testing.T) {
	db := mustOpen(t)

	repo := &RepoRecord{
		FullName:        "test/repo",
		Owner:           "test",
		Name:            "repo",
		Status:          "active",
		PrimaryCategory: "observability",
		ReadmeHash:      "oldhash123",
	}
	if err := db.UpsertRepo(repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	changed, err := db.UpdateReadmeHash("test/repo", "newhash456")
	if err != nil {
		t.Fatalf("UpdateReadmeHash: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when hash differs")
	}

	got, err := db.GetRepo("test/repo")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.ReadmeHash != "newhash456" {
		t.Errorf("ReadmeHash = %q, want %q", got.ReadmeHash, "newhash456")
	}
	if got.Status != "needs_reclassify" {
		t.Errorf("Status = %q, want %q", got.Status, "needs_reclassify")
	}
}

func TestUpdateReadmeHash_Unchanged(t *testing.T) {
	db := mustOpen(t)

	repo := &RepoRecord{
		FullName:        "test/repo",
		Owner:           "test",
		Name:            "repo",
		Status:          "active",
		PrimaryCategory: "observability",
		ReadmeHash:      "samehash",
	}
	if err := db.UpsertRepo(repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	changed, err := db.UpdateReadmeHash("test/repo", "samehash")
	if err != nil {
		t.Fatalf("UpdateReadmeHash: %v", err)
	}
	if changed {
		t.Error("expected changed=false when hash is the same")
	}

	got, err := db.GetRepo("test/repo")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.Status != "active" {
		t.Errorf("Status = %q, want %q (should not change)", got.Status, "active")
	}
}

func TestUpdateReadmeHash_SkipsEmptyHash(t *testing.T) {
	db := mustOpen(t)

	// Repo with empty readme_hash (initial state) should not trigger reclassify
	repo := &RepoRecord{
		FullName:   "test/repo",
		Owner:      "test",
		Name:       "repo",
		Status:     "pending",
		ReadmeHash: "",
	}
	if err := db.UpsertRepo(repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	changed, err := db.UpdateReadmeHash("test/repo", "newhash")
	if err != nil {
		t.Fatalf("UpdateReadmeHash: %v", err)
	}
	if changed {
		t.Error("expected changed=false when old hash is empty (initial classification)")
	}
}

func TestMarkAllNeedsReclassify(t *testing.T) {
	db := mustOpen(t)

	repos := []*RepoRecord{
		{FullName: "a/classified", Owner: "a", Name: "classified", Status: "active", PrimaryCategory: "observability"},
		{FullName: "b/pending", Owner: "b", Name: "pending", Status: "pending", PrimaryCategory: ""},
		{FullName: "c/excluded", Owner: "c", Name: "excluded", Status: "active", PrimaryCategory: "networking", Excluded: 1},
		{FullName: "d/forced", Owner: "d", Name: "forced", Status: "active", PrimaryCategory: "ai-agents", ForceCategory: "ai-agents"},
	}
	for _, r := range repos {
		if err := db.UpsertRepo(r); err != nil {
			t.Fatalf("UpsertRepo(%s): %v", r.FullName, err)
		}
	}

	count, err := db.MarkAllNeedsReclassify()
	if err != nil {
		t.Fatalf("MarkAllNeedsReclassify: %v", err)
	}
	// Only a/classified should be affected (b has no category, c is excluded, d has force_category)
	if count != 1 {
		t.Errorf("MarkAllNeedsReclassify affected %d rows, want 1", count)
	}

	got, err := db.GetRepo("a/classified")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.Status != "needs_reclassify" {
		t.Errorf("Status = %q, want %q", got.Status, "needs_reclassify")
	}
}

func TestReposNeedingClassification(t *testing.T) {
	db := mustOpen(t)

	repos := []*RepoRecord{
		{FullName: "a/new", Owner: "a", Name: "new", Status: "pending", PrimaryCategory: ""},
		{FullName: "b/reclassify", Owner: "b", Name: "reclassify", Status: "needs_reclassify", PrimaryCategory: "observability"},
		{FullName: "c/review", Owner: "c", Name: "review", Status: "needs_review", PrimaryCategory: "networking"},
		{FullName: "d/active", Owner: "d", Name: "active", Status: "active", PrimaryCategory: "kubernetes"},
		{FullName: "e/excluded", Owner: "e", Name: "excluded", Status: "pending", PrimaryCategory: "", Excluded: 1},
		{FullName: "f/forced", Owner: "f", Name: "forced", Status: "pending", PrimaryCategory: "", ForceCategory: "ai-agents"},
	}
	for _, r := range repos {
		if err := db.UpsertRepo(r); err != nil {
			t.Fatalf("UpsertRepo(%s): %v", r.FullName, err)
		}
	}

	got, err := db.ReposNeedingClassification()
	if err != nil {
		t.Fatalf("ReposNeedingClassification: %v", err)
	}
	// a/new (no category), b/reclassify, c/review — but not d/active, e/excluded, f/forced
	if len(got) != 3 {
		t.Errorf("ReposNeedingClassification returned %d repos, want 3", len(got))
		for _, r := range got {
			t.Logf("  got: %s (status=%s, category=%s)", r.FullName, r.Status, r.PrimaryCategory)
		}
	}
}

// Story 11.5: UpdateClassification with minConfidence logic

func TestUpdateClassification_HighConfidence(t *testing.T) {
	db := mustOpen(t)

	repo := &RepoRecord{
		FullName: "test/repo", Owner: "test", Name: "repo",
		Status: "pending", PrimaryCategory: "",
	}
	if err := db.UpsertRepo(repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	err := db.UpdateClassification("test/repo", "kubernetes", 0.92, "hash123", "qwen3:1.7b", 0.6)
	if err != nil {
		t.Fatalf("UpdateClassification: %v", err)
	}

	got, err := db.GetRepo("test/repo")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.PrimaryCategory != "kubernetes" {
		t.Errorf("PrimaryCategory = %q, want %q", got.PrimaryCategory, "kubernetes")
	}
	if got.CategoryConfidence != 0.92 {
		t.Errorf("CategoryConfidence = %f, want 0.92", got.CategoryConfidence)
	}
	if got.ReadmeHash != "hash123" {
		t.Errorf("ReadmeHash = %q, want %q", got.ReadmeHash, "hash123")
	}
	if got.ModelUsed != "qwen3:1.7b" {
		t.Errorf("ModelUsed = %q, want %q", got.ModelUsed, "qwen3:1.7b")
	}
	if got.Status != "active" {
		t.Errorf("Status = %q, want %q (above minConfidence)", got.Status, "active")
	}
	if got.ClassifiedAt == "" {
		t.Error("ClassifiedAt should be set")
	}
}

func TestUpdateClassification_LowConfidence_NeedsReview(t *testing.T) {
	db := mustOpen(t)

	repo := &RepoRecord{
		FullName: "test/repo", Owner: "test", Name: "repo",
		Status: "pending", PrimaryCategory: "",
	}
	if err := db.UpsertRepo(repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	// Confidence 0.3 is below minConfidence 0.6
	err := db.UpdateClassification("test/repo", "other", 0.3, "hash456", "qwen3:1.7b", 0.6)
	if err != nil {
		t.Fatalf("UpdateClassification: %v", err)
	}

	got, err := db.GetRepo("test/repo")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.Status != "needs_review" {
		t.Errorf("Status = %q, want %q (below minConfidence)", got.Status, "needs_review")
	}
	if got.PrimaryCategory != "other" {
		t.Errorf("PrimaryCategory = %q, want %q", got.PrimaryCategory, "other")
	}
}

func TestUpdateClassification_ExactThreshold(t *testing.T) {
	db := mustOpen(t)

	repo := &RepoRecord{
		FullName: "test/repo", Owner: "test", Name: "repo",
		Status: "pending", PrimaryCategory: "",
	}
	if err := db.UpsertRepo(repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	// Confidence exactly at threshold → should be "active" (not < threshold)
	err := db.UpdateClassification("test/repo", "kubernetes", 0.6, "hash789", "qwen3:1.7b", 0.6)
	if err != nil {
		t.Fatalf("UpdateClassification: %v", err)
	}

	got, err := db.GetRepo("test/repo")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.Status != "active" {
		t.Errorf("Status = %q, want %q (at exact threshold)", got.Status, "active")
	}
}

func TestUpdateClassification_Reclassify(t *testing.T) {
	db := mustOpen(t)

	// First classification
	repo := &RepoRecord{
		FullName: "test/repo", Owner: "test", Name: "repo",
		Status: "active", PrimaryCategory: "observability",
		CategoryConfidence: 0.7, ReadmeHash: "oldhash", ModelUsed: "old-model",
	}
	if err := db.UpsertRepo(repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	// Re-classify with new category
	err := db.UpdateClassification("test/repo", "kubernetes", 0.95, "newhash", "new-model", 0.6)
	if err != nil {
		t.Fatalf("UpdateClassification: %v", err)
	}

	got, err := db.GetRepo("test/repo")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.PrimaryCategory != "kubernetes" {
		t.Errorf("PrimaryCategory = %q, want %q", got.PrimaryCategory, "kubernetes")
	}
	if got.ModelUsed != "new-model" {
		t.Errorf("ModelUsed = %q, want %q", got.ModelUsed, "new-model")
	}
	if got.ReadmeHash != "newhash" {
		t.Errorf("ReadmeHash = %q, want %q", got.ReadmeHash, "newhash")
	}
}
