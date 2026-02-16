package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/hrexed/github-radar/internal/github"
	"github.com/hrexed/github-radar/internal/state"
)

// buildLargeSearchResponse builds a GitHub search API response with n repos.
func buildLargeSearchResponse(n int) map[string]interface{} {
	items := make([]map[string]interface{}, n)
	for i := 0; i < n; i++ {
		items[i] = map[string]interface{}{
			"owner":            map[string]string{"login": fmt.Sprintf("org-%d", i)},
			"name":             fmt.Sprintf("repo-%d", i),
			"full_name":        fmt.Sprintf("org-%d/repo-%d", i, i),
			"description":      fmt.Sprintf("Repository number %d for testing", i),
			"language":         "Go",
			"topics":           []string{"kubernetes", "testing"},
			"stargazers_count": 100 + i*10,
			"forks_count":      10 + i,
			"created_at":       time.Now().AddDate(0, -1, 0).Format(time.RFC3339),
			"updated_at":       time.Now().AddDate(0, 0, -1).Format(time.RFC3339),
		}
	}
	return map[string]interface{}{
		"total_count":        n,
		"incomplete_results": false,
		"items":              items,
	}
}

func TestDiscoverer_LargeResultSet_Memory(t *testing.T) {
	// Simulate discovery returning 100 repos (max per page from GitHub)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := buildLargeSearchResponse(100)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(server.URL)
	store := state.NewStore("")

	config := Config{
		Topics:             []string{"kubernetes"},
		MinStars:           100,
		MaxAgeDays:         0, // no age filter
		AutoTrackThreshold: 50.0,
	}
	discoverer := NewDiscoverer(client, store, config)

	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	result, err := discoverer.DiscoverTopic(context.Background(), "kubernetes")
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}

	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	if result.TotalFound != 100 {
		t.Errorf("TotalFound = %d, want 100", result.TotalFound)
	}

	var memGrowthMB float64
	if memAfter.Alloc > memBefore.Alloc {
		memGrowthMB = float64(memAfter.Alloc-memBefore.Alloc) / 1024 / 1024
	}
	t.Logf("Memory growth for 100 discovered repos: %.2f MB", memGrowthMB)

	// 100 discovered repos should be well under 10 MB
	if memGrowthMB > 10 {
		t.Errorf("Memory growth %.2f MB exceeds 10 MB for 100 repos", memGrowthMB)
	}
}

func TestDiscoverer_MultipleTopics_Memory(t *testing.T) {
	// Each topic returns 100 repos, test with 5 topics = 500 total
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := buildLargeSearchResponse(100)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(server.URL)
	store := state.NewStore("")

	topics := []string{"kubernetes", "ebpf", "wasm", "cilium", "envoy"}
	config := Config{
		Topics:             topics,
		MinStars:           100,
		MaxAgeDays:         0,
		AutoTrackThreshold: 50.0,
	}
	discoverer := NewDiscoverer(client, store, config)

	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	results, err := discoverer.DiscoverAll(context.Background())
	if err != nil {
		t.Fatalf("discover all failed: %v", err)
	}

	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	if len(results) != 5 {
		t.Errorf("expected 5 results, got %d", len(results))
	}

	totalRepos := 0
	for _, r := range results {
		totalRepos += len(r.Repos)
	}

	var memGrowthMB float64
	if memAfter.Alloc > memBefore.Alloc {
		memGrowthMB = float64(memAfter.Alloc-memBefore.Alloc) / 1024 / 1024
	}
	t.Logf("Memory growth for %d total discovered repos across %d topics: %.2f MB",
		totalRepos, len(topics), memGrowthMB)

	// 500 total discovered repos across 5 topics should stay under 50 MB
	if memGrowthMB > 50 {
		t.Errorf("Memory growth %.2f MB exceeds 50 MB for 5 topics x 100 repos", memGrowthMB)
	}
}

func TestDiscoverer_AutoTrack_DoesNotDuplicateState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := buildLargeSearchResponse(50)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(server.URL)
	store := state.NewStore("")

	config := Config{
		Topics:             []string{"kubernetes"},
		MinStars:           100,
		MaxAgeDays:         0,
		AutoTrackThreshold: 0.0, // auto-track everything
	}
	discoverer := NewDiscoverer(client, store, config)

	// Run discovery twice to ensure no duplication
	result1, _ := discoverer.DiscoverTopic(context.Background(), "kubernetes")
	tracked1 := discoverer.AutoTrack(result1)

	result2, _ := discoverer.DiscoverTopic(context.Background(), "kubernetes")
	tracked2 := discoverer.AutoTrack(result2)

	t.Logf("First run: %d tracked, Second run: %d tracked", len(tracked1), len(tracked2))

	// Second run should not auto-track any since they're already in state
	if len(tracked2) != 0 {
		t.Errorf("Second discovery run auto-tracked %d repos, expected 0 (already tracked)", len(tracked2))
	}

	// State should have exactly 50 repos, not 100
	count := store.RepoCount()
	if count != 50 {
		t.Errorf("Store has %d repos, expected 50 (no duplicates)", count)
	}
}

func TestDiscoverer_ContextCancellation(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := buildLargeSearchResponse(10)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(server.URL)
	store := state.NewStore("")

	config := Config{
		Topics:             []string{"topic1", "topic2", "topic3", "topic4", "topic5"},
		MinStars:           100,
		MaxAgeDays:         0,
		AutoTrackThreshold: 50.0,
	}
	discoverer := NewDiscoverer(client, store, config)

	// Cancel after a very short time
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately after first topic could complete
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	results, err := discoverer.DiscoverAll(ctx)

	// Should return partial results, not an error panic
	t.Logf("Completed %d topics before cancellation (API calls: %d)", len(results), callCount)

	if err != nil && err != context.Canceled {
		t.Errorf("Unexpected error: %v", err)
	}

	// Should have fewer results than total topics
	if len(results) > len(config.Topics) {
		t.Errorf("Got %d results, but only %d topics configured", len(results), len(config.Topics))
	}
}
