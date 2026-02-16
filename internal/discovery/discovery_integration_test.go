package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hrexed/github-radar/internal/github"
	"github.com/hrexed/github-radar/internal/state"
)

// mockSearchServer creates a configurable mock GitHub search API server.
type mockSearchServer struct {
	server       *httptest.Server
	callLog      []string
	responseFunc func(query string) map[string]interface{}
}

func newMockSearchServer(respFunc func(query string) map[string]interface{}) *mockSearchServer {
	m := &mockSearchServer{
		responseFunc: respFunc,
	}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		m.callLog = append(m.callLog, query)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.Header().Set("X-RateLimit-Limit", "5000")

		resp := m.responseFunc(query)
		json.NewEncoder(w).Encode(resp)
	}))
	return m
}

func (m *mockSearchServer) Close() {
	m.server.Close()
}

func TestDiscovery_FindsReposByTopic(t *testing.T) {
	mock := newMockSearchServer(func(query string) map[string]interface{} {
		if !strings.Contains(query, "topic:kubernetes") {
			return map[string]interface{}{
				"total_count": 0, "incomplete_results": false, "items": []interface{}{},
			}
		}
		return map[string]interface{}{
			"total_count":        3,
			"incomplete_results": false,
			"items": []map[string]interface{}{
				{
					"owner": map[string]string{"login": "cncf"},
					"name": "kubernetes", "full_name": "cncf/kubernetes",
					"description": "Container orchestration",
					"language": "Go", "topics": []string{"kubernetes", "containers"},
					"stargazers_count": 10000, "forks_count": 5000,
					"created_at": time.Now().AddDate(0, -2, 0).Format(time.RFC3339),
					"updated_at": time.Now().AddDate(0, 0, -1).Format(time.RFC3339),
				},
				{
					"owner": map[string]string{"login": "helm"},
					"name": "helm", "full_name": "helm/helm",
					"description": "Kubernetes package manager",
					"language": "Go", "topics": []string{"kubernetes", "helm"},
					"stargazers_count": 5000, "forks_count": 2000,
					"created_at": time.Now().AddDate(0, -1, 0).Format(time.RFC3339),
					"updated_at": time.Now().AddDate(0, 0, -2).Format(time.RFC3339),
				},
				{
					"owner": map[string]string{"login": "istio"},
					"name": "istio", "full_name": "istio/istio",
					"description": "Service mesh",
					"language": "Go", "topics": []string{"kubernetes", "service-mesh"},
					"stargazers_count": 3000, "forks_count": 1500,
					"created_at": time.Now().AddDate(0, -1, -15).Format(time.RFC3339),
					"updated_at": time.Now().AddDate(0, 0, -3).Format(time.RFC3339),
				},
			},
		}
	})
	defer mock.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(mock.server.URL)
	store := state.NewStore("")

	discoverer := NewDiscoverer(client, store, Config{
		Topics:             []string{"kubernetes"},
		MinStars:           100,
		MaxAgeDays:         90,
		AutoTrackThreshold: 50.0,
	})

	result, err := discoverer.DiscoverTopic(context.Background(), "kubernetes")
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}

	if result.TotalFound != 3 {
		t.Errorf("expected 3 repos found, got %d", result.TotalFound)
	}
	if result.AfterFilters != 3 {
		t.Errorf("expected 3 after filters, got %d", result.AfterFilters)
	}

	// Verify repos have expected data
	repoNames := make(map[string]bool)
	for _, repo := range result.Repos {
		repoNames[repo.FullName] = true
		if repo.Stars <= 0 {
			t.Errorf("Repo %s has invalid stars: %d", repo.FullName, repo.Stars)
		}
		if repo.Language == "" {
			t.Errorf("Repo %s has empty language", repo.FullName)
		}
	}

	if !repoNames["cncf/kubernetes"] {
		t.Error("Expected cncf/kubernetes in results")
	}
	if !repoNames["helm/helm"] {
		t.Error("Expected helm/helm in results")
	}
}

