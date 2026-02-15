package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSearchRepositories(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/repositories" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Check query parameters
		query := r.URL.Query()
		if q := query.Get("q"); q != "topic:kubernetes stars:>=100" {
			t.Errorf("unexpected query: %s", q)
		}
		if sort := query.Get("sort"); sort != "stars" {
			t.Errorf("unexpected sort: %s", sort)
		}
		if order := query.Get("order"); order != "desc" {
			t.Errorf("unexpected order: %s", order)
		}

		resp := map[string]interface{}{
			"total_count":        2,
			"incomplete_results": false,
			"items": []map[string]interface{}{
				{
					"owner":            map[string]string{"login": "kubernetes"},
					"name":             "kubernetes",
					"full_name":        "kubernetes/kubernetes",
					"description":      "Production-Grade Container Scheduling and Management",
					"language":         "Go",
					"topics":           []string{"kubernetes", "containers"},
					"stargazers_count": 100000,
					"forks_count":      35000,
					"created_at":       "2014-06-06T22:56:04Z",
					"updated_at":       "2024-01-15T10:00:00Z",
				},
				{
					"owner":            map[string]string{"login": "helm"},
					"name":             "helm",
					"full_name":        "helm/helm",
					"description":      "The Kubernetes Package Manager",
					"language":         "Go",
					"topics":           []string{"kubernetes", "helm"},
					"stargazers_count": 25000,
					"forks_count":      7000,
					"created_at":       "2015-10-06T00:00:00Z",
					"updated_at":       "2024-01-14T08:00:00Z",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create client and point to test server
	client, err := NewClient("test-token")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	client.SetBaseURL(server.URL)

	// Execute search
	results, err := client.SearchRepositories(context.Background(), "topic:kubernetes stars:>=100", "stars", "desc", 100)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Verify first result
	if results[0].FullName != "kubernetes/kubernetes" {
		t.Errorf("expected kubernetes/kubernetes, got %s", results[0].FullName)
	}
	if results[0].Stars != 100000 {
		t.Errorf("expected 100000 stars, got %d", results[0].Stars)
	}
	if results[0].Language != "Go" {
		t.Errorf("expected Go language, got %s", results[0].Language)
	}

	// Verify second result
	if results[1].FullName != "helm/helm" {
		t.Errorf("expected helm/helm, got %s", results[1].FullName)
	}
}

func TestSearchRepositories_PerPageLimits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		perPage := r.URL.Query().Get("per_page")
		if perPage != "100" {
			t.Errorf("expected per_page=100, got %s", perPage)
		}

		resp := map[string]interface{}{
			"total_count":        0,
			"incomplete_results": false,
			"items":              []interface{}{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	// Test that perPage > 100 is capped to 100
	_, err := client.SearchRepositories(context.Background(), "test", "", "", 200)
	if err != nil {
		t.Errorf("search failed: %v", err)
	}
}

func TestSearchRepositories_DefaultPerPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		perPage := r.URL.Query().Get("per_page")
		if perPage != "30" {
			t.Errorf("expected per_page=30 (default), got %s", perPage)
		}

		resp := map[string]interface{}{
			"total_count":        0,
			"incomplete_results": false,
			"items":              []interface{}{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	// Test that perPage <= 0 uses default of 30
	_, err := client.SearchRepositories(context.Background(), "test", "", "", 0)
	if err != nil {
		t.Errorf("search failed: %v", err)
	}
}

func TestSearchResult_ParseDates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
					"stargazers_count": 100,
					"forks_count":      10,
					"created_at":       "2023-06-15T14:30:00Z",
					"updated_at":       "2024-01-10T09:15:30Z",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	results, err := client.SearchRepositories(context.Background(), "test", "", "", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Check dates were parsed correctly
	expectedCreated := time.Date(2023, 6, 15, 14, 30, 0, 0, time.UTC)
	if !results[0].CreatedAt.Equal(expectedCreated) {
		t.Errorf("expected created_at %v, got %v", expectedCreated, results[0].CreatedAt)
	}

	expectedUpdated := time.Date(2024, 1, 10, 9, 15, 30, 0, time.UTC)
	if !results[0].UpdatedAt.Equal(expectedUpdated) {
		t.Errorf("expected updated_at %v, got %v", expectedUpdated, results[0].UpdatedAt)
	}
}
