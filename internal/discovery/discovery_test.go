package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hrexed/github-radar/internal/github"
	"github.com/hrexed/github-radar/internal/state"
)

func TestDiscoverer_DiscoverTopic(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"total_count":        2,
			"incomplete_results": false,
			"items": []map[string]interface{}{
				{
					"owner":            map[string]string{"login": "foo"},
					"name":             "trending-repo",
					"full_name":        "foo/trending-repo",
					"description":      "A trending repository",
					"language":         "Go",
					"topics":           []string{"kubernetes", "trending"},
					"stargazers_count": 500,
					"forks_count":      50,
					"created_at":       time.Now().AddDate(0, -1, 0).Format(time.RFC3339),
					"updated_at":       time.Now().AddDate(0, 0, -1).Format(time.RFC3339),
				},
				{
					"owner":            map[string]string{"login": "bar"},
					"name":             "another-repo",
					"full_name":        "bar/another-repo",
					"description":      "Another repository",
					"language":         "Python",
					"topics":           []string{"kubernetes"},
					"stargazers_count": 200,
					"forks_count":      20,
					"created_at":       time.Now().AddDate(0, -2, 0).Format(time.RFC3339),
					"updated_at":       time.Now().AddDate(0, 0, -3).Format(time.RFC3339),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create client
	client, _ := github.NewClient("test-token")
	client.SetBaseURL(server.URL)

	// Create store
	store := state.NewStore("")

	// Create discoverer
	config := Config{
		Topics:             []string{"kubernetes"},
		MinStars:           100,
		MaxAgeDays:         90,
		AutoTrackThreshold: 50.0,
	}
	discoverer := NewDiscoverer(client, store, config)

	// Run discovery
	result, err := discoverer.DiscoverTopic(context.Background(), "kubernetes")
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}

	// Verify results
	if result.Topic != "kubernetes" {
		t.Errorf("expected topic kubernetes, got %s", result.Topic)
	}
	if result.TotalFound != 2 {
		t.Errorf("expected 2 total found, got %d", result.TotalFound)
	}
	if result.AfterFilters != 2 {
		t.Errorf("expected 2 after filters, got %d", result.AfterFilters)
	}
	if result.NewRepos != 2 {
		t.Errorf("expected 2 new repos, got %d", result.NewRepos)
	}
}

func TestDiscoverer_AlreadyTracked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"total_count":        1,
			"incomplete_results": false,
			"items": []map[string]interface{}{
				{
					"owner":            map[string]string{"login": "foo"},
					"name":             "tracked-repo",
					"full_name":        "foo/tracked-repo",
					"description":      "Already tracked",
					"language":         "Go",
					"topics":           []string{"kubernetes"},
					"stargazers_count": 500,
					"forks_count":      50,
					"created_at":       time.Now().AddDate(0, -1, 0).Format(time.RFC3339),
					"updated_at":       time.Now().AddDate(0, 0, -1).Format(time.RFC3339),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(server.URL)

	store := state.NewStore("")
	// Pre-add repo to state (simulating it's already tracked)
	store.SetRepoState("foo/tracked-repo", state.RepoState{
		Owner: "foo",
		Name:  "tracked-repo",
		Stars: 400,
	})

	config := Config{
		Topics:             []string{"kubernetes"},
		MinStars:           100,
		MaxAgeDays:         90,
		AutoTrackThreshold: 50.0,
	}
	discoverer := NewDiscoverer(client, store, config)

	result, err := discoverer.DiscoverTopic(context.Background(), "kubernetes")
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}

	if result.AlreadyTracked != 1 {
		t.Errorf("expected 1 already tracked, got %d", result.AlreadyTracked)
	}
	if result.NewRepos != 0 {
		t.Errorf("expected 0 new repos, got %d", result.NewRepos)
	}
	if len(result.Repos) > 0 && !result.Repos[0].AlreadyTracked {
		t.Error("expected repo to be marked as already tracked")
	}
}

func TestDiscoverer_Exclusions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"total_count":        1,
			"incomplete_results": false,
			"items": []map[string]interface{}{
				{
					"owner":            map[string]string{"login": "excluded"},
					"name":             "excluded-repo",
					"full_name":        "excluded/excluded-repo",
					"description":      "This is excluded",
					"language":         "Go",
					"topics":           []string{"kubernetes"},
					"stargazers_count": 500,
					"forks_count":      50,
					"created_at":       time.Now().AddDate(0, -1, 0).Format(time.RFC3339),
					"updated_at":       time.Now().AddDate(0, 0, -1).Format(time.RFC3339),
				},
			},
		}
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
		MaxAgeDays:         90,
		AutoTrackThreshold: 50.0,
		Exclusions:         []string{"excluded/excluded-repo"},
	}
	discoverer := NewDiscoverer(client, store, config)

	result, err := discoverer.DiscoverTopic(context.Background(), "kubernetes")
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}

	if result.Excluded != 1 {
		t.Errorf("expected 1 excluded, got %d", result.Excluded)
	}
	if len(result.Repos) > 0 && !result.Repos[0].Excluded {
		t.Error("expected repo to be marked as excluded")
	}
}

