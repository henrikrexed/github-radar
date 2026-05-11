package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate_ValidConfig(t *testing.T) {
	cfg := &Config{
		GitHub: GithubConfig{
			Token:     "valid-token",
			RateLimit: 4000,
		},
		Otel: OtelConfig{
			Endpoint:    "http://localhost:4318",
			ServiceName: "test",
		},
		Discovery: DiscoveryConfig{
			Enabled:            true,
			MinStars:           100,
			MaxAgeDays:         90,
			AutoTrackThreshold: 50,
		},
		Scoring: ScoringConfig{
			Weights: WeightConfig{
				StarVelocity:      2.0,
				StarAcceleration:  3.0,
				ContributorGrowth: 1.5,
				PRVelocity:        1.0,
				IssueVelocity:     0.5,
			},
		},
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Validate() returned error for valid config: %v", err)
	}
}

func TestValidate_MissingToken(t *testing.T) {
	cfg := &Config{
		GitHub: GithubConfig{
			Token:     "",
			RateLimit: 4000,
		},
		Otel: OtelConfig{
			Endpoint: "http://localhost:4318",
		},
		Discovery: DiscoveryConfig{
			MinStars:   100,
			MaxAgeDays: 90,
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should return error for missing token")
	}

	valErr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("error should be *ValidationError, got %T", err)
	}

	found := false
	for _, issue := range valErr.Issues {
		if strings.Contains(issue, "github.token") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("error should mention github.token: %v", err)
	}
}

func TestValidate_MissingEndpoint(t *testing.T) {
	cfg := &Config{
		GitHub: GithubConfig{
			Token:     "token",
			RateLimit: 4000,
		},
		Otel: OtelConfig{
			Endpoint: "",
		},
		Discovery: DiscoveryConfig{
			MinStars:   100,
			MaxAgeDays: 90,
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should return error for missing endpoint")
	}

	if !strings.Contains(err.Error(), "otel.endpoint") {
		t.Errorf("error should mention otel.endpoint: %v", err)
	}
}

func TestValidate_InvalidEndpointURL(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantErr  string
	}{
		{
			name:     "invalid scheme",
			endpoint: "ftp://localhost:4318",
			wantErr:  "must use http:// or https://",
		},
		{
			name:     "missing host",
			endpoint: "http://",
			wantErr:  "missing host",
		},
		{
			name:     "no scheme",
			endpoint: "localhost:4318",
			wantErr:  "must use http:// or https://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				GitHub: GithubConfig{
					Token:     "token",
					RateLimit: 4000,
				},
				Otel: OtelConfig{
					Endpoint: tt.endpoint,
				},
				Discovery: DiscoveryConfig{
					MinStars:   100,
					MaxAgeDays: 90,
				},
			}

			err := cfg.Validate()
			if err == nil {
				t.Fatal("Validate() should return error for invalid endpoint")
			}

			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error should contain %q: %v", tt.wantErr, err)
			}
		})
	}
}

func TestValidate_ValidEndpointURLs(t *testing.T) {
	endpoints := []string{
		"http://localhost:4318",
		"https://otel.example.com",
		"http://192.168.1.100:4318",
		"https://otel.example.com:443/v1/metrics",
	}

	for _, endpoint := range endpoints {
		t.Run(endpoint, func(t *testing.T) {
			cfg := &Config{
				GitHub: GithubConfig{
					Token:     "token",
					RateLimit: 4000,
				},
				Otel: OtelConfig{
					Endpoint: endpoint,
				},
				Discovery: DiscoveryConfig{
					MinStars:   100,
					MaxAgeDays: 90,
				},
			}

			err := cfg.Validate()
			if err != nil {
				t.Errorf("Validate() returned error for valid endpoint %q: %v", endpoint, err)
			}
		})
	}
}

