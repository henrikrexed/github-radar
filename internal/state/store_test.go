package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewStore(t *testing.T) {
	store := NewStore("")
	if store.Path() != DefaultStatePath {
		t.Errorf("Path() = %s, want %s", store.Path(), DefaultStatePath)
	}

	store = NewStore("/custom/path.json")
	if store.Path() != "/custom/path.json" {
		t.Errorf("Path() = %s, want /custom/path.json", store.Path())
	}
}

func TestStore_LoadMissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nonexistent.json")

	store := NewStore(path)
	err := store.Load()
	if err != nil {
		t.Errorf("Load() error = %v, want nil for missing file", err)
	}

	// Should have initialized empty state
	if store.RepoCount() != 0 {
		t.Errorf("RepoCount() = %d, want 0", store.RepoCount())
	}
}

func TestStore_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.json")

	// Create and save state
	store := NewStore(path)
	store.SetRepoState("owner/repo", RepoState{
		Owner:         "owner",
		Name:          "repo",
		Stars:         100,
		StarsPrev:     90,
		StarVelocity:  1.5,
		LastCollected: time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC),
	})
	store.SetLastScan(time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC))

	if err := store.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Load into new store
	store2 := NewStore(path)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify state
	repoState := store2.GetRepoState("owner/repo")
	if repoState == nil {
		t.Fatal("GetRepoState() returned nil")
	}
	if repoState.Stars != 100 {
		t.Errorf("Stars = %d, want 100", repoState.Stars)
	}
	if repoState.StarVelocity != 1.5 {
		t.Errorf("StarVelocity = %f, want 1.5", repoState.StarVelocity)
	}
}

func TestStore_AtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.json")

	store := NewStore(path)
	store.SetRepoState("test/repo", RepoState{Stars: 50})

	if err := store.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify temp file is cleaned up
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("temp file should not exist after save")
	}

	// Verify main file exists
	if _, err := os.Stat(path); err != nil {
		t.Errorf("state file should exist: %v", err)
	}
}

func TestStore_GetRepoState_NotFound(t *testing.T) {
	store := NewStore("")
	state := store.GetRepoState("nonexistent/repo")
	if state != nil {
		t.Errorf("GetRepoState() = %v, want nil for missing repo", state)
	}
}

func TestStore_DeleteRepoState(t *testing.T) {
	store := NewStore("")
	store.SetRepoState("owner/repo", RepoState{Stars: 100})

	if store.GetRepoState("owner/repo") == nil {
		t.Fatal("repo should exist before delete")
	}

	store.DeleteRepoState("owner/repo")

	if store.GetRepoState("owner/repo") != nil {
		t.Error("repo should not exist after delete")
	}
}

func TestStore_AllRepoStates(t *testing.T) {
	store := NewStore("")
	store.SetRepoState("repo1", RepoState{Stars: 100})
	store.SetRepoState("repo2", RepoState{Stars: 200})

	all := store.AllRepoStates()
	if len(all) != 2 {
		t.Errorf("AllRepoStates() length = %d, want 2", len(all))
	}

	// Modify returned map shouldn't affect store
	all["repo1"] = RepoState{Stars: 999}
	original := store.GetRepoState("repo1")
	if original.Stars != 100 {
		t.Error("AllRepoStates() should return a copy")
	}
}

func TestStore_IsModified(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.json")

	store := NewStore(path)

	if store.IsModified() {
		t.Error("new store should not be modified")
	}

	store.SetRepoState("test/repo", RepoState{Stars: 50})
	if !store.IsModified() {
		t.Error("store should be modified after SetRepoState")
	}

	if err := store.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if store.IsModified() {
		t.Error("store should not be modified after Save")
	}
}

func TestStore_Discovery(t *testing.T) {
	store := NewStore("")

	// Test known repos
	if store.IsKnownRepo("test/repo") {
		t.Error("repo should not be known initially")
	}

	store.MarkKnownRepo("test/repo")
	if !store.IsKnownRepo("test/repo") {
		t.Error("repo should be known after marking")
	}

	// Test topic scans
	if !store.GetTopicScan("kubernetes").IsZero() {
		t.Error("topic scan should be zero initially")
	}

	scanTime := time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC)
	store.SetTopicScan("kubernetes", scanTime)
	if !store.GetTopicScan("kubernetes").Equal(scanTime) {
		t.Error("topic scan time should be set")
	}
}