func TestDiscoverer_MinStarsFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"total_count":        2,
			"incomplete_results": false,
			"items": []map[string]interface{}{
				{
					"owner":            map[string]string{"login": "foo"},
					"name":             "high-stars",
					"full_name":        "foo/high-stars",
					"description":      "High stars",
					"language":         "Go",
					"topics":           []string{"test"},
					"stargazers_count": 500,
					"forks_count":      50,
					"created_at":       time.Now().AddDate(0, -1, 0).Format(time.RFC3339),
					"updated_at":       time.Now().AddDate(0, 0, -1).Format(time.RFC3339),
				},
				{
					"owner":            map[string]string{"login": "bar"},
					"name":             "low-stars",
					"full_name":        "bar/low-stars",
					"description":      "Low stars",
					"language":         "Go",
					"topics":           []string{"test"},
					"stargazers_count": 50, // Below threshold
					"forks_count":      5,
					"created_at":       time.Now().AddDate(0, -1, 0).Format(time.RFC3339),
					"updated_at":       time.Now().AddDate(0, 0, -1).Format(time.RFC3339),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(server.URL)

	store := state.NewStore("")

	config := Config{
		Topics:             []string{"test"},
		MinStars:           100, // Filter out repos with < 100 stars
		MaxAgeDays:         0,
		AutoTrackThreshold: 50.0,
	}
	discoverer := NewDiscoverer(client, store, config)

	result, err := discoverer.DiscoverTopic(context.Background(), "test")
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}

	if result.TotalFound != 2 {
		t.Errorf("expected 2 total found, got %d", result.TotalFound)
	}
	if result.AfterFilters != 1 {
		t.Errorf("expected 1 after filters, got %d", result.AfterFilters)
	}
}

func TestDiscoverer_AutoTrack(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"total_count":        1,
			"incomplete_results": false,
			"items": []map[string]interface{}{
				{
					"owner":            map[string]string{"login": "foo"},
					"name":             "new-repo",
					"full_name":        "foo/new-repo",
					"description":      "New trending repo",
					"language":         "Go",
					"topics":           []string{"kubernetes"},
					"stargazers_count": 1000,
					"forks_count":      100,
					"created_at":       time.Now().AddDate(0, -1, 0).Format(time.RFC3339),
					"updated_at":       time.Now().AddDate(0, 0, -1).Format(time.RFC3339),
				},
			},
		}
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
		MaxAgeDays:         90,
		AutoTrackThreshold: 0.0, // Auto-track all
	}
	discoverer := NewDiscoverer(client, store, config)

	// Discover
	result, err := discoverer.DiscoverTopic(context.Background(), "kubernetes")
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}

	// Auto-track
	tracked := discoverer.AutoTrack(result)

	if len(tracked) != 1 {
		t.Errorf("expected 1 auto-tracked, got %d", len(tracked))
	}

	// Verify it was added to state
	repoState := store.GetRepoState("foo/new-repo")
	if repoState == nil {
		t.Error("expected repo to be added to state")
	}
	if repoState.Owner != "foo" || repoState.Name != "new-repo" {
		t.Errorf("unexpected repo state: %+v", repoState)
	}
}

func TestDiscoverer_DiscoverAll(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := map[string]interface{}{
			"total_count":        1,
			"incomplete_results": false,
			"items": []map[string]interface{}{
				{
					"owner":            map[string]string{"login": "test"},
					"name":             "repo",
					"full_name":        "test/repo",
					"description":      "Test",
					"language":         "Go",
					"topics":           []string{},
					"stargazers_count": 200,
					"forks_count":      20,
					"created_at":       time.Now().AddDate(0, -1, 0).Format(time.RFC3339),
					"updated_at":       time.Now().AddDate(0, 0, -1).Format(time.RFC3339),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(server.URL)

	store := state.NewStore("")

	config := Config{
		Topics:             []string{"kubernetes", "ebpf", "wasm"},
		MinStars:           100,
		MaxAgeDays:         0,
		AutoTrackThreshold: 50.0,
	}
	discoverer := NewDiscoverer(client, store, config)

	results, err := discoverer.DiscoverAll(context.Background())
	if err != nil {
		t.Fatalf("discover all failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("expected 3 results (one per topic), got %d", len(results))
	}
	if callCount != 3 {
		t.Errorf("expected 3 API calls, got %d", callCount)
	}
}

func TestDiscoverer_NoTopics(t *testing.T) {
	client, _ := github.NewClient("test-token")
	store := state.NewStore("")

	config := Config{
		Topics: []string{}, // No topics
	}
	discoverer := NewDiscoverer(client, store, config)

	results, err := discoverer.DiscoverAll(context.Background())
	if err != nil {
		t.Fatalf("discover all failed: %v", err)
	}

	if results != nil {
		t.Errorf("expected nil results for no topics, got %v", results)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MinStars != 100 {
		t.Errorf("expected MinStars 100, got %d", cfg.MinStars)
	}
	if cfg.MaxAgeDays != 90 {
		t.Errorf("expected MaxAgeDays 90, got %d", cfg.MaxAgeDays)
	}
	if cfg.AutoTrackThreshold != 50.0 {
		t.Errorf("expected AutoTrackThreshold 50.0, got %f", cfg.AutoTrackThreshold)
	}
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		match   bool
	}{
		// Exact matches
		{"foo/bar", "foo/bar", true},
		{"foo/bar", "foo/baz", false},
		{"foo/bar", "bar/bar", false},
		// Wildcard suffix (owner/*)
		{"foo/bar", "foo/*", true},
		{"foo/other", "foo/*", true},
		{"bar/baz", "foo/*", false},
		// Wildcard prefix (*/repo)
		{"foo/bar", "*/bar", true},
		{"other/bar", "*/bar", true},
		{"foo/baz", "*/bar", false},
		// Full wildcard
		{"any/thing", "*/*", true},
		{"foo/bar", "*/*", true},
		// Invalid patterns
		{"foo", "foo/*", false},
		{"foo/bar/baz", "foo/*", false},
	}

	for _, tc := range tests {
		result := matchesPattern(tc.name, tc.pattern)
		if result != tc.match {
			t.Errorf("matchesPattern(%q, %q) = %v, want %v", tc.name, tc.pattern, result, tc.match)
		}
	}
}