func TestValidate_InvalidRateLimit(t *testing.T) {
	cfg := &Config{
		GitHub: GithubConfig{
			Token:     "token",
			RateLimit: 0,
		},
		Otel: OtelConfig{
			Endpoint: "http://localhost:4318",
		},
		Discovery: DiscoveryConfig{
			MinStars:   100,
			MaxAgeDays: 90,
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should return error for invalid rate_limit")
	}

	if !strings.Contains(err.Error(), "rate_limit") {
		t.Errorf("error should mention rate_limit: %v", err)
	}
}

func TestValidate_NegativeMinStars(t *testing.T) {
	cfg := &Config{
		GitHub: GithubConfig{
			Token:     "token",
			RateLimit: 4000,
		},
		Otel: OtelConfig{
			Endpoint: "http://localhost:4318",
		},
		Discovery: DiscoveryConfig{
			MinStars:   -1,
			MaxAgeDays: 90,
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should return error for negative min_stars")
	}

	if !strings.Contains(err.Error(), "min_stars") {
		t.Errorf("error should mention min_stars: %v", err)
	}
}

func TestValidate_InvalidMaxAgeDays(t *testing.T) {
	cfg := &Config{
		GitHub: GithubConfig{
			Token:     "token",
			RateLimit: 4000,
		},
		Otel: OtelConfig{
			Endpoint: "http://localhost:4318",
		},
		Discovery: DiscoveryConfig{
			MinStars:   100,
			MaxAgeDays: 0,
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should return error for invalid max_age_days")
	}

	if !strings.Contains(err.Error(), "max_age_days") {
		t.Errorf("error should mention max_age_days: %v", err)
	}
}

func TestValidate_NegativeWeight(t *testing.T) {
	cfg := &Config{
		GitHub: GithubConfig{
			Token:     "token",
			RateLimit: 4000,
		},
		Otel: OtelConfig{
			Endpoint: "http://localhost:4318",
		},
		Discovery: DiscoveryConfig{
			MinStars:   100,
			MaxAgeDays: 90,
		},
		Scoring: ScoringConfig{
			Weights: WeightConfig{
				StarVelocity: -1.0,
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should return error for negative weight")
	}

	if !strings.Contains(err.Error(), "star_velocity") {
		t.Errorf("error should mention star_velocity: %v", err)
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	cfg := &Config{
		GitHub: GithubConfig{
			Token:     "",
			RateLimit: -1,
		},
		Otel: OtelConfig{
			Endpoint: "",
		},
		Discovery: DiscoveryConfig{
			MinStars:   -5,
			MaxAgeDays: 0,
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should return error")
	}

	valErr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("error should be *ValidationError, got %T", err)
	}

	// Should have multiple issues
	if len(valErr.Issues) < 4 {
		t.Errorf("expected at least 4 issues, got %d: %v", len(valErr.Issues), valErr.Issues)
	}

	// Error message should contain all issues
	errStr := err.Error()
	expectedFields := []string{"github.token", "github.rate_limit", "otel.endpoint", "discovery.min_stars"}
	for _, field := range expectedFields {
		if !strings.Contains(errStr, field) {
			t.Errorf("error should mention %s: %v", field, err)
		}
	}
}

func TestValidate_ZeroMinStarsAllowed(t *testing.T) {
	cfg := &Config{
		GitHub: GithubConfig{
			Token:     "token",
			RateLimit: 4000,
		},
		Otel: OtelConfig{
			Endpoint: "http://localhost:4318",
		},
		Discovery: DiscoveryConfig{
			MinStars:   0, // Zero should be allowed
			MaxAgeDays: 90,
		},
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Validate() should allow min_stars=0: %v", err)
	}
}

func TestValidate_ZeroAutoTrackThresholdAllowed(t *testing.T) {
	cfg := &Config{
		GitHub: GithubConfig{
			Token:     "token",
			RateLimit: 4000,
		},
		Otel: OtelConfig{
			Endpoint: "http://localhost:4318",
		},
		Discovery: DiscoveryConfig{
			MinStars:           100,
			MaxAgeDays:         90,
			AutoTrackThreshold: 0, // Zero should be allowed (disabled)
		},
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Validate() should allow auto_track_threshold=0: %v", err)
	}
}

func TestValidationError_Error(t *testing.T) {
	err := &ValidationError{
		Issues: []string{
			"github.token: required field is empty",
			"otel.endpoint: invalid URL format",
		},
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "configuration validation failed") {
		t.Errorf("error should contain header: %v", errStr)
	}
	if !strings.Contains(errStr, "github.token") {
		t.Errorf("error should contain first issue: %v", errStr)
	}
	if !strings.Contains(errStr, "otel.endpoint") {
		t.Errorf("error should contain second issue: %v", errStr)
	}
}

func TestValidationError_EmptyIssues(t *testing.T) {
	err := &ValidationError{Issues: []string{}}
	if err.Error() != "configuration validation failed" {
		t.Errorf("Error() = %q, want %q", err.Error(), "configuration validation failed")
	}
}

// validBaseConfig returns a Config that passes Validate(); tests below
// mutate only the discovery.sources.gharchive sub-config to exercise the
// new bound checks added in ISI-953.
func validBaseConfig() *Config {
	return &Config{
		GitHub: GithubConfig{
			Token:     "valid-token",
			RateLimit: 4000,
		},
		Otel: OtelConfig{
			Endpoint:    "http://localhost:4318",
			ServiceName: "test",
		},
		Discovery: DiscoveryConfig{
			Enabled:            true,
			MinStars:           100,
			MaxAgeDays:         90,
			AutoTrackThreshold: 50,
		},
	}
}

func TestValidate_GHArchiveDiscovery_DefaultsAreValid(t *testing.T) {
	cfg := DefaultConfig()
	cfg.GitHub.Token = "valid-token"
	cfg.Otel.Endpoint = "http://localhost:4318"

	if err := cfg.Validate(); err != nil {
		t.Errorf("DefaultConfig() should validate cleanly: %v", err)
	}
}

func TestValidate_GHArchiveDiscovery_AbsentSectionAccepted(t *testing.T) {
	// Zero values across the board mean "section omitted; runtime fills
	// defaults". Validate must not flag this.
	cfg := validBaseConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("absent gharchive sub-config must validate: %v", err)
	}
}

func TestValidate_GHArchiveDiscovery_NegativeValuesRejected(t *testing.T) {
	cases := map[string]func(*DiscoveryGHArchiveConfig){
		"window_hours":   func(g *DiscoveryGHArchiveConfig) { g.WindowHours = -1 },
		"top_n_per_hour": func(g *DiscoveryGHArchiveConfig) { g.TopNPerHour = -1 },
		"activity_floor": func(g *DiscoveryGHArchiveConfig) { g.ActivityFloor = -1 },
		"min_stars_gate": func(g *DiscoveryGHArchiveConfig) { g.MinStarsGate = -1 },
		"daily_cap_warn": func(g *DiscoveryGHArchiveConfig) { g.DailyCapWarn = -1 },
		"daily_cap_hard": func(g *DiscoveryGHArchiveConfig) { g.DailyCapHard = -1 },
	}

	for field, mutate := range cases {
		t.Run(field, func(t *testing.T) {
			cfg := validBaseConfig()
			mutate(&cfg.Discovery.Sources.GHArchive)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected error for negative %s", field)
			}
			if !strings.Contains(err.Error(), "gharchive."+field) {
				t.Errorf("error should mention discovery.sources.gharchive.%s: %v", field, err)
			}
		})
	}
}

func TestValidate_GHArchiveDiscovery_EnabledRequiresPositives(t *testing.T) {
	// Flag flip with zeros: every required positive int surfaces as a
	// specific issue so the operator knows what to fix.
	cfg := validBaseConfig()
	cfg.Discovery.Sources.GHArchive.Enabled = true

	err := cfg.Validate()
	if err == nil {
		t.Fatal("enabling gharchive without sizing knobs should fail validation")
	}
	for _, want := range []string{
		"window_hours",
		"top_n_per_hour",
		"daily_cap_warn",
		"daily_cap_hard",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should call out missing %s: %v", want, err)
		}
	}
}

// L1 ([ISI-965]): unknown event_types must be rejected only when the
// source is Enabled. Empty list is allowed; runtime fills the canonical
// default set.
func TestValidate_GHArchiveDiscovery_EventTypesAllowlist(t *testing.T) {
	t.Run("known_types_accepted_when_enabled", func(t *testing.T) {
		cfg := validBaseConfig()
		cfg.Discovery.Sources.GHArchive = DiscoveryGHArchiveConfig{
			Enabled:      true,
			WindowHours:  24,
			TopNPerHour:  500,
			DailyCapWarn: 4000,
			DailyCapHard: 5000,
			EventTypes:   []string{"WatchEvent", "ForkEvent", "PushEvent", "PullRequestEvent"},
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("canonical event_types must validate when enabled: %v", err)
		}
	})

	t.Run("unknown_type_rejected_when_enabled", func(t *testing.T) {
		cfg := validBaseConfig()
		cfg.Discovery.Sources.GHArchive = DiscoveryGHArchiveConfig{
			Enabled:      true,
			WindowHours:  24,
			TopNPerHour:  500,
			DailyCapWarn: 4000,
			DailyCapHard: 5000,
			EventTypes:   []string{"WatchEvent", "BogusEvent"},
		}
		err := cfg.Validate()
		if err == nil {
			t.Fatal("unknown event_types value must fail validation")
		}
		if !strings.Contains(err.Error(), "event_types") || !strings.Contains(err.Error(), "BogusEvent") {
			t.Errorf("error should call out unknown event_types value: %v", err)
		}
	})

	t.Run("unknown_type_ignored_when_disabled", func(t *testing.T) {
		// When the source is disabled, the event_types list is dormant
		// data; we deliberately don't lint it so an operator can stage
		// edits behind the disabled flag.
		cfg := validBaseConfig()
		cfg.Discovery.Sources.GHArchive.EventTypes = []string{"BogusEvent"}
		if err := cfg.Validate(); err != nil {
			t.Errorf("event_types must not be linted when source disabled: %v", err)
		}
	})

	t.Run("empty_event_types_accepted", func(t *testing.T) {
		// Empty list means "use the runtime default"; not an error
		// regardless of Enabled state.
		cfg := validBaseConfig()
		cfg.Discovery.Sources.GHArchive = DiscoveryGHArchiveConfig{
			Enabled:      true,
			WindowHours:  24,
			TopNPerHour:  500,
			DailyCapWarn: 4000,
			DailyCapHard: 5000,
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("empty event_types must validate: %v", err)
		}
	})
}

// L2 ([ISI-965]): WindowHours capped at 168 (one week). Beyond is
// backfill, out of scope.
func TestValidate_GHArchiveDiscovery_WindowHoursCap(t *testing.T) {
	cases := []struct {
		name        string
		windowHours int
		wantErr     bool
	}{
		{"under_cap", 24, false},
		{"at_cap", 168, false},
		{"over_cap", 169, true},
		{"way_over_cap", 720, true}, // 30 days
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validBaseConfig()
			cfg.Discovery.Sources.GHArchive.WindowHours = tc.windowHours
			err := cfg.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("WindowHours=%d should fail validation", tc.windowHours)
				}
				if !strings.Contains(err.Error(), "window_hours") {
					t.Errorf("error should mention window_hours: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("WindowHours=%d should validate: %v", tc.windowHours, err)
				}
			}
		})
	}
}

