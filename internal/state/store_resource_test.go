package state

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestStore_LargeStateLoad_Memory(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "large_state.json")

	// Create a store with 1000 repos and save it
	store := NewStore(path)
	for i := 0; i < 1000; i++ {
		store.SetRepoState(fmt.Sprintf("org/repo-%d", i), RepoState{
			Owner:                 "org",
			Name:                  fmt.Sprintf("repo-%d", i),
			Stars:                 i * 10,
			StarsPrev:             i * 9,
			Forks:                 i * 2,
			Contributors:          i + 1,
			ContributorsPrev:      i,
			StarVelocity:          float64(i) * 1.5,
			StarAcceleration:      float64(i) * 0.1,
			PRVelocity:            float64(i) * 0.5,
			IssueVelocity:         float64(i) * 0.3,
			ContributorGrowth:     float64(i) * 0.05,
			MergedPRs7d:           i % 20,
			NewIssues7d:           i % 10,
			GrowthScore:           float64(i) * 2.0,
			NormalizedGrowthScore: float64(i) / 1000.0,
			ETag:                  fmt.Sprintf(`"etag-%d"`, i),
			LastCollected:         time.Now().Add(-time.Duration(i) * time.Hour),
		})
	}

	if err := store.Save(); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	// Now load and measure memory
	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	store2 := NewStore(path)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}

	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	if store2.RepoCount() != 1000 {
		t.Errorf("RepoCount = %d, want 1000", store2.RepoCount())
	}

	var memGrowthMB float64
	if memAfter.Alloc > memBefore.Alloc {
		memGrowthMB = float64(memAfter.Alloc-memBefore.Alloc) / 1024 / 1024
	}
	t.Logf("Memory for loading 1000 repos: %.2f MB", memGrowthMB)

	// 1000 repos should fit comfortably in < 20 MB
	if memGrowthMB > 20 {
		t.Errorf("Memory growth %.2f MB exceeds 20 MB threshold for 1000 repos", memGrowthMB)
	}
}

func TestStore_LargeStateSave_Duration(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "large_state.json")

	store := NewStore(path)
	for i := 0; i < 1000; i++ {
		store.SetRepoState(fmt.Sprintf("org/repo-%d", i), RepoState{
			Owner:             "org",
			Name:              fmt.Sprintf("repo-%d", i),
			Stars:             i * 10,
			Forks:             i * 2,
			StarVelocity:      float64(i) * 1.5,
			GrowthScore:       float64(i) * 2.0,
			ETag:              fmt.Sprintf(`"etag-%d"`, i),
			LastCollected:     time.Now(),
		})
	}

	start := time.Now()
	if err := store.Save(); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	saveDuration := time.Since(start)

	t.Logf("Save duration for 1000 repos: %v", saveDuration)

	// Saving 1000 repos should complete in < 5 seconds
	if saveDuration > 5*time.Second {
		t.Errorf("Save took %v, expected < 5s for 1000 repos", saveDuration)
	}

	// Load should also be fast
	store2 := NewStore(path)
	start = time.Now()
	if err := store2.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}
	loadDuration := time.Since(start)

	t.Logf("Load duration for 1000 repos: %v", loadDuration)
	if loadDuration > 5*time.Second {
		t.Errorf("Load took %v, expected < 5s for 1000 repos", loadDuration)
	}
}

func TestStore_5000Repos_ScalingTest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large scaling test in short mode")
	}

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "huge_state.json")

	store := NewStore(path)
	for i := 0; i < 5000; i++ {
		store.SetRepoState(fmt.Sprintf("org-%d/repo-%d", i/100, i), RepoState{
			Owner:             fmt.Sprintf("org-%d", i/100),
			Name:              fmt.Sprintf("repo-%d", i),
			Stars:             i * 10,
			Forks:             i * 2,
			StarVelocity:      float64(i) * 1.5,
			GrowthScore:       float64(i) * 2.0,
			ETag:              fmt.Sprintf(`"etag-%d"`, i),
			LastCollected:     time.Now(),
		})
	}

	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	if err := store.Save(); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	store2 := NewStore(path)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}

	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	if store2.RepoCount() != 5000 {
		t.Errorf("RepoCount = %d, want 5000", store2.RepoCount())
	}

	var memGrowthMB float64
	if memAfter.Alloc > memBefore.Alloc {
		memGrowthMB = float64(memAfter.Alloc-memBefore.Alloc) / 1024 / 1024
	}
	t.Logf("Memory for 5000 repos (save+load): %.2f MB", memGrowthMB)

	// 5000 repos should stay under 100 MB
	if memGrowthMB > 100 {
		t.Errorf("Memory growth %.2f MB exceeds 100 MB threshold for 5000 repos", memGrowthMB)
	}
}

func TestStore_AllRepoStates_MemoryCopy(t *testing.T) {
	store := NewStore("")
	for i := 0; i < 500; i++ {
		store.SetRepoState(fmt.Sprintf("org/repo-%d", i), RepoState{
			Owner: "org",
			Name:  fmt.Sprintf("repo-%d", i),
			Stars: i * 10,
		})
	}

	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// AllRepoStates makes a copy - ensure it doesn't double memory unexpectedly
	all := store.AllRepoStates()

	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	if len(all) != 500 {
		t.Errorf("AllRepoStates() length = %d, want 500", len(all))
	}

	var memGrowthMB float64
	if memAfter.Alloc > memBefore.Alloc {
		memGrowthMB = float64(memAfter.Alloc-memBefore.Alloc) / 1024 / 1024
	}
	t.Logf("Memory growth from AllRepoStates copy (500 repos): %.2f MB", memGrowthMB)

	// A copy of 500 repo states should be < 5 MB
	if memGrowthMB > 5 {
		t.Errorf("AllRepoStates copy took %.2f MB, expected < 5 MB", memGrowthMB)
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	store := NewStore("")

	done := make(chan bool, 3)

	// Writer goroutine
	go func() {
		for i := 0; i < 1000; i++ {
			store.SetRepoState(fmt.Sprintf("org/repo-%d", i), RepoState{
				Stars: i,
			})
		}
		done <- true
	}()

	// Reader goroutine 1
	go func() {
		for i := 0; i < 1000; i++ {
			store.GetRepoState(fmt.Sprintf("org/repo-%d", i%100))
		}
		done <- true
	}()

	// Reader goroutine 2
	go func() {
		for i := 0; i < 100; i++ {
			store.AllRepoStates()
		}
		done <- true
	}()

	// Wait for all goroutines - no data race or deadlock should occur
	for i := 0; i < 3; i++ {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("Concurrent access test timed out - possible deadlock")
		}
	}
}
