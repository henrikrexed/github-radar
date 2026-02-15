package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCollector_CollectRepo_Success(t *testing.T) {
	recent := time.Now().Add(-3 * 24 * time.Hour)
	pullsRequests := int32(0)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"owner": {"login": "owner"},
				"name": "repo",
				"full_name": "owner/repo",
				"stargazers_count": 100,
				"forks_count": 10,
				"open_issues_count": 5,
				"language": "Go",
				"topics": ["test"],
				"description": "Test repo"
			}`))
		case r.URL.Path == "/repos/owner/repo/pulls":
			atomic.AddInt32(&pullsRequests, 1)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[{"number": 1}, {"number": 2}]`))
		case r.URL.Path == "/repos/owner/repo/issues":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`[{"number": 1, "created_at": "%s"}]`,
				recent.Format(time.RFC3339))))
		case r.URL.Path == "/repos/owner/repo/contributors":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[{"login": "user1"}]`))
		case r.URL.Path == "/repos/owner/repo/releases/latest":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	collector := NewCollector(client)
	result := collector.CollectRepo(context.Background(), "owner", "repo")

	if result.Error != nil {
		t.Fatalf("CollectRepo error: %v", result.Error)
	}
	if result.Metrics == nil {
		t.Fatal("Metrics should not be nil")
	}
	if result.Metrics.Stars != 100 {
		t.Errorf("Stars = %d, want 100", result.Metrics.Stars)
	}
	if result.Metrics.OpenPRs != 2 {
		t.Errorf("OpenPRs = %d, want 2", result.Metrics.OpenPRs)
	}
	if result.Activity == nil {
		t.Fatal("Activity should not be nil")
	}
}

func TestCollector_CollectRepo_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message": "Not Found"}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	var errorCalled bool
	var receivedErr error
	collector := NewCollector(client)
	collector.SetErrorHandler(func(owner, name string, err error) {
		errorCalled = true
		receivedErr = err
	})

	result := collector.CollectRepo(context.Background(), "owner", "repo")

	if result.Error == nil {
		t.Error("expected error for 404")
	}
	if !errorCalled {
		t.Error("error handler should have been called")
	}

	// Check that it's classified as NotFound
	repoErr, ok := receivedErr.(*RepoError)
	if !ok {
		t.Fatalf("expected RepoError, got %T", receivedErr)
	}
	if repoErr.Type != RepoErrorNotFound {
		t.Errorf("error type = %v, want RepoErrorNotFound", repoErr.Type)
	}
}

func TestCollector_CollectAll(t *testing.T) {
	requestCounts := make(map[string]int32)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if strings.HasPrefix(path, "/repos/good/") {
			if strings.Contains(path, "/pulls") || strings.Contains(path, "/issues") ||
				strings.Contains(path, "/contributors") || strings.Contains(path, "/releases") {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`[]`))
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"owner": {"login": "good"},
				"name": "repo",
				"full_name": "good/repo",
				"stargazers_count": 50,
				"forks_count": 5,
				"open_issues_count": 2,
				"language": "Python",
				"topics": [],
				"description": ""
			}`))
			return
		}

		if strings.HasPrefix(path, "/repos/bad/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		t.Errorf("unexpected path: %s", path)
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	collector := NewCollector(client)
	collector.SetCollectActivity(false) // Disable to simplify test

	repos := []struct{ Owner, Name string }{
		{"good", "repo"},
		{"bad", "repo"},
	}

	summary := collector.CollectAll(context.Background(), repos)

	if summary.Total != 2 {
		t.Errorf("Total = %d, want 2", summary.Total)
	}
	if summary.Successful != 1 {
		t.Errorf("Successful = %d, want 1", summary.Successful)
	}
	if summary.Failed != 1 {
		t.Errorf("Failed = %d, want 1", summary.Failed)
	}
	if len(summary.FailedRepos) != 1 {
		t.Errorf("FailedRepos = %v, want 1 entry", summary.FailedRepos)
	}
	if len(summary.Results) != 2 {
		t.Errorf("Results = %d, want 2", len(summary.Results))
	}

	// Verify counts
	_ = requestCounts
}

func TestCollector_CollectAll_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	collector := NewCollector(client)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	repos := []struct{ Owner, Name string }{
		{"owner1", "repo1"},
		{"owner2", "repo2"},
	}

	summary := collector.CollectAll(ctx, repos)

	// All should fail due to context cancellation
	// The exact failure count depends on timing, but all should be failed
	if summary.Failed == 0 {
		t.Error("Expected some failures due to context cancellation")
	}
	if summary.Successful > 0 {
		t.Errorf("Successful = %d, expected 0 with cancelled context", summary.Successful)
	}
}

func TestCollector_DisablePRCollection(t *testing.T) {
	var prEndpointCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/pulls") {
			prEndpointCalled = true
		}
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
	collector.SetCollectPRs(false)
	collector.SetCollectActivity(false)

	result := collector.CollectRepo(context.Background(), "owner", "repo")

	if result.Error != nil {
		t.Fatalf("CollectRepo error: %v", result.Error)
	}
	if prEndpointCalled {
		t.Error("PR endpoint should not be called when disabled")
	}
}

func TestRepoError(t *testing.T) {
	err := &RepoError{
		Repo:    "owner/repo",
		Type:    RepoErrorNotFound,
		Message: "repository not found",
		Err:     fmt.Errorf("HTTP 404"),
	}

	if !strings.Contains(err.Error(), "owner/repo") {
		t.Error("error should contain repo name")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Error("error should contain message")
	}
	if err.Unwrap() == nil {
		t.Error("Unwrap should return underlying error")
	}
}