// L4 ([ISI-965]): end-to-end load of configs/github-radar.example.yaml
// and assert the gharchive sub-tree round-trips. Guards against schema
// drift between the example yaml and the Config struct tags.
func TestLoad_GHArchive_FromExampleYAML(t *testing.T) {
	// The example yaml lives at the repo root under configs/. From
	// the package test working dir (internal/config) that's two levels up.
	path := filepath.Join("..", "..", "configs", "github-radar.example.yaml")

	// Load() expands ${GITHUB_TOKEN}; provide a dummy value so
	// expansion succeeds. We're not exercising the github sub-tree
	// here — just verifying the gharchive YAML round-trips through
	// the loader. OTEL_ENDPOINT has a built-in ${VAR:-default}, so
	// only GITHUB_TOKEN needs to be set.
	t.Setenv("GITHUB_TOKEN", "test-token")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%s): %v", path, err)
	}

	ga := cfg.Discovery.Sources.GHArchive

	if ga.Enabled {
		t.Errorf("Enabled = %v, want false (example ships dark)", ga.Enabled)
	}
	if ga.WindowHours != 24 {
		t.Errorf("WindowHours = %d, want 24", ga.WindowHours)
	}
	if ga.TopNPerHour != 500 {
		t.Errorf("TopNPerHour = %d, want 500", ga.TopNPerHour)
	}
	if ga.ActivityFloor != 10 {
		t.Errorf("ActivityFloor = %d, want 10", ga.ActivityFloor)
	}
	if ga.MinStarsGate != 0 {
		t.Errorf("MinStarsGate = %d, want 0", ga.MinStarsGate)
	}
	if ga.DailyCapWarn != 4000 {
		t.Errorf("DailyCapWarn = %d, want 4000", ga.DailyCapWarn)
	}
	if ga.DailyCapHard != 5000 {
		t.Errorf("DailyCapHard = %d, want 5000", ga.DailyCapHard)
	}

	wantEventTypes := []string{"WatchEvent", "ForkEvent", "PushEvent", "PullRequestEvent"}
	if got := ga.EventTypes; len(got) != len(wantEventTypes) {
		t.Fatalf("EventTypes = %v, want %v", got, wantEventTypes)
	}
	for i, want := range wantEventTypes {
		if ga.EventTypes[i] != want {
			t.Errorf("EventTypes[%d] = %q, want %q", i, ga.EventTypes[i], want)
		}
	}
}

func TestValidate_GHArchiveDiscovery_WarnHardOrdering(t *testing.T) {
	cases := []struct {
		name string
		warn int
		hard int
		ok   bool
	}{
		{"warn<hard", 4000, 5000, true},
		{"warn==hard", 5000, 5000, false},
		{"warn>hard", 6000, 5000, false},
		{"warn=0,hard=5000", 0, 5000, true}, // 0 = unset, no cross-check
		{"warn=4000,hard=0", 4000, 0, true}, // 0 = unset, no cross-check
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validBaseConfig()
			cfg.Discovery.Sources.GHArchive.DailyCapWarn = tc.warn
			cfg.Discovery.Sources.GHArchive.DailyCapHard = tc.hard

			err := cfg.Validate()
			if tc.ok && err != nil {
				t.Errorf("expected valid, got: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected validation error")
			}
			if !tc.ok && !strings.Contains(err.Error(), "daily_cap_warn") {
				t.Errorf("error should mention daily_cap_warn: %v", err)
			}
		})
	}
}
