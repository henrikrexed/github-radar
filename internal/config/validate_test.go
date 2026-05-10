package config

import (
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
