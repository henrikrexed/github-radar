package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{"valid token", "ghp_test123", false},
		{"empty token", "", true},
		{"whitespace only", "   ", true},
		{"token with whitespace", "  ghp_test123  ", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewClient(tc.token)
			if tc.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if client == nil {
				t.Error("expected client, got nil")
			}
		})
	}
}

func TestClient_ValidateToken_Success(t *testing.T) {
	resetTime := time.Now().Add(time.Hour).Unix()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("expected Authorization header 'Bearer test-token', got '%s'", auth)
		}
		if accept := r.Header.Get("Accept"); accept != "application/vnd.github.v3+json" {
			t.Errorf("expected Accept header, got '%s'", accept)
		}
		if ua := r.Header.Get("User-Agent"); ua != UserAgent {
			t.Errorf("expected User-Agent '%s', got '%s'", UserAgent, ua)
		}

		// Set rate limit headers
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime, 10))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"resources":{}}`))
	}))
	defer server.Close()

	client, err := NewClient("test-token")
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}
	client.SetBaseURL(server.URL)

	err = client.ValidateToken(context.Background())
	if err != nil {
		t.Errorf("ValidateToken error: %v", err)
	}

	// Check rate limit was parsed
	rl := client.RateLimitInfo()
	if rl.Limit != 5000 {
		t.Errorf("RateLimit.Limit = %d, want 5000", rl.Limit)
	}
	if rl.Remaining != 4999 {
		t.Errorf("RateLimit.Remaining = %d, want 4999", rl.Remaining)
	}
	if rl.Reset.Unix() != resetTime {
		t.Errorf("RateLimit.Reset = %v, want %v", rl.Reset.Unix(), resetTime)
	}
}

func TestClient_ValidateToken_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message": "Bad credentials"}`))
	}))
	defer server.Close()

	client, _ := NewClient("bad-token")
	client.SetBaseURL(server.URL)

	err := client.ValidateToken(context.Background())
	if err == nil {
		t.Error("expected error for unauthorized token")
	}
}

func TestClient_ValidateToken_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "Server error"}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	err := client.ValidateToken(context.Background())
	if err == nil {
		t.Error("expected error for server error response")
	}
}

func TestClient_Get(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("X-RateLimit-Remaining", "4998")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id": 123}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	resp, err := client.Get(context.Background(), "/repos/owner/repo")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// Check rate limit was updated
	rl := client.RateLimitInfo()
	if rl.Remaining != 4998 {
		t.Errorf("RateLimit.Remaining = %d, want 4998", rl.Remaining)
	}
}

func TestClient_GetJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"name": "test-repo", "stars": 100}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	var result struct {
		Name  string `json:"name"`
		Stars int    `json:"stars"`
	}

	err := client.GetJSON(context.Background(), "/repos/owner/repo", &result)
	if err != nil {
		t.Fatalf("GetJSON error: %v", err)
	}

	if result.Name != "test-repo" {
		t.Errorf("Name = %s, want test-repo", result.Name)
	}
	if result.Stars != 100 {
		t.Errorf("Stars = %d, want 100", result.Stars)
	}
}

func TestClient_GetJSON_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message": "Not Found"}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	var result struct{}
	err := client.GetJSON(context.Background(), "/repos/owner/repo", &result)
	if err == nil {
		t.Error("expected error for 404 response")
	}
}

func TestClient_RateLimitParsing(t *testing.T) {
	tests := []struct {
		name      string
		headers   map[string]string
		wantLimit int
		wantRem   int
	}{
		{
			name: "all headers present",
			headers: map[string]string{
				"X-RateLimit-Limit":     "5000",
				"X-RateLimit-Remaining": "4500",
			},
			wantLimit: 5000,
			wantRem:   4500,
		},
		{
			name: "missing headers",
			headers: map[string]string{
				"Content-Type": "application/json",
			},
			wantLimit: 0,
			wantRem:   0,
		},
		{
			name: "invalid values",
			headers: map[string]string{
				"X-RateLimit-Limit":     "not-a-number",
				"X-RateLimit-Remaining": "also-not-a-number",
			},
			wantLimit: 0,
			wantRem:   0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{}`))
			}))
			defer server.Close()

			client, _ := NewClient("test-token")
			client.SetBaseURL(server.URL)

			_, err := client.Get(context.Background(), "/test")
			if err != nil {
				t.Fatalf("Get error: %v", err)
			}

			rl := client.RateLimitInfo()
			if rl.Limit != tc.wantLimit {
				t.Errorf("Limit = %d, want %d", rl.Limit, tc.wantLimit)
			}
			if rl.Remaining != tc.wantRem {
				t.Errorf("Remaining = %d, want %d", rl.Remaining, tc.wantRem)
			}
		})
	}
}

func TestClient_BaseURL(t *testing.T) {
	client, _ := NewClient("test-token")

	if client.BaseURL() != DefaultBaseURL {
		t.Errorf("BaseURL = %s, want %s", client.BaseURL(), DefaultBaseURL)
	}

	client.SetBaseURL("https://custom.github.example.com/api/")
	if client.BaseURL() != "https://custom.github.example.com/api" {
		t.Errorf("BaseURL after SetBaseURL = %s", client.BaseURL())
	}
}

func TestClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.Get(ctx, "/test")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}
