package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hrexed/github-radar/internal/state"
)

func TestScanner_Scan_Success(t *testing.T) {
	recent := time.Now().Add(-3 * 24 * time.Hour)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/test/repo"):
			w.Header().Set("ETag", `"abc123"`)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"owner": {"login": "test"},
				"name": "repo",
				"full_name": "test/repo",
				"stargazers_count": 150,
				"forks_count": 15,
				"open_issues_count": 5,
				"language": "Go",
				"topics": [],
				"description": ""
			}`))
		case strings.Contains(path, "/pulls"):
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[]`))
		case strings.Contains(path, "/issues"):
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[{"number": 1, "created_at": "` + recent.Format(time.RFC3339) + `"}]`))
		case strings.Contains(path, "/contributors"):
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[{"login": "user1"}]`))
		case strings.Contains(path, "/releases"):
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	tmpDir := t.TempDir()
	store := state.NewStore(filepath.Join(tmpDir, "state.json"))

	scanner := NewScanner(client, store)

	repos := []Repo{{Owner: "test", Name: "repo"}}
	result, err := scanner.Scan(context.Background(), repos)

	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	if result.Total != 1 {
		t.Errorf("Total = %d, want 1", result.Total)
	}
	if result.Successful != 1 {
		t.Errorf("Successful = %d, want 1", result.Successful)
	}
	if result.Failed != 0 {
		t.Errorf("Failed = %d, want 0", result.Failed)
	}

	// Verify state was saved
	repoState := store.GetRepoState("test/repo")
	if repoState == nil {
		t.Fatal("repo state not found")
	}
	if repoState.Stars != 150 {
		t.Errorf("Stars = %d, want 150", repoState.Stars)
	}
}

func TestScanner_Scan_VelocityCalculation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/test/repo") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"owner": {"login": "test"},
				"name": "repo",
				"full_name": "test/repo",
				"stargazers_count": 200,
				"forks_count": 20,
				"open_issues_count": 10,
				"language": "Go",
				"topics": [],
				"description": ""
			}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	tmpDir := t.TempDir()
	store := state.NewStore(filepath.Join(tmpDir, "state.json"))

	// Set up previous state
	prevTime := time.Now().Add(-7 * 24 * time.Hour) // 7 days ago
	store.SetRepoState("test/repo", state.RepoState{
		Owner:         "test",
		Name:          "repo",
		Stars:         100,
		LastCollected: prevTime,
		StarVelocity:  5.0, // 5 stars/day previously
	})

	scanner := NewScanner(client, store)
	scanner.collector.SetCollectActivity(false) // Simplify test

	repos := []Repo{{Owner: "test", Name: "repo"}}
	_, err := scanner.Scan(context.Background(), repos)

	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	repoState := store.GetRepoState("test/repo")
	if repoState == nil {
		t.Fatal("repo state not found")
	}

	// New stars: 200, Previous: 100, Delta: 100 over ~7 days
	// Velocity should be ~14.3 stars/day
	if repoState.StarVelocity < 10 || repoState.StarVelocity > 20 {
		t.Errorf("StarVelocity = %f, expected ~14", repoState.StarVelocity)
	}

	// Previous velocity was 5, new is ~14, so acceleration should be positive
	if repoState.StarAcceleration <= 0 {
		t.Errorf("StarAcceleration = %f, expected positive", repoState.StarAcceleration)
	}

	// Previous stars should be saved
	if repoState.StarsPrev != 100 {
		t.Errorf("StarsPrev = %d, want 100", repoState.StarsPrev)
	}
}

func TestScanner_Scan_ConditionalRequest(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/test/repo") {
			requestCount++
			// Check for conditional header
			if r.Header.Get("If-None-Match") == `"abc123"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"owner": {"login": "test"},
				"name": "repo",
				"full_name": "test/repo",
				"stargazers_count": 100,
				"forks_count": 10,
				"open_issues_count": 5,
				"language": "Go",
				"topics": [],
				"description": ""
			}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	tmpDir := t.TempDir()
	store := state.NewStore(filepath.Join(tmpDir, "state.json"))

	// Set up previous state with ETag
	store.SetRepoState("test/repo", state.RepoState{
		Owner:         "test",
		Name:          "repo",
		Stars:         100,
		LastCollected: time.Now().Add(-1 * time.Hour),
		ETag:          `"abc123"`,
	})

	scanner := NewScanner(client, store)

	repos := []Repo{{Owner: "test", Name: "repo"}}
	result, err := scanner.Scan(context.Background(), repos)

	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}

	if requestCount != 1 {
		t.Errorf("Request count = %d, want 1", requestCount)
	}
}

func TestScanner_Scan_PartialFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.Contains(path, "/good/") {
			if strings.HasSuffix(path, "/good/repo") {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{
					"owner": {"login": "good"},
					"name": "repo",
					"full_name": "good/repo",
					"stargazers_count": 50,
					"forks_count": 5,
					"open_issues_count": 2,
					"language": "Go",
					"topics": [],
					"description": ""
				}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[]`))
			return
		}
		if strings.Contains(path, "/bad/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	tmpDir := t.TempDir()
	store := state.NewStore(filepath.Join(tmpDir, "state.json"))

	scanner := NewScanner(client, store)
	scanner.collector.SetCollectActivity(false)

	repos := []Repo{
		{Owner: "good", Name: "repo"},
		{Owner: "bad", Name: "repo"},
	}
	result, err := scanner.Scan(context.Background(), repos)

	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	if result.Successful != 1 {
		t.Errorf("Successful = %d, want 1", result.Successful)
	}
	if result.Failed != 1 {
		t.Errorf("Failed = %d, want 1", result.Failed)
	}

	// Good repo should be in state
	if store.GetRepoState("good/repo") == nil {
		t.Error("good/repo should be in state")
	}
}

func TestScanner_Scan_StatePersisted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/test/repo") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"owner": {"login": "test"},
				"name": "repo",
				"full_name": "test/repo",
				"stargazers_count": 100,
				"forks_count": 10,
				"open_issues_count": 5,
				"language": "Go",
				"topics": [],
				"description": ""
			}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	store := state.NewStore(statePath)

	scanner := NewScanner(client, store)
	scanner.collector.SetCollectActivity(false)

	repos := []Repo{{Owner: "test", Name: "repo"}}
	_, err := scanner.Scan(context.Background(), repos)

	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	// Load state in a new store to verify persistence
	store2 := state.NewStore(statePath)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}

	repoState := store2.GetRepoState("test/repo")
	if repoState == nil {
		t.Fatal("repo state not persisted")
	}
	if repoState.Stars != 100 {
		t.Errorf("Stars = %d, want 100", repoState.Stars)
	}
}
