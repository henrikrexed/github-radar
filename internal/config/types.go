// Package config provides configuration management for github-radar.
package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Config represents the complete github-radar configuration.
type Config struct {
	GitHub         GithubConfig         `yaml:"github"`
	Otel           OtelConfig           `yaml:"otel"`
	Discovery      DiscoveryConfig      `yaml:"discovery"`
	Scoring        ScoringConfig        `yaml:"scoring"`
	Classification ClassificationConfig `yaml:"classification"`
	Exclusions     []string             `yaml:"exclusions"`
	Repositories   []TrackedRepo        `yaml:"repositories"`
}

// TrackedRepo represents a repository being tracked with its categories.
// Supports YAML unmarshalling from both a simple string ("owner/repo")
// and a full object ({repo: "owner/repo", categories: [...]}).
type TrackedRepo struct {
	Repo       string   `yaml:"repo"`
	Categories []string `yaml:"categories"`
}

// UnmarshalYAML implements yaml.Unmarshaler so that TrackedRepo can be
// parsed from either a bare string or a mapping node.
func (t *TrackedRepo) UnmarshalYAML(value *yaml.Node) error {
	// Simple string: "owner/repo"
	if value.Kind == yaml.ScalarNode {
		t.Repo = value.Value
		t.Categories = nil
		return nil
	}

	// Full object: {repo: ..., categories: [...]}
	type plain TrackedRepo // avoid recursion
	var p plain
	if err := value.Decode(&p); err != nil {
		return err
	}
	*t = TrackedRepo(p)
	return nil
}

// GithubConfig contains GitHub API settings.
type GithubConfig struct {
	Token     string `yaml:"token"`
	RateLimit int    `yaml:"rate_limit"`
}

// OtelConfig contains OpenTelemetry export settings.
type OtelConfig struct {
	Endpoint       string            `yaml:"endpoint"`
	Headers        map[string]string `yaml:"headers"`
	ServiceName    string            `yaml:"service_name"`
	ServiceVersion string            `yaml:"service_version"`
	FlushTimeout   int               `yaml:"flush_timeout"` // seconds, default 10
	Attributes     map[string]string `yaml:"attributes"`    // additional resource attributes
}

// DiscoveryConfig contains trend discovery settings.
type DiscoveryConfig struct {
	Enabled            bool     `yaml:"enabled"`
	Topics             []string `yaml:"topics"`               // Topics to search for (e.g., kubernetes, ebpf)
	MinStars           int      `yaml:"min_stars"`            // Minimum stars filter (default: 100)
	MaxAgeDays         int      `yaml:"max_age_days"`         // Maximum repo age in days (0 = no limit)
	AutoTrackThreshold float64  `yaml:"auto_track_threshold"` // Growth score threshold for auto-tracking

	// Sources configures discovery sources beyond the default topic
	// search. Each sub-source is feature-flagged and disabled by
	// default; rollout is staged via config alone (no rebuild needed).
	Sources DiscoverySourcesConfig `yaml:"sources"`
}

// DiscoverySourcesConfig groups the per-source discovery sub-configs.
type DiscoverySourcesConfig struct {
	// Orgs is Source (3) of the 4-source funnel: per-org repository
	// search. Catches actively-maintained repos under high-signal
	// orgs (kubernetes, hashicorp, opentelemetry, …).
	Orgs DiscoveryOrgsConfig `yaml:"orgs"`
	// Languages is Source (4): language-pivot search for popular
	// projects pushed within recent windows.
	Languages DiscoveryLanguagesConfig `yaml:"languages"`
}

// DiscoveryOrgsConfig configures org-scoped repository search.
type DiscoveryOrgsConfig struct {
	Enabled  bool     `yaml:"enabled"`
	Names    []string `yaml:"names"`     // GitHub org logins to search
	MinStars int      `yaml:"min_stars"` // Override Discovery.MinStars; 0 = inherit
}

// DiscoveryLanguagesConfig configures language-pivot repository search.
type DiscoveryLanguagesConfig struct {
	Enabled         bool     `yaml:"enabled"`
	Names           []string `yaml:"names"`             // GitHub language identifiers
	MinStars        int      `yaml:"min_stars"`         // Override Discovery.MinStars; 0 = inherit
	PushWindowsDays []int    `yaml:"push_windows_days"` // pushed:>= windows in days; empty = [7]
}

