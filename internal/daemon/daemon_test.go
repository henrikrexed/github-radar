package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hrexed/github-radar/internal/config"
)

func TestDefaultDaemonConfig(t *testing.T) {
	cfg := DefaultDaemonConfig()

	if cfg.Interval != 24*time.Hour {
		t.Errorf("expected 24h interval, got %v", cfg.Interval)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("expected :8080, got %s", cfg.HTTPAddr)
	}
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		{"foo/bar", "foo/bar", true},
		{"foo/bar", "foo/baz", false},
		{"foo/bar", "foo/*", true},
		{"foo/bar", "bar/*", false},
		{"org/repo", "org/*", true},
		{"other/repo", "org/*", false},
	}

	for _, tc := range tests {
		got := matchesPattern(tc.name, tc.pattern)
		if got != tc.want {
			t.Errorf("matchesPattern(%q, %q) = %v, want %v", tc.name, tc.pattern, got, tc.want)
		}
	}
}

func TestHealthEndpoint(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GithubConfig{
			Token:     "test-token",
			RateLimit: 4000,
		},
		Otel: config.OtelConfig{
			Endpoint:    "http://localhost:4318",
			ServiceName: "test",
		},
	}

	daemonCfg := DaemonConfig{
		Interval: time.Hour,
		HTTPAddr: ":0", // Random port
		DryRun:   true,
	}

	d, err := New(cfg, daemonCfg)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	// Test health endpoint
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	d.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if !resp["healthy"] {
		t.Error("expected healthy=true")
	}
}

func TestStatusEndpoint(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GithubConfig{
			Token:     "test-token",
			RateLimit: 4000,
		},
		Otel: config.OtelConfig{
			Endpoint:    "http://localhost:4318",
			ServiceName: "test",
		},
	}

	daemonCfg := DaemonConfig{
		Interval: time.Hour,
		HTTPAddr: ":0",
		DryRun:   true,
	}

	d, err := New(cfg, daemonCfg)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	// Test status endpoint
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()

	d.handleStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp StatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Status != string(StatusIdle) {
		t.Errorf("expected status idle, got %s", resp.Status)
	}

	if resp.Uptime == "" {
		t.Error("expected uptime to be set")
	}
}

func TestHealthEndpoint_Stopping(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GithubConfig{
			Token:     "test-token",
			RateLimit: 4000,
		},
		Otel: config.OtelConfig{
			Endpoint:    "http://localhost:4318",
			ServiceName: "test",
		},
	}

	daemonCfg := DaemonConfig{
		Interval: time.Hour,
		HTTPAddr: ":0",
		DryRun:   true,
	}

	d, err := New(cfg, daemonCfg)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	// Set status to stopping
	d.setStatus(StatusStopping)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	d.handleHealth(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}

	var resp map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["healthy"] {
		t.Error("expected healthy=false when stopping")
	}
}

func TestDaemon_IsExcluded(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GithubConfig{
			Token:     "test-token",
			RateLimit: 4000,
		},
		Otel: config.OtelConfig{
			Endpoint:    "http://localhost:4318",
			ServiceName: "test",
		},
		Exclusions: []string{
			"excluded/repo",
			"blocked-org/*",
		},
	}

	daemonCfg := DaemonConfig{
		Interval: time.Hour,
		HTTPAddr: ":0",
		DryRun:   true,
	}

	d, err := New(cfg, daemonCfg)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	tests := []struct {
		repo     string
		excluded bool
	}{
		{"excluded/repo", true},
		{"blocked-org/something", true},
		{"blocked-org/other", true},
		{"allowed/repo", false},
		{"other/repo", false},
	}

	for _, tc := range tests {
		got := d.isExcluded(tc.repo)
		if got != tc.excluded {
			t.Errorf("isExcluded(%q) = %v, want %v", tc.repo, got, tc.excluded)
		}
	}
}
