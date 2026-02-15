package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_GetRepository(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/kubernetes/kubernetes" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"owner": {"login": "kubernetes"},
			"name": "kubernetes",
			"full_name": "kubernetes/kubernetes",
			"stargazers_count": 105000,
			"forks_count": 38000,
			"open_issues_count": 2500,
			"language": "Go",
			"topics": ["kubernetes", "containers", "orchestration"],
			"description": "Production-Grade Container Scheduling and Management"
		}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	metrics, err := client.GetRepository(context.Background(), "kubernetes", "kubernetes")
	if err != nil {
		t.Fatalf("GetRepository error: %v", err)
	}

	if metrics.Owner != "kubernetes" {
		t.Errorf("Owner = %s, want kubernetes", metrics.Owner)
	}
	if metrics.Name != "kubernetes" {
		t.Errorf("Name = %s, want kubernetes", metrics.Name)
	}
	if metrics.FullName != "kubernetes/kubernetes" {
		t.Errorf("FullName = %s, want kubernetes/kubernetes", metrics.FullName)
	}
	if metrics.Stars != 105000 {
		t.Errorf("Stars = %d, want 105000", metrics.Stars)
	}
	if metrics.Forks != 38000 {
		t.Errorf("Forks = %d, want 38000", metrics.Forks)
	}
	if metrics.OpenIssues != 2500 {
		t.Errorf("OpenIssues = %d, want 2500", metrics.OpenIssues)
	}
	if metrics.Language != "Go" {
		t.Errorf("Language = %s, want Go", metrics.Language)
	}
	if len(metrics.Topics) != 3 {
		t.Errorf("Topics length = %d, want 3", len(metrics.Topics))
	}
}

func TestClient_GetRepository_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message": "Not Found"}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	_, err := client.GetRepository(context.Background(), "nonexistent", "repo")
	if err == nil {
		t.Error("expected error for 404 response")
	}
}

func TestClient_GetRepository_NullLanguage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"owner": {"login": "test"},
			"name": "repo",
			"full_name": "test/repo",
			"stargazers_count": 100,
			"forks_count": 10,
			"open_issues_count": 5,
			"language": null,
			"topics": [],
			"description": "A test repo"
		}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	metrics, err := client.GetRepository(context.Background(), "test", "repo")
	if err != nil {
		t.Fatalf("GetRepository error: %v", err)
	}

	if metrics.Language != "" {
		t.Errorf("Language = %s, want empty string", metrics.Language)
	}
}

func TestClient_GetOpenPRCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/pulls" {
			w.WriteHeader(http.StatusOK)
			// Return 3 open PRs
			w.Write([]byte(`[{"number": 1}, {"number": 2}, {"number": 3}]`))
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[]`))
		}
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	count, err := client.GetOpenPRCount(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("GetOpenPRCount error: %v", err)
	}

	if count != 3 {
		t.Errorf("PR count = %d, want 3", count)
	}
}

func TestClient_GetOpenPRCount_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	count, err := client.GetOpenPRCount(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("GetOpenPRCount error: %v", err)
	}

	if count != 0 {
		t.Errorf("PR count = %d, want 0", count)
	}
}

func TestClient_GetRepositoryWithPRs(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.URL.Path == "/repos/owner/repo" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"owner": {"login": "owner"},
				"name": "repo",
				"full_name": "owner/repo",
				"stargazers_count": 500,
				"forks_count": 50,
				"open_issues_count": 20,
				"language": "Python",
				"topics": ["ml", "ai"],
				"description": "ML repo"
			}`))
		} else if r.URL.Path == "/repos/owner/repo/pulls" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[{"number": 1}, {"number": 2}]`))
		} else {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	metrics, err := client.GetRepositoryWithPRs(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("GetRepositoryWithPRs error: %v", err)
	}

	if metrics.Stars != 500 {
		t.Errorf("Stars = %d, want 500", metrics.Stars)
	}
	if metrics.OpenPRs != 2 {
		t.Errorf("OpenPRs = %d, want 2", metrics.OpenPRs)
	}
	if metrics.Language != "Python" {
		t.Errorf("Language = %s, want Python", metrics.Language)
	}
}

func TestClient_GetRepository_RateLimitTracking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4500")
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
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	_, err := client.GetRepository(context.Background(), "test", "repo")
	if err != nil {
		t.Fatalf("GetRepository error: %v", err)
	}

	rl := client.RateLimitInfo()
	if rl.Limit != 5000 {
		t.Errorf("RateLimit.Limit = %d, want 5000", rl.Limit)
	}
	if rl.Remaining != 4500 {
		t.Errorf("RateLimit.Remaining = %d, want 4500", rl.Remaining)
	}
}
