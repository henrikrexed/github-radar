package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/hrexed/github-radar/internal/config"
	"github.com/hrexed/github-radar/internal/database"
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
		{"any/repo", "*/repo", true},
		{"any/other", "*/repo", false},
		{"any/thing", "*/*", true},
	}

	for _, tc := range tests {
		got := MatchesPattern(tc.name, tc.pattern)
		if got != tc.want {
			t.Errorf("MatchesPattern(%q, %q) = %v, want %v", tc.name, tc.pattern, got, tc.want)
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

	// Mark daemon as ready (normally done by Run())
	d.setReady(true)

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

func TestHealthEndpoint_NotReady(t *testing.T) {
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

	// Daemon is not ready by default
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	d.handleHealth(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 when not ready, got %d", w.Code)
	}

	var resp map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["healthy"] {
		t.Error("expected healthy=false when not ready")
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

// withTempDBPath redirects database.DefaultDBPath at a fresh
// per-test temp directory so daemon.New does not litter the operator's
// XDG_DATA_HOME / ~/.local/share path. Restored at test cleanup.
func withTempDBPath(t *testing.T) {
	t.Helper()
	old := database.DefaultDBPath
	database.DefaultDBPath = filepath.Join(t.TempDir(), "scanner.db")
	t.Cleanup(func() { database.DefaultDBPath = old })
}

// gharchiveDaemonConfig builds a config.Config + DaemonConfig pair
// suitable for the integration tests below: discovery is enabled with
// a single placeholder topic so the daemon constructs a Discoverer,
// dry-run is on, and the rate-limit floor is non-zero.
func gharchiveDaemonConfig(gharchiveEnabled bool) (*config.Config, DaemonConfig) {
	cfg := &config.Config{
		GitHub: config.GithubConfig{
			Token:     "test-token",
			RateLimit: 4000,
		},
		Otel: config.OtelConfig{
			Endpoint:    "",
			ServiceName: "test",
		},
		Discovery: config.DiscoveryConfig{
			Enabled: true,
			Topics:  []string{"placeholder"},
			Sources: config.DiscoverySourcesConfig{
				GHArchive: config.DiscoveryGHArchiveConfig{
					Enabled:       gharchiveEnabled,
					WindowHours:   24,
					TopNPerHour:   500,
					ActivityFloor: 10,
					EventTypes:    []string{"WatchEvent"},
				},
			},
		},
	}
	return cfg, DaemonConfig{
		Interval: time.Hour,
		HTTPAddr: ":0",
		DryRun:   true,
	}
}

// TestDaemonNew_GHArchiveDiscovery_EnabledWiresSource — happy path of
// [ISI-967]: with discovery.sources.gharchive.enabled=true the daemon
// constructs a *discovery.GHArchiveSource and registers it on the
// Discoverer. We probe via the public DiscoverFromGHArchive contract:
// when the source is wired the function returns a non-nil *Result
// (TotalFound=0 because the in-memory collector has not processed any
// archives yet, so no live REST hydration is triggered).
func TestDaemonNew_GHArchiveDiscovery_EnabledWiresSource(t *testing.T) {
	withTempDBPath(t)
	cfg, dCfg := gharchiveDaemonConfig(true)

	d, err := New(cfg, dCfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if d.db != nil {
			_ = d.db.Close()
		}
	})

	if d.discoverer == nil {
		t.Fatal("d.discoverer = nil, want non-nil (Discovery.Enabled=true with one topic should construct a Discoverer)")
	}

	res, err := d.discoverer.DiscoverFromGHArchive(context.Background())
	if err != nil {
		t.Fatalf("DiscoverFromGHArchive err = %v, want nil", err)
	}
	if res == nil {
		t.Fatal("DiscoverFromGHArchive result = nil, want non-nil — gharchive source was not wired into the Discoverer")
	}
	if res.Topic != "gharchive" {
		t.Errorf("result.Topic = %q, want %q", res.Topic, "gharchive")
	}
	if res.TotalFound != 0 {
		t.Errorf("result.TotalFound = %d, want 0 (collector empty, no archives processed yet)", res.TotalFound)
	}
}

// TestDaemonNew_GHArchiveDiscovery_DisabledKeepsSourceUnwired —
// negative path of [ISI-967]: with the user-facing flag at its default
// false, the daemon must NOT call SetGHArchiveSource, preserving the
// pre-Path-C behaviour where DiscoverFromGHArchive returns
// (nil, nil).
func TestDaemonNew_GHArchiveDiscovery_DisabledKeepsSourceUnwired(t *testing.T) {
	withTempDBPath(t)
	cfg, dCfg := gharchiveDaemonConfig(false)

	d, err := New(cfg, dCfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if d.db != nil {
			_ = d.db.Close()
		}
	})

	if d.discoverer == nil {
		t.Fatal("d.discoverer = nil, want non-nil (Discovery.Enabled=true with one topic should construct a Discoverer)")
	}

	res, err := d.discoverer.DiscoverFromGHArchive(context.Background())
	if err != nil {
		t.Fatalf("DiscoverFromGHArchive err = %v, want nil", err)
	}
	if res != nil {
		t.Errorf("result = %+v, want nil — gharchive source must stay unwired when discovery.sources.gharchive.enabled=false", res)
	}
}

func TestIsExcluded(t *testing.T) {
	exclusions := []string{
		"excluded/repo",
		"blocked-org/*",
		"*/blocked-repo",
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
		{"any/blocked-repo", true},
		{"owner/blocked-repo", true},
	}

	for _, tc := range tests {
		got := isExcluded(tc.repo, exclusions)
		if got != tc.excluded {
			t.Errorf("isExcluded(%q) = %v, want %v", tc.repo, got, tc.excluded)
		}
	}
}
