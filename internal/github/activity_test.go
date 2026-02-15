package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_GetMergedPRsCount(t *testing.T) {
	// Create timestamps
	recent := time.Now().Add(-3 * 24 * time.Hour) // 3 days ago
	old := time.Now().Add(-14 * 24 * time.Hour)   // 14 days ago

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		// First request returns data, second returns empty to stop pagination
		if requestCount == 1 {
			w.Write([]byte(fmt.Sprintf(`[
				{"number": 1, "state": "closed", "merged_at": "%s"},
				{"number": 2, "state": "closed", "merged_at": "%s"},
				{"number": 3, "state": "closed", "merged_at": "%s"},
				{"number": 4, "state": "closed", "merged_at": null}
			]`, recent.Format(time.RFC3339), recent.Format(time.RFC3339), old.Format(time.RFC3339))))
		} else {
			w.Write([]byte(`[]`))
		}
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	count, err := client.GetMergedPRsCount(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("GetMergedPRsCount error: %v", err)
	}

	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestClient_GetMergedPRsCount_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	count, err := client.GetMergedPRsCount(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("GetMergedPRsCount error: %v", err)
	}

	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestClient_GetRecentIssuesCount(t *testing.T) {
	recent := time.Now().Add(-3 * 24 * time.Hour)
	old := time.Now().Add(-14 * 24 * time.Hour)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// 2 recent issues (1 is actually a PR), 1 old issue
		w.Write([]byte(fmt.Sprintf(`[
			{"number": 1, "created_at": "%s"},
			{"number": 2, "created_at": "%s", "pull_request": {}},
			{"number": 3, "created_at": "%s"}
		]`, recent.Format(time.RFC3339), recent.Format(time.RFC3339), old.Format(time.RFC3339))))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	count, err := client.GetRecentIssuesCount(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("GetRecentIssuesCount error: %v", err)
	}

	// Only count 1 - the recent one that isn't a PR
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestClient_GetContributorCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[
			{"login": "user1"},
			{"login": "user2"},
			{"login": "user3"}
		]`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	count, err := client.GetContributorCount(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("GetContributorCount error: %v", err)
	}

	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}

func TestClient_GetContributorCount_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	count, err := client.GetContributorCount(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("GetContributorCount error: %v", err)
	}

	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestClient_GetLatestRelease(t *testing.T) {
	publishedAt := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf(`{
			"tag_name": "v1.5.0",
			"name": "Release 1.5.0",
			"published_at": "%s",
			"html_url": "https://github.com/owner/repo/releases/tag/v1.5.0"
		}`, publishedAt.Format(time.RFC3339))))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	release, err := client.GetLatestRelease(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("GetLatestRelease error: %v", err)
	}

	if release == nil {
		t.Fatal("expected release, got nil")
	}
	if release.TagName != "v1.5.0" {
		t.Errorf("TagName = %s, want v1.5.0", release.TagName)
	}
	if release.Name != "Release 1.5.0" {
		t.Errorf("Name = %s, want Release 1.5.0", release.Name)
	}
	if !release.PublishedAt.Equal(publishedAt) {
		t.Errorf("PublishedAt = %v, want %v", release.PublishedAt, publishedAt)
	}
}

func TestClient_GetLatestRelease_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message": "Not Found"}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	release, err := client.GetLatestRelease(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("GetLatestRelease error: %v", err)
	}

	// No release should return nil, not error
	if release != nil {
		t.Errorf("expected nil release for 404, got %+v", release)
	}
}

func TestClient_GetActivityMetrics(t *testing.T) {
	recent := time.Now().Add(-3 * 24 * time.Hour)
	publishedAt := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)

	pullsRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/pulls":
			pullsRequests++
			w.WriteHeader(http.StatusOK)
			// Return data on first request, empty on subsequent (pagination)
			if pullsRequests == 1 {
				w.Write([]byte(fmt.Sprintf(`[{"number": 1, "state": "closed", "merged_at": "%s"}]`,
					recent.Format(time.RFC3339))))
			} else {
				w.Write([]byte(`[]`))
			}
		case r.URL.Path == "/repos/owner/repo/issues":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`[{"number": 1, "created_at": "%s"}]`,
				recent.Format(time.RFC3339))))
		case r.URL.Path == "/repos/owner/repo/contributors":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[{"login": "user1"}, {"login": "user2"}]`))
		case r.URL.Path == "/repos/owner/repo/releases/latest":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{
				"tag_name": "v1.0.0",
				"name": "Release 1.0",
				"published_at": "%s",
				"html_url": "https://github.com/owner/repo/releases/tag/v1.0.0"
			}`, publishedAt.Format(time.RFC3339))))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	metrics, err := client.GetActivityMetrics(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("GetActivityMetrics error: %v", err)
	}

	if metrics.MergedPRs7d != 1 {
		t.Errorf("MergedPRs7d = %d, want 1", metrics.MergedPRs7d)
	}
	if metrics.NewIssues7d != 1 {
		t.Errorf("NewIssues7d = %d, want 1", metrics.NewIssues7d)
	}
	if metrics.Contributors != 2 {
		t.Errorf("Contributors = %d, want 2", metrics.Contributors)
	}
	if metrics.LatestRelease == nil {
		t.Error("expected LatestRelease, got nil")
	} else if metrics.LatestRelease.TagName != "v1.0.0" {
		t.Errorf("LatestRelease.TagName = %s, want v1.0.0", metrics.LatestRelease.TagName)
	}
}