// ScoringConfig contains growth scoring settings.
type ScoringConfig struct {
	Weights WeightConfig `yaml:"weights"`
}

// WeightConfig contains scoring weight values.
type WeightConfig struct {
	StarVelocity      float64 `yaml:"star_velocity"`
	StarAcceleration  float64 `yaml:"star_acceleration"`
	ForkVelocity      float64 `yaml:"fork_velocity"`
	ReleaseCadence    float64 `yaml:"release_cadence"`
	ContributorGrowth float64 `yaml:"contributor_growth"`
	PRVelocity        float64 `yaml:"pr_velocity"`
	IssueVelocity     float64 `yaml:"issue_velocity"`
}

// ClassificationConfig contains LLM-based repository classification settings.
type ClassificationConfig struct {
	OllamaEndpoint string   `yaml:"ollama_endpoint"`  // Ollama API endpoint
	Model          string   `yaml:"model"`            // LLM model name
	TimeoutMs      int      `yaml:"timeout_ms"`       // Request timeout in milliseconds
	MaxReadmeChars int      `yaml:"max_readme_chars"` // Max README characters to send to LLM
	MinConfidence  float64  `yaml:"min_confidence"`   // Minimum confidence threshold
	Categories     []string `yaml:"categories"`       // Allowed classification categories
	SystemPrompt   string   `yaml:"system_prompt"`    // Go template for system prompt
	UserPrompt     string   `yaml:"user_prompt"`      // Go template for user prompt
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
			Endpoint:    "",
			ServiceName: "github-radar",
		},
		Discovery: DiscoveryConfig{
			Enabled:            true,
			Topics:             []string{},
			MinStars:           100,
			MaxAgeDays:         90,
			AutoTrackThreshold: 50.0,
		},
		Scoring: ScoringConfig{
			Weights: WeightConfig{
				StarVelocity:      2.0,
				StarAcceleration:  3.0,
				ForkVelocity:      1.5,
				ReleaseCadence:    1.0,
				ContributorGrowth: 1.5,
				PRVelocity:        1.0,
				IssueVelocity:     0.5,
			},
		},
		Classification: ClassificationConfig{
			OllamaEndpoint: "http://10.0.0.185:11434",
			Model:          "qwen3:1.7b",
			TimeoutMs:      30000,
			MaxReadmeChars: 2000,
			MinConfidence:  0.6,
			Categories: []string{
				// AI & ML
				"ai-agents",
				"llm-tooling",
				"ai-coding-assistants",
				"mcp-ecosystem",
				"ai-infrastructure",
				"computer-vision",
				"voice-and-audio-ai",
				"mlops",
				"vector-database",
				"rag",
				// Cloud-Native & Infrastructure
				"kubernetes",
				"observability",
				"cloud-native-security",
				"networking",
				"service-mesh",
				"platform-engineering",
				"gitops",
				"infrastructure",
				"container-runtime",
				"wasm",
				// Web & Frontend
				"web-frameworks",
				"frontend-ui",
				"css-and-styling",
				// Mobile & Desktop
				"mobile-development",
				"desktop-apps",
				// Systems & Languages
				"rust-ecosystem",
				"programming-languages",
				"embedded-iot",
				// Security
				"cybersecurity",
				"privacy-tools",
				// Data & Databases
				"databases",
				"data-engineering",
				"data-science",
				// Productivity & Self-Hosted
				"self-hosted",
				"cli-tools",
				"productivity",
				"low-code-automation",
				// Developer Tools & Testing
				"developer-tools",
				"testing",
				// Game Dev & Creative
				"game-development",
				"media-tools",
				// Crypto & Web3
				"blockchain-web3",
				// Robotics
				"robotics",
				// Catch-all
				"other",
			},
			SystemPrompt: `You are a GitHub repository classifier for trending open-source projects across all technology domains.
Classify into exactly ONE category from: {{.Categories}}
Pick the most specific category that fits. Use "other" only if no category applies.
Respond ONLY with JSON: {"category": "<name>", "confidence": <0.0-1.0>, "reasoning": "<one sentence>"}`,
			UserPrompt: `Repository: {{.RepoName}}
Description: {{.Description}}
Language: {{.Language}}
Topics: {{.Topics}}
Stars: {{.Stars}} (trend: {{.StarTrend}})
README excerpt:
{{.Readme}}`,
		},
		Exclusions:   []string{},
		Repositories: []TrackedRepo{},
	}
}
