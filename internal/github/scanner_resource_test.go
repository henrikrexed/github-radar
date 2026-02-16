package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/hrexed/github-radar/internal/state"
)

// newResourceTestServer creates a mock GitHub API server that responds to
// all standard scanner endpoints. It tracks request count via the counter pointer.
func newResourceTestServer(requestCount *int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestCount != nil {
			*requestCount++
		}
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.WriteHeader(http.StatusOK)
		// Return minimal valid responses based on path
		if r.URL.Path == "/rate_limit" {
			w.Write([]byte(`{"resources":{"core":{"limit":5000,"remaining":4999}}}`))
			return
		}
		if len(r.URL.Path) > 7 && r.URL.Query().Get("state") == "closed" {
			// PRs endpoint
			w.Write([]byte(`[]`))
			return
		}
		if r.URL.Query().Get("since") != "" {
			// Issues endpoint
			w.Write([]byte(`[]`))
			return
		}
		if r.URL.Query().Get("anon") != "" {
			// Contributors endpoint
			w.Write([]byte(`[{"login":"user1"}]`))
			return
		}
		if r.URL.Path != "" && len(r.URL.Path) > 1 {
			// Default repo endpoint
			w.Write([]byte(`{
				"owner": {"login": "owner"},
				"name": "repo",
				"full_name": "owner/repo",
				"stargazers_count": 100,
				"forks_count": 10,
				"open_issues_count": 5,
				"language": "Go",
				"topics": [],
				"description": "test"
			}`))
			return
		}
		w.Write([]byte(`{}`))
	}))
}

func TestScanner_MemoryBounded_100Repos(t *testing.T) {
	var requestCount int
	server := newResourceTestServer(&requestCount)
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	tmpDir := t.TempDir()
	store := state.NewStore(filepath.Join(tmpDir, "state.json"))

	scanner := NewScanner(client, store)
	scanner.collector.SetCollectActivity(false)
	scanner.collector.SetCollectPRs(false)

	// Build a list of 100 repos
	repos := make([]Repo, 100)
	for i := 0; i < 100; i++ {
		repos[i] = Repo{Owner: "owner", Name: fmt.Sprintf("repo-%d", i)}
	}

	// Force GC and measure baseline memory
	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	result, err := scanner.Scan(context.Background(), repos)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	// Verify scan completed
	if result.Total != 100 {
		t.Errorf("Total = %d, want 100", result.Total)
	}

	// Memory growth should be reasonable (< 50 MB for 100 repos)
	var memGrowthMB float64
	if memAfter.Alloc > memBefore.Alloc {
		memGrowthMB = float64(memAfter.Alloc-memBefore.Alloc) / 1024 / 1024
	}
	t.Logf("Memory growth for 100 repos: %.2f MB", memGrowthMB)
	t.Logf("Total alloc: %.2f MB", float64(memAfter.TotalAlloc)/1024/1024)
	t.Logf("Requests made: %d", requestCount)

	if memGrowthMB > 50 {
		t.Errorf("Memory growth %.2f MB exceeds 50 MB threshold for 100 repos", memGrowthMB)
	}
}

func TestScanner_MemoryBounded_500Repos(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large repo test in short mode")
	}

	server := newResourceTestServer(nil)
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	tmpDir := t.TempDir()
	store := state.NewStore(filepath.Join(tmpDir, "state.json"))

	scanner := NewScanner(client, store)
	scanner.collector.SetCollectActivity(false)
	scanner.collector.SetCollectPRs(false)

	repos := make([]Repo, 500)
	for i := 0; i < 500; i++ {
		repos[i] = Repo{Owner: "owner", Name: fmt.Sprintf("repo-%d", i)}
	}

	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	result, err := scanner.Scan(context.Background(), repos)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	if result.Total != 500 {
		t.Errorf("Total = %d, want 500", result.Total)
	}

	var memGrowthMB float64
	if memAfter.Alloc > memBefore.Alloc {
		memGrowthMB = float64(memAfter.Alloc-memBefore.Alloc) / 1024 / 1024
	}
	t.Logf("Memory growth for 500 repos: %.2f MB", memGrowthMB)

	// Memory should scale linearly, not exponentially (< 200 MB for 500 repos)
	if memGrowthMB > 200 {
		t.Errorf("Memory growth %.2f MB exceeds 200 MB threshold for 500 repos", memGrowthMB)
	}
}

func TestScanner_RequestCount_PerRepo(t *testing.T) {
	var requestCount int
	server := newResourceTestServer(&requestCount)
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	tmpDir := t.TempDir()
	store := state.NewStore(filepath.Join(tmpDir, "state.json"))

	scanner := NewScanner(client, store)

	repos := []Repo{{Owner: "owner", Name: "repo"}}
	_, err := scanner.Scan(context.Background(), repos)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	// With activity + PRs enabled, expect ~6 requests per repo:
	// 1 repo metadata + 1 PR count + 1 merged PRs + 1 issues + 1 contributors + 1 releases
	t.Logf("Requests per repo: %d", requestCount)
	if requestCount > 10 {
		t.Errorf("Too many requests per repo: %d (expected <= 10)", requestCount)
	}
}

func TestScanner_ScanDuration_Reasonable(t *testing.T) {
	server := newResourceTestServer(nil)
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	tmpDir := t.TempDir()
	store := state.NewStore(filepath.Join(tmpDir, "state.json"))

	scanner := NewScanner(client, store)
	scanner.collector.SetCollectActivity(false)
	scanner.collector.SetCollectPRs(false)

	repos := make([]Repo, 50)
	for i := 0; i < 50; i++ {
		repos[i] = Repo{Owner: "owner", Name: fmt.Sprintf("repo-%d", i)}
	}

	start := time.Now()
	result, err := scanner.Scan(context.Background(), repos)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	if result.Total != 50 {
		t.Errorf("Total = %d, want 50", result.Total)
	}

	// 50 repos with local mock server should complete in under 30 seconds
	t.Logf("Scan duration for 50 repos: %v", duration)
	if duration > 30*time.Second {
		t.Errorf("Scan took %v, expected < 30s for 50 repos against local mock", duration)
	}
}
