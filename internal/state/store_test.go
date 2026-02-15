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
