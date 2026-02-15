// Package config provides configuration management for github-radar.
package config

import "fmt"

// Config represents the complete github-radar configuration.
type Config struct {
	GitHub       GithubConfig    `yaml:"github"`
	Otel         OtelConfig      `yaml:"otel"`
	Discovery    DiscoveryConfig `yaml:"discovery"`
	Scoring      ScoringConfig   `yaml:"scoring"`
	Exclusions   []string        `yaml:"exclusions"`
	Repositories []TrackedRepo   `yaml:"repositories"`
}

// TrackedRepo represents a repository being tracked with its categories.
type TrackedRepo struct {
	Repo       string   `yaml:"repo"`
	Categories []string `yaml:"categories"`
}

// GithubConfig contains GitHub API settings.
type GithubConfig struct {
	Token     string `yaml:"token"`
	RateLimit int    `yaml:"rate_limit"`
}

// OtelConfig contains OpenTelemetry export settings.
type OtelConfig struct {
	Endpoint    string            `yaml:"endpoint"`
	Headers     map[string]string `yaml:"headers"`
	ServiceName string            `yaml:"service_name"`
}

// DiscoveryConfig contains trend discovery settings.
type DiscoveryConfig struct {
	Enabled            bool `yaml:"enabled"`
	MinStars           int  `yaml:"min_stars"`
	MaxAgeDays         int  `yaml:"max_age_days"`
	AutoTrackThreshold int  `yaml:"auto_track_threshold"`
}

// ScoringConfig contains growth scoring settings.
type ScoringConfig struct {
	Weights WeightConfig `yaml:"weights"`
}

// WeightConfig contains scoring weight values.
type WeightConfig struct {
	StarVelocity      float64 `yaml:"star_velocity"`
	StarAcceleration  float64 `yaml:"star_acceleration"`
	ContributorGrowth float64 `yaml:"contributor_growth"`
	PRVelocity        float64 `yaml:"pr_velocity"`
	IssueVelocity     float64 `yaml:"issue_velocity"`
}

// ConfigError wraps config-related errors with context and hints.
type ConfigError struct {
	Path    string
	Message string
	Hint    string
	Err     error
}

// Error implements the error interface.
func (e *ConfigError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("config %s: %s: %v", e.Path, e.Message, e.Err)
	}
	return fmt.Sprintf("config %s: %s", e.Path, e.Message)
}

// Unwrap returns the underlying error.
func (e *ConfigError) Unwrap() error {
	return e.Err
}

// Verbose returns a detailed error message with hints.
func (e *ConfigError) Verbose() string {
	var result string
	if e.Err != nil {
		result = fmt.Sprintf("Error: config %s: %s\n  Cause: %v", e.Path, e.Message, e.Err)
	} else {
		result = fmt.Sprintf("Error: config %s: %s", e.Path, e.Message)
	}
	if e.Hint != "" {
		result += fmt.Sprintf("\n  Hint: %s", e.Hint)
	}
	return result
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		GitHub: GithubConfig{
			RateLimit: 4000,
		},
		Otel: OtelConfig{
			Endpoint:    "http://localhost:4318",
			ServiceName: "github-radar",
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
		Exclusions:   []string{},
		Repositories: []TrackedRepo{},
	}
}