func TestDiscovery_EmptyResults(t *testing.T) {
	mock := newMockSearchServer(func(query string) map[string]interface{} {
		return map[string]interface{}{
			"total_count":        0,
			"incomplete_results": false,
			"items":              []interface{}{},
		}
	})
	defer mock.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(mock.server.URL)
	store := state.NewStore("")

	discoverer := NewDiscoverer(client, store, Config{
		Topics:             []string{"nonexistent-topic"},
		MinStars:           100,
		MaxAgeDays:         0,
		AutoTrackThreshold: 50.0,
	})

	result, err := discoverer.DiscoverTopic(context.Background(), "nonexistent-topic")
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}

	if result.TotalFound != 0 {
		t.Errorf("expected 0 repos found, got %d", result.TotalFound)
	}
	if len(result.Repos) != 0 {
		t.Errorf("expected 0 repos in results, got %d", len(result.Repos))
	}
}

func TestDiscovery_APIError_Handled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer server.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(server.URL)
	store := state.NewStore("")

	discoverer := NewDiscoverer(client, store, Config{
		Topics:             []string{"kubernetes"},
		MinStars:           100,
		AutoTrackThreshold: 50.0,
	})

	_, err := discoverer.DiscoverTopic(context.Background(), "kubernetes")
	if err == nil {
		t.Error("Expected error for API failure, got nil")
	}

	t.Logf("API error correctly returned: %v", err)
}

func TestDiscovery_FiltersOldRepos(t *testing.T) {
	mock := newMockSearchServer(func(query string) map[string]interface{} {
		return map[string]interface{}{
			"total_count":        2,
			"incomplete_results": false,
			"items": []map[string]interface{}{
				{
					"owner": map[string]string{"login": "new"},
					"name": "repo", "full_name": "new/repo",
					"description": "Recent", "language": "Go", "topics": []string{},
					"stargazers_count": 500, "forks_count": 50,
					"created_at": time.Now().AddDate(0, -1, 0).Format(time.RFC3339), // 1 month old
					"updated_at": time.Now().AddDate(0, 0, -1).Format(time.RFC3339),
				},
				{
					"owner": map[string]string{"login": "old"},
					"name": "repo", "full_name": "old/repo",
					"description": "Ancient", "language": "Go", "topics": []string{},
					"stargazers_count": 500, "forks_count": 50,
					"created_at": time.Now().AddDate(-1, 0, 0).Format(time.RFC3339), // 1 year old
					"updated_at": time.Now().AddDate(0, -6, 0).Format(time.RFC3339),
				},
			},
		}
	})
	defer mock.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(mock.server.URL)
	store := state.NewStore("")

	discoverer := NewDiscoverer(client, store, Config{
		Topics:             []string{"test"},
		MinStars:           100,
		MaxAgeDays:         90, // Only repos < 90 days old
		AutoTrackThreshold: 50.0,
	})

	result, err := discoverer.DiscoverTopic(context.Background(), "test")
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}

	if result.TotalFound != 2 {
		t.Errorf("expected 2 total found, got %d", result.TotalFound)
	}
	if result.AfterFilters != 1 {
		t.Errorf("expected 1 after age filter, got %d", result.AfterFilters)
	}
}

func TestDiscovery_SearchQueryFormat(t *testing.T) {
	var capturedQueries []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQueries = append(capturedQueries, r.URL.Query().Get("q"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"total_count": 0, "incomplete_results": false, "items": []interface{}{},
		})
	}))
	defer server.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(server.URL)
	store := state.NewStore("")

	discoverer := NewDiscoverer(client, store, Config{
		Topics:             []string{"kubernetes"},
		MinStars:           200,
		MaxAgeDays:         60,
		AutoTrackThreshold: 50.0,
	})

	discoverer.DiscoverTopic(context.Background(), "kubernetes")

	if len(capturedQueries) == 0 {
		t.Fatal("No search queries captured")
	}

	query := capturedQueries[0]
	t.Logf("Search query: %s", query)

	if !strings.Contains(query, "topic:kubernetes") {
		t.Errorf("Query missing topic filter: %s", query)
	}
	if !strings.Contains(query, "stars:>=200") {
		t.Errorf("Query missing stars filter: %s", query)
	}
	if !strings.Contains(query, "created:>=") {
		t.Errorf("Query missing created date filter: %s", query)
	}
}

