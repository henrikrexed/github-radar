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
