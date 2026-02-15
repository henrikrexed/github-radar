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

	// Scoring weights must be non-negative
	if c.Scoring.Weights.StarVelocity < 0 {
		issues = append(issues, fmt.Sprintf("scoring.weights.star_velocity: must be >= 0, got %f", c.Scoring.Weights.StarVelocity))
	}
	if c.Scoring.Weights.StarAcceleration < 0 {
		issues = append(issues, fmt.Sprintf("scoring.weights.star_acceleration: must be >= 0, got %f", c.Scoring.Weights.StarAcceleration))
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