func TestDiscovery_FullPipeline_DiscoverFilterTrack(t *testing.T) {
	mock := newMockSearchServer(func(query string) map[string]interface{} {
		return map[string]interface{}{
			"total_count":        3,
			"incomplete_results": false,
			"items": []map[string]interface{}{
				{
					"owner": map[string]string{"login": "hot"},
					"name": "project", "full_name": "hot/project",
					"description": "Hot project", "language": "Rust", "topics": []string{"wasm"},
					"stargazers_count": 2000, "forks_count": 200,
					"created_at": time.Now().AddDate(0, -1, 0).Format(time.RFC3339),
					"updated_at": time.Now().AddDate(0, 0, -1).Format(time.RFC3339),
				},
				{
					"owner": map[string]string{"login": "small"},
					"name": "lib", "full_name": "small/lib",
					"description": "Small lib", "language": "Go", "topics": []string{"wasm"},
					"stargazers_count": 50, "forks_count": 5, // Below MinStars
					"created_at": time.Now().AddDate(0, -1, 0).Format(time.RFC3339),
					"updated_at": time.Now().AddDate(0, 0, -1).Format(time.RFC3339),
				},
				{
					"owner": map[string]string{"login": "excluded"},
					"name": "tool", "full_name": "excluded/tool",
					"description": "Excluded tool", "language": "Go", "topics": []string{"wasm"},
					"stargazers_count": 500, "forks_count": 50,
					"created_at": time.Now().AddDate(0, -1, 0).Format(time.RFC3339),
					"updated_at": time.Now().AddDate(0, 0, -1).Format(time.RFC3339),
				},
			},
		}
	})
	defer mock.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(mock.server.URL)

	tmpDir := t.TempDir()
	store := state.NewStore(filepath.Join(tmpDir, "state.json"))

	discoverer := NewDiscoverer(client, store, Config{
		Topics:             []string{"wasm"},
		MinStars:           100,
		MaxAgeDays:         90,
		AutoTrackThreshold: 0.0, // Auto-track all passing repos
		Exclusions:         []string{"excluded/*"},
	})

	// Step 1: Discover
	result, err := discoverer.DiscoverTopic(context.Background(), "wasm")
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}

	t.Logf("Discovery: total=%d, after_filters=%d, new=%d, excluded=%d, auto_track=%d",
		result.TotalFound, result.AfterFilters, result.NewRepos, result.Excluded, result.AutoTracked)

	// 3 found, 1 filtered by stars, 1 excluded
	if result.TotalFound != 3 {
		t.Errorf("expected 3 total, got %d", result.TotalFound)
	}

	// Step 2: Auto-track
	tracked := discoverer.AutoTrack(result)
	t.Logf("Auto-tracked: %d repos", len(tracked))

	// Only hot/project should be auto-tracked
	// (small/lib filtered by stars, excluded/tool is excluded)
	for _, r := range tracked {
		if r.FullName == "excluded/tool" {
			t.Error("excluded/tool should not be auto-tracked")
		}
		if r.FullName == "small/lib" {
			t.Error("small/lib should not be auto-tracked (below min stars)")
		}
	}

	// Verify state was persisted
	hotState := store.GetRepoState("hot/project")
	if hotState == nil {
		t.Error("hot/project should be in state after auto-track")
	} else if hotState.Stars != 2000 {
		t.Errorf("hot/project stars = %d, want 2000", hotState.Stars)
	}

	// Step 3: Save and reload state
	if err := store.Save(); err != nil {
		t.Fatalf("state save failed: %v", err)
	}

	store2 := state.NewStore(filepath.Join(tmpDir, "state.json"))
	if err := store2.Load(); err != nil {
		t.Fatalf("state load failed: %v", err)
	}

	if store2.GetRepoState("hot/project") == nil {
		t.Error("hot/project not persisted after save/load")
	}
}

