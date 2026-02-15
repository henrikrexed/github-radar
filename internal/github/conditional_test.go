package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_GetConditional_NoCondition(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should not have conditional headers
		if r.Header.Get("If-None-Match") != "" {
			t.Error("should not have If-None-Match header")
		}
		if r.Header.Get("If-Modified-Since") != "" {
			t.Error("should not have If-Modified-Since header")
		}

		w.Header().Set("ETag", `"abc123"`)
		w.Header().Set("Last-Modified", "Sat, 15 Feb 2026 12:00:00 GMT")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	resp, err := client.GetConditional(context.Background(), "/test", nil)
	if err != nil {
		t.Fatalf("GetConditional error: %v", err)
	}
	defer resp.Response.Body.Close()

	if resp.NotModified {
		t.Error("NotModified should be false")
	}
	if resp.Info.ETag != `"abc123"` {
		t.Errorf("ETag = %s, want \"abc123\"", resp.Info.ETag)
	}
	if resp.Info.LastModified != "Sat, 15 Feb 2026 12:00:00 GMT" {
		t.Errorf("LastModified = %s", resp.Info.LastModified)
	}
}

func TestClient_GetConditional_WithETag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != `"abc123"` {
			t.Errorf("If-None-Match = %s, want \"abc123\"", r.Header.Get("If-None-Match"))
		}

		// Return 304 Not Modified
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	resp, err := client.GetConditional(context.Background(), "/test", &ConditionalInfo{
		ETag: `"abc123"`,
	})
	if err != nil {
		t.Fatalf("GetConditional error: %v", err)
	}

	if !resp.NotModified {
		t.Error("NotModified should be true for 304")
	}
	if resp.Response != nil {
		t.Error("Response should be nil for 304")
	}
}

func TestClient_GetConditional_WithLastModified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-Modified-Since") != "Sat, 15 Feb 2026 12:00:00 GMT" {
			t.Errorf("If-Modified-Since = %s", r.Header.Get("If-Modified-Since"))
		}

		// Return 304 Not Modified
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	resp, err := client.GetConditional(context.Background(), "/test", &ConditionalInfo{
		LastModified: "Sat, 15 Feb 2026 12:00:00 GMT",
	})
	if err != nil {
		t.Fatalf("GetConditional error: %v", err)
	}

	if !resp.NotModified {
		t.Error("NotModified should be true for 304")
	}
}

func TestClient_GetConditional_Modified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Resource was modified, return new data
		w.Header().Set("ETag", `"def456"`)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"updated": true}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	resp, err := client.GetConditional(context.Background(), "/test", &ConditionalInfo{
		ETag: `"abc123"`,
	})
	if err != nil {
		t.Fatalf("GetConditional error: %v", err)
	}
	defer resp.Response.Body.Close()

	if resp.NotModified {
		t.Error("NotModified should be false when resource changed")
	}
	if resp.Info.ETag != `"def456"` {
		t.Errorf("new ETag = %s, want \"def456\"", resp.Info.ETag)
	}
}

func TestClient_GetRepositoryConditional_NotModified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	metrics, notModified, info, err := client.GetRepositoryConditional(
		context.Background(), "owner", "repo",
		&ConditionalInfo{ETag: `"abc123"`},
	)
	if err != nil {
		t.Fatalf("GetRepositoryConditional error: %v", err)
	}

	if !notModified {
		t.Error("notModified should be true")
	}
	if metrics != nil {
		t.Error("metrics should be nil for 304")
	}
	if info == nil {
		t.Error("info should not be nil")
	}
}

func TestClient_GetRepositoryConditional_Modified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"newetag"`)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"owner": {"login": "owner"},
			"name": "repo",
			"full_name": "owner/repo",
			"stargazers_count": 200,
			"forks_count": 20,
			"open_issues_count": 5,
			"language": "Go",
			"topics": [],
			"description": "Updated repo"
		}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	metrics, notModified, info, err := client.GetRepositoryConditional(
		context.Background(), "owner", "repo",
		&ConditionalInfo{ETag: `"oldetag"`},
	)
	if err != nil {
		t.Fatalf("GetRepositoryConditional error: %v", err)
	}

	if notModified {
		t.Error("notModified should be false")
	}
	if metrics == nil {
		t.Fatal("metrics should not be nil")
	}
	if metrics.Stars != 200 {
		t.Errorf("Stars = %d, want 200", metrics.Stars)
	}
	if info.ETag != `"newetag"` {
		t.Errorf("ETag = %s, want \"newetag\"", info.ETag)
	}
}

func TestClient_GetRepositoryConditional_NoCondition(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"firsetag"`)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"owner": {"login": "owner"},
			"name": "repo",
			"full_name": "owner/repo",
			"stargazers_count": 100,
			"forks_count": 10,
			"open_issues_count": 2,
			"language": "Python",
			"topics": ["ml"],
			"description": "ML repo"
		}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	metrics, notModified, info, err := client.GetRepositoryConditional(
		context.Background(), "owner", "repo", nil,
	)
	if err != nil {
		t.Fatalf("GetRepositoryConditional error: %v", err)
	}

	if notModified {
		t.Error("notModified should be false for first request")
	}
	if metrics == nil {
		t.Fatal("metrics should not be nil")
	}
	if info.ETag != `"firsetag"` {
		t.Errorf("ETag = %s", info.ETag)
	}
}
