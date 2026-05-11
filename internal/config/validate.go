package config

import (
	"fmt"
	"net/url"
	"strings"
)

// ValidationError contains a list of configuration validation issues.
type ValidationError struct {
	Issues []string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	if len(e.Issues) == 0 {
		return "configuration validation failed"
	}
	var sb strings.Builder
	sb.WriteString("configuration validation failed:\n")
	for _, issue := range e.Issues {
		sb.WriteString("  - ")
		sb.WriteString(issue)
		sb.WriteString("\n")
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// Validate checks the configuration for errors.
// Returns nil if valid, or ValidationError with all issues.
func (c *Config) Validate() error {
	var issues []string

	// Required fields
	if c.GitHub.Token == "" {
		issues = append(issues, "github.token: required field is empty")
	}

	if c.Otel.Endpoint == "" {
		issues = append(issues, "otel.endpoint: required field is empty")
	} else {
		// Validate URL format
		parsedURL, err := url.Parse(c.Otel.Endpoint)
		if err != nil {
			issues = append(issues, fmt.Sprintf("otel.endpoint: invalid URL format: %v", err))
		} else if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			issues = append(issues, fmt.Sprintf("otel.endpoint: must use http:// or https:// scheme, got %q", parsedURL.Scheme))
		} else if parsedURL.Host == "" {
			issues = append(issues, "otel.endpoint: missing host")
		}
	}

	// Value constraints
	if c.GitHub.RateLimit <= 0 {
		issues = append(issues, fmt.Sprintf("github.rate_limit: must be greater than 0, got %d", c.GitHub.RateLimit))
	}

	if c.Discovery.MinStars < 0 {
		issues = append(issues, fmt.Sprintf("discovery.min_stars: must be >= 0, got %d", c.Discovery.MinStars))
	}

	if c.Discovery.MaxAgeDays <= 0 {
		issues = append(issues, fmt.Sprintf("discovery.max_age_days: must be greater than 0, got %d", c.Discovery.MaxAgeDays))
	}

	if c.Discovery.AutoTrackThreshold < 0 {
		issues = append(issues, fmt.Sprintf("discovery.auto_track_threshold: must be >= 0, got %.1f", c.Discovery.AutoTrackThreshold))
	}

	// discovery.sources.gharchive (Path C — ISI-950).
	// Validation rules:
	//   - Zero on a positive-int field means "unset; runtime fills from
	//     DefaultConfig()". So zero is always accepted here.
	//   - Negative values are always errors (real misconfig).
	//   - When the source is Enabled, zero on a required positive int
	//     becomes a hard error so a flag flip never lights up an
	//     unconfigured source.
	//   - Cross-field invariant (warn < hard) is only checked when both
	//     are explicitly set so partially specified blocks still pass.
	ga := c.Discovery.Sources.GHArchive
	if ga.WindowHours < 0 {
		issues = append(issues, fmt.Sprintf("discovery.sources.gharchive.window_hours: must be >= 0 (0 = use default 24), got %d", ga.WindowHours))
	}
	if ga.TopNPerHour < 0 {
		issues = append(issues, fmt.Sprintf("discovery.sources.gharchive.top_n_per_hour: must be >= 0 (0 = use default 500), got %d", ga.TopNPerHour))
	}
	if ga.ActivityFloor < 0 {
		issues = append(issues, fmt.Sprintf("discovery.sources.gharchive.activity_floor: must be >= 0, got %d", ga.ActivityFloor))
	}
	if ga.MinStarsGate < 0 {
		issues = append(issues, fmt.Sprintf("discovery.sources.gharchive.min_stars_gate: must be >= 0, got %d", ga.MinStarsGate))
	}
	if ga.DailyCapWarn < 0 {
		issues = append(issues, fmt.Sprintf("discovery.sources.gharchive.daily_cap_warn: must be >= 0 (0 = use default 4000), got %d", ga.DailyCapWarn))
	}
	if ga.DailyCapHard < 0 {
		issues = append(issues, fmt.Sprintf("discovery.sources.gharchive.daily_cap_hard: must be >= 0 (0 = use default 5000), got %d", ga.DailyCapHard))
	}
	if ga.Enabled {
		// MinStarsGate intentionally omitted from the required-when-enabled set: the ISI-950 Q3 decision keeps the star floor opt-in (0 = disabled by default) so event volume alone drives gharchive discovery.
		if ga.WindowHours == 0 {
			issues = append(issues, "discovery.sources.gharchive.window_hours: required when source is enabled (suggested: 24)")
		}
		if ga.TopNPerHour == 0 {
			issues = append(issues, "discovery.sources.gharchive.top_n_per_hour: required when source is enabled (suggested: 500)")
		}
		if ga.DailyCapWarn == 0 {
			issues = append(issues, "discovery.sources.gharchive.daily_cap_warn: required when source is enabled (suggested: 4000)")
		}
		if ga.DailyCapHard == 0 {
			issues = append(issues, "discovery.sources.gharchive.daily_cap_hard: required when source is enabled (suggested: 5000)")
		}
	}
	if ga.DailyCapWarn > 0 && ga.DailyCapHard > 0 && ga.DailyCapWarn >= ga.DailyCapHard {
		issues = append(issues, fmt.Sprintf("discovery.sources.gharchive: daily_cap_warn (%d) must be less than daily_cap_hard (%d)", ga.DailyCapWarn, ga.DailyCapHard))
	}

	// Classification settings
	if c.Classification.OllamaEndpoint != "" {
		parsedURL, err := url.Parse(c.Classification.OllamaEndpoint)
		if err != nil {
			issues = append(issues, fmt.Sprintf("classification.ollama_endpoint: invalid URL format: %v", err))
		} else if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			issues = append(issues, fmt.Sprintf("classification.ollama_endpoint: must use http:// or https:// scheme, got %q", parsedURL.Scheme))
		} else if parsedURL.Host == "" {
			issues = append(issues, "classification.ollama_endpoint: missing host")
		}
	}

	if c.Classification.TimeoutMs < 0 {
		issues = append(issues, fmt.Sprintf("classification.timeout_ms: must be >= 0, got %d", c.Classification.TimeoutMs))
	}

	if c.Classification.MaxReadmeChars < 0 {
		issues = append(issues, fmt.Sprintf("classification.max_readme_chars: must be >= 0, got %d", c.Classification.MaxReadmeChars))
	}

	if c.Classification.MinConfidence < 0 || c.Classification.MinConfidence > 1 {
		issues = append(issues, fmt.Sprintf("classification.min_confidence: must be between 0 and 1, got %.2f", c.Classification.MinConfidence))
	}

	// Scoring weights must be non-negative
	if c.Scoring.Weights.StarVelocity < 0 {
		issues = append(issues, fmt.Sprintf("scoring.weights.star_velocity: must be >= 0, got %f", c.Scoring.Weights.StarVelocity))
	}
	if c.Scoring.Weights.StarAcceleration < 0 {
		issues = append(issues, fmt.Sprintf("scoring.weights.star_acceleration: must be >= 0, got %f", c.Scoring.Weights.StarAcceleration))
	}
	if c.Scoring.Weights.ForkVelocity < 0 {
		issues = append(issues, fmt.Sprintf("scoring.weights.fork_velocity: must be >= 0, got %f", c.Scoring.Weights.ForkVelocity))
	}
	if c.Scoring.Weights.ReleaseCadence < 0 {
		issues = append(issues, fmt.Sprintf("scoring.weights.release_cadence: must be >= 0, got %f", c.Scoring.Weights.ReleaseCadence))
	}
	if c.Scoring.Weights.ContributorGrowth < 0 {
		issues = append(issues, fmt.Sprintf("scoring.weights.contributor_growth: must be >= 0, got %f", c.Scoring.Weights.ContributorGrowth))
	}
	if c.Scoring.Weights.PRVelocity < 0 {
		issues = append(issues, fmt.Sprintf("scoring.weights.pr_velocity: must be >= 0, got %f", c.Scoring.Weights.PRVelocity))
	}
	if c.Scoring.Weights.IssueVelocity < 0 {
		issues = append(issues, fmt.Sprintf("scoring.weights.issue_velocity: must be >= 0, got %f", c.Scoring.Weights.IssueVelocity))
	}

	if len(issues) > 0 {
		return &ValidationError{Issues: issues}
	}
	return nil
}

// ValidateAndLoad loads a config file, expands env vars, and validates.
// This is the recommended function for application startup.
func ValidateAndLoad(path string) (*Config, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}