func TestDiscovery_MultiTopicDeduplication(t *testing.T) {
	// Same repo appears in multiple topic searches
	mock := newMockSearchServer(func(query string) map[string]interface{} {
		return map[string]interface{}{
			"total_count":        1,
			"incomplete_results": false,
			"items": []map[string]interface{}{
				{
					"owner": map[string]string{"login": "shared"},
					"name": "repo", "full_name": "shared/repo",
					"description": "Appears in all topics",
					"language": "Go", "topics": []string{"kubernetes", "ebpf"},
					"stargazers_count": 500, "forks_count": 50,
					"created_at": time.Now().AddDate(0, -1, 0).Format(time.RFC3339),
					"updated_at": time.Now().AddDate(0, 0, -1).Format(time.RFC3339),
				},
			},
		}
	})
	defer mock.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(mock.server.URL)
	store := state.NewStore("")

	discoverer := NewDiscoverer(client, store, Config{
		Topics:             []string{"kubernetes", "ebpf"},
		MinStars:           100,
		MaxAgeDays:         0,
		AutoTrackThreshold: 0.0,
	})

	results, err := discoverer.DiscoverAll(context.Background())
	if err != nil {
		t.Fatalf("discover all failed: %v", err)
	}

	// Auto-track from first topic
	if len(results) > 0 {
		discoverer.AutoTrack(results[0])
	}
	// Auto-track from second topic - should not duplicate
	if len(results) > 1 {
		tracked := discoverer.AutoTrack(results[1])
		t.Logf("Second topic auto-tracked: %d repos", len(tracked))
	}

	// State should have the repo only once
	count := store.RepoCount()
	if count != 1 {
		t.Errorf("Expected 1 repo in state (deduplicated), got %d", count)
	}
}

func TestDiscovery_PerPageLimit(t *testing.T) {
	var capturedPerPage string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPerPage = r.URL.Query().Get("per_page")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"total_count": 0, "incomplete_results": false, "items": []interface{}{},
		})
	}))
	defer server.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(server.URL)
	store := state.NewStore("")

	discoverer := NewDiscoverer(client, store, Config{
		Topics:   []string{"test"},
		MinStars: 100,
	})

	discoverer.DiscoverTopic(context.Background(), "test")

	if capturedPerPage == "" {
		t.Error("per_page parameter not sent")
	}

	t.Logf("per_page sent: %s", capturedPerPage)

	// Should request max 100 per page (GitHub API limit)
	if capturedPerPage != "100" {
		t.Errorf("Expected per_page=100, got %s", capturedPerPage)
	}
}

func TestDiscovery_LargeResultCount_NoOverflow(t *testing.T) {
	// Server claims huge total_count but returns only page worth
	mock := newMockSearchServer(func(query string) map[string]interface{} {
		items := make([]map[string]interface{}, 100)
		for i := 0; i < 100; i++ {
			items[i] = map[string]interface{}{
				"owner":            map[string]string{"login": fmt.Sprintf("org-%d", i)},
				"name":             fmt.Sprintf("repo-%d", i),
				"full_name":        fmt.Sprintf("org-%d/repo-%d", i, i),
				"description":      "Test repo",
				"language":         "Go",
				"topics":           []string{},
				"stargazers_count": 200 + i,
				"forks_count":      20 + i,
				"created_at":       time.Now().AddDate(0, -1, 0).Format(time.RFC3339),
				"updated_at":       time.Now().AddDate(0, 0, -1).Format(time.RFC3339),
			}
		}
		return map[string]interface{}{
			"total_count":        50000, // Claims 50k but only returns 100
			"incomplete_results": true,
			"items":              items,
		}
	})
	defer mock.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(mock.server.URL)
	store := state.NewStore("")

	discoverer := NewDiscoverer(client, store, Config{
		Topics:             []string{"popular"},
		MinStars:           100,
		MaxAgeDays:         0,
		AutoTrackThreshold: 50.0,
	})

	result, err := discoverer.DiscoverTopic(context.Background(), "popular")
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}

	// Should handle the 100 items we got, not try to fetch all 50k
	if result.TotalFound != 100 {
		t.Errorf("Expected 100 repos processed, got %d", result.TotalFound)
	}

	t.Logf("Handled large result set: %d items processed from claimed %d total",
		result.TotalFound, 50000)
}
