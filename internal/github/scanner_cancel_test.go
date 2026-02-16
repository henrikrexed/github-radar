package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hrexed/github-radar/internal/state"
)

func TestScanner_ContextCancel_StopsMidScan(t *testing.T) {
	var processedRepos atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		processedRepos.Add(1)
		// Add a small delay to simulate real API calls
		time.Sleep(10 * time.Millisecond)

		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"owner": {"login": "owner"},
			"name": "repo",
			"full_name": "owner/repo",
			"stargazers_count": 100,
			"forks_count": 10,
			"open_issues_count": 5,
			"language": "Go",
			"topics": [],
			"description": ""
		}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	tmpDir := t.TempDir()
	store := state.NewStore(filepath.Join(tmpDir, "state.json"))

	scanner := NewScanner(client, store)
	scanner.collector.SetCollectActivity(false)
	scanner.collector.SetCollectPRs(false)

	// Create 100 repos to scan
	repos := make([]Repo, 100)
	for i := 0; i < 100; i++ {
		repos[i] = Repo{Owner: "owner", Name: fmt.Sprintf("repo-%d", i)}
	}

	// Cancel after a short time
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	result, err := scanner.Scan(ctx, repos)

	if err != context.DeadlineExceeded {
		t.Errorf("Expected context.DeadlineExceeded, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected partial result, got nil")
	}

	// Should have processed some repos but not all 100
	total := result.Successful + result.Failed
	t.Logf("Processed %d repos before cancellation (API calls: %d)", total, processedRepos.Load())

	if total >= 100 {
		t.Error("Expected scan to stop before processing all 100 repos")
	}
}

func TestScanner_ContextCancel_NoGoroutineLeak(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"owner": {"login": "owner"},
			"name": "repo",
			"full_name": "owner/repo",
			"stargazers_count": 100,
			"forks_count": 10,
			"open_issues_count": 5,
			"language": "Go",
			"topics": [],
			"description": ""
		}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	// Record goroutine count before
	goroutinesBefore := runtime.NumGoroutine()

	for i := 0; i < 5; i++ {
		tmpDir := t.TempDir()
		store := state.NewStore(filepath.Join(tmpDir, "state.json"))

		scanner := NewScanner(client, store)
		scanner.collector.SetCollectActivity(false)
		scanner.collector.SetCollectPRs(false)

		repos := make([]Repo, 20)
		for j := 0; j < 20; j++ {
			repos[j] = Repo{Owner: "owner", Name: fmt.Sprintf("repo-%d", j)}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		scanner.Scan(ctx, repos)
		cancel()
	}

	// Give goroutines time to clean up
	time.Sleep(500 * time.Millisecond)
	runtime.GC()

	goroutinesAfter := runtime.NumGoroutine()
	leaked := goroutinesAfter - goroutinesBefore

	t.Logf("Goroutines: before=%d, after=%d, diff=%d", goroutinesBefore, goroutinesAfter, leaked)

	// Allow a small margin for background goroutines, but shouldn't leak many
	if leaked > 5 {
		t.Errorf("Possible goroutine leak: %d goroutines leaked after 5 cancelled scans", leaked)
	}
}

func TestScanner_ContextCancel_StateConsistent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"owner": {"login": "owner"},
			"name": "repo",
			"full_name": "owner/repo",
			"stargazers_count": 100,
			"forks_count": 10,
			"open_issues_count": 5,
			"language": "Go",
			"topics": [],
			"description": ""
		}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	store := state.NewStore(statePath)

	scanner := NewScanner(client, store)
	scanner.collector.SetCollectActivity(false)
	scanner.collector.SetCollectPRs(false)

	repos := make([]Repo, 50)
	for i := 0; i < 50; i++ {
		repos[i] = Repo{Owner: "owner", Name: fmt.Sprintf("repo-%d", i)}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	scanner.Scan(ctx, repos)

	// State should be consistent - repos that were processed should have valid data
	allStates := store.AllRepoStates()
	for name, repoState := range allStates {
		if repoState.Stars <= 0 {
			t.Errorf("Repo %s has invalid stars: %d", name, repoState.Stars)
		}
		if repoState.Owner == "" {
			t.Errorf("Repo %s has empty owner", name)
		}
	}

	t.Logf("State contains %d repos after cancelled scan", len(allStates))
}

func TestCollector_ContextCancel_StopsCollection(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"owner": {"login": "owner"},
			"name": "repo",
			"full_name": "owner/repo",
			"stargazers_count": 100,
			"forks_count": 10,
			"open_issues_count": 5,
			"language": "Go",
			"topics": [],
			"description": ""
		}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	collector := NewCollector(client)
	collector.SetCollectActivity(false)
	collector.SetCollectPRs(false)

	repos := make([]struct{ Owner, Name string }, 50)
	for i := 0; i < 50; i++ {
		repos[i] = struct{ Owner, Name string }{
			Owner: "owner",
			Name:  fmt.Sprintf("repo-%d", i),
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	summary := collector.CollectAll(ctx, repos)

	t.Logf("CollectAll: successful=%d, failed=%d, total=%d, API calls=%d",
		summary.Successful, summary.Failed, summary.Total, requestCount.Load())

	// Some repos should have been cancelled
	if summary.Failed == 0 && summary.Successful == 50 {
		t.Error("Expected some repos to be cancelled, but all succeeded")
	}
}
