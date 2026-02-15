package cli

import (
	"fmt"
	"os"

	"github.com/hrexed/github-radar/internal/config"
)

// ConfigCmd handles the config subcommand.
type ConfigCmd struct {
	cli *CLI
}

// NewConfigCmd creates a new config command handler.
func NewConfigCmd(cli *CLI) *ConfigCmd {
	return &ConfigCmd{cli: cli}
}

// Validate validates the configuration file.
func (c *ConfigCmd) Validate() int {
	path := config.ResolveConfigPath(c.cli.ConfigPath)

	if c.cli.Verbose {
		fmt.Printf("Config path resolution:\n")
		fmt.Printf("  --config flag: %s\n", valueOrDefault(c.cli.ConfigPath, "(not set)"))
		fmt.Printf("  %s env: %s\n", config.EnvConfigPath, valueOrDefault(os.Getenv(config.EnvConfigPath), "(not set)"))
		fmt.Printf("  Resolved path: %s\n\n", path)
	}

	fmt.Printf("Validating configuration: %s\n", path)

	cfg, err := config.Load(path)
	if err != nil {
		c.printError(err)
		return 1
	}

	if c.cli.Verbose {
		fmt.Println("Config loaded successfully, running validation...")
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Validation failed:\n%v\n", err)
		return 1
	}

	fmt.Println("Configuration is valid.")
	return 0
}

// Show displays the current configuration (without secrets).
func (c *ConfigCmd) Show() int {
	path := config.ResolveConfigPath(c.cli.ConfigPath)

	if c.cli.Verbose {
		fmt.Printf("Config path resolution:\n")
		fmt.Printf("  --config flag: %s\n", valueOrDefault(c.cli.ConfigPath, "(not set)"))
		fmt.Printf("  %s env: %s\n", config.EnvConfigPath, valueOrDefault(os.Getenv(config.EnvConfigPath), "(not set)"))
		fmt.Printf("  Resolved path: %s\n\n", path)
	}

	cfg, err := config.Load(path)
	if err != nil {
		c.printError(err)
		return 1
	}

	fmt.Printf("Configuration loaded from: %s\n\n", path)
	fmt.Printf("GitHub:\n")
	fmt.Printf("  Token: %s\n", maskSecret(cfg.GitHub.Token))
	fmt.Printf("  Rate Limit: %d\n", cfg.GitHub.RateLimit)
	fmt.Printf("\nOTel:\n")
	fmt.Printf("  Endpoint: %s\n", cfg.Otel.Endpoint)
	fmt.Printf("  Service Name: %s\n", cfg.Otel.ServiceName)
	fmt.Printf("\nDiscovery:\n")
	fmt.Printf("  Enabled: %v\n", cfg.Discovery.Enabled)
	fmt.Printf("  Min Stars: %d\n", cfg.Discovery.MinStars)
	fmt.Printf("  Max Age Days: %d\n", cfg.Discovery.MaxAgeDays)
	fmt.Printf("  Auto Track Threshold: %.1f\n", cfg.Discovery.AutoTrackThreshold)
	fmt.Printf("\nScoring Weights:\n")
	fmt.Printf("  Star Velocity: %.2f\n", cfg.Scoring.Weights.StarVelocity)
	fmt.Printf("  Star Acceleration: %.2f\n", cfg.Scoring.Weights.StarAcceleration)
	fmt.Printf("  Contributor Growth: %.2f\n", cfg.Scoring.Weights.ContributorGrowth)
	fmt.Printf("  PR Velocity: %.2f\n", cfg.Scoring.Weights.PRVelocity)
	fmt.Printf("  Issue Velocity: %.2f\n", cfg.Scoring.Weights.IssueVelocity)
	fmt.Printf("\nExclusions: %d repos\n", len(cfg.Exclusions))

	return 0
}

// printError prints an error with verbose details if enabled.
func (c *ConfigCmd) printError(err error) {
	if c.cli.Verbose {
		// Check if it's a ConfigError with Verbose() method
		if cfgErr, ok := err.(*config.ConfigError); ok {
			fmt.Fprintln(os.Stderr, cfgErr.Verbose())
			return
		}
	}
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
}

// maskSecret masks a secret value for display.
func maskSecret(s string) string {
	if s == "" {
		return "(not set)"
	}
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "****" + s[len(s)-4:]
}

// valueOrDefault returns the value or a default if empty.
func valueOrDefault(value, def string) string {
	if value == "" {
		return def
	}
	return value
}
