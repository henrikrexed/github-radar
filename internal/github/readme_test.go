package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetReadme_Success(t *testing.T) {
	readme := "# My Project\n\nThis is a test README."
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/readme" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if accept := r.Header.Get("Accept"); accept != "application/vnd.github.raw" {
			t.Errorf("expected raw accept header, got %s", accept)
		}
		w.Header().Set("ETag", `"abc123"`)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(readme))
	}))
	defer srv.Close()

	client, err := NewClient("test-token")
	if err != nil {
		t.Fatal(err)
	}
	client.SetBaseURL(srv.URL)

	resp, err := client.GetReadme(context.Background(), "owner", "repo", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Found {
		t.Error("expected Found=true")
	}
	if resp.NotModified {
		t.Error("expected NotModified=false")
	}
	if resp.Content != readme {
		t.Errorf("content = %q, want %q", resp.Content, readme)
	}
	if resp.ETag != `"abc123"` {
		t.Errorf("etag = %q, want %q", resp.ETag, `"abc123"`)
	}
}

func TestGetReadme_NotModified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if etag := r.Header.Get("If-None-Match"); etag != `"abc123"` {
			t.Errorf("expected If-None-Match header, got %q", etag)
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	client, err := NewClient("test-token")
	if err != nil {
		t.Fatal(err)
	}
	client.SetBaseURL(srv.URL)

	resp, err := client.GetReadme(context.Background(), "owner", "repo", `"abc123"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.NotModified {
		t.Error("expected NotModified=true")
	}
	if !resp.Found {
		t.Error("expected Found=true")
	}
	if resp.Content != "" {
		t.Errorf("expected empty content, got %q", resp.Content)
	}
}

func TestGetReadme_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()

	client, err := NewClient("test-token")
	if err != nil {
		t.Fatal(err)
	}
	client.SetBaseURL(srv.URL)

	resp, err := client.GetReadme(context.Background(), "owner", "repo", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Found {
		t.Error("expected Found=false")
	}
}

func TestGetReadme_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	client, err := NewClient("test-token")
	if err != nil {
		t.Fatal(err)
	}
	client.SetBaseURL(srv.URL)

	_, err = client.GetReadme(context.Background(), "owner", "repo", "")
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}