func TestStore_LoadCorruptFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "corrupt.json")

	// Write corrupt JSON
	if err := os.WriteFile(path, []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewStore(path)
	err := store.Load()
	if err == nil {
		t.Error("Load() should return error for corrupt file")
	}
}

func TestStore_CreateDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "subdir", "nested", "state.json")

	store := NewStore(path)
	store.SetRepoState("test/repo", RepoState{Stars: 50})

	if err := store.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Errorf("state file should exist in nested directory: %v", err)
	}
}

func TestStore_LastScan(t *testing.T) {
	store := NewStore("")

	if !store.GetLastScan().IsZero() {
		t.Error("LastScan should be zero initially")
	}

	scanTime := time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC)
	store.SetLastScan(scanTime)

	if !store.GetLastScan().Equal(scanTime) {
		t.Errorf("GetLastScan() = %v, want %v", store.GetLastScan(), scanTime)
	}
}

// TestStore_StarObservations covers Get/Set, JSON roundtrip, and the
// failing-safe load path: a state.json written before ISI-982 (no
// star_observations key) must Load cleanly with an empty (non-nil) map.
func TestStore_StarObservations(t *testing.T) {
	store := NewStore("")

	// Initial Get must report "not present" — the prefilter relies on
	// this to fall through to hydrate on cold cache.
	if _, ok := store.GetStarObservation("owner/repo"); ok {
		t.Error("GetStarObservation() on fresh store reported ok=true; want false (cold cache)")
	}

	obs := StarObservation{
		Stars:      42,
		ObservedAt: time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC),
	}
	store.SetStarObservation("owner/repo", obs)

	got, ok := store.GetStarObservation("owner/repo")
	if !ok {
		t.Fatalf("GetStarObservation() reported ok=false after Set; want true")
	}
	if got.Stars != 42 || !got.ObservedAt.Equal(obs.ObservedAt) {
		t.Errorf("roundtrip mismatch: got %+v, want %+v", got, obs)
	}
	if !store.IsModified() {
		t.Error("IsModified() = false after SetStarObservation; want true")
	}

	// JSON roundtrip: Save then Load must preserve the observation.
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.json")
	store2 := NewStore(path)
	store2.SetStarObservation("foo/bar", obs)
	if err := store2.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	store3 := NewStore(path)
	if err := store3.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got, ok = store3.GetStarObservation("foo/bar")
	if !ok {
		t.Fatalf("post-Load GetStarObservation() ok=false; want true")
	}
	if got.Stars != obs.Stars || !got.ObservedAt.Equal(obs.ObservedAt) {
		t.Errorf("post-Load mismatch: got %+v, want %+v", got, obs)
	}
}

// TestStore_LegacyStateWithoutStarObservations confirms a pre-ISI-982
// state.json (no `star_observations` key) loads cleanly and writes are
// safe on the empty map (no nil-map panic in SetStarObservation).
func TestStore_LegacyStateWithoutStarObservations(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.json")

	// Write a legacy state file with no star_observations field.
	legacy := []byte(`{
  "version": 1,
  "last_scan": "2026-02-15T12:00:00Z",
  "repos": {},
  "discovery": {
    "last_scan": "2026-02-15T12:00:00Z",
    "known_repos": {},
    "topic_scans": {}
  }
}`)
	if err := os.WriteFile(path, legacy, 0644); err != nil {
		t.Fatalf("seed legacy state: %v", err)
	}

	store := NewStore(path)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() legacy file: %v", err)
	}

	// Lookup must report "not present"; no panic.
	if _, ok := store.GetStarObservation("any/repo"); ok {
		t.Error("legacy file reported a star observation; want none")
	}

	// Write must succeed without a nil-map panic (Load initializes the
	// map when absent).
	store.SetStarObservation("any/repo", StarObservation{Stars: 1, ObservedAt: time.Now()})
	if _, ok := store.GetStarObservation("any/repo"); !ok {
		t.Error("post-write GetStarObservation() ok=false; want true")
	}
}
