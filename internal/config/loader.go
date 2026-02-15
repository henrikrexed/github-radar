// Package config provides configuration management for github-radar.
package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultConfigPath is the default config file location.
	DefaultConfigPath = "./github-radar.yaml"

	// EnvConfigPath is the environment variable for config path.
	EnvConfigPath = "GITHUB_RADAR_CONFIG"
)

// Load reads and parses a YAML configuration file.
// Environment variables in the format ${VAR} or ${VAR:-default} are expanded.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &ConfigError{
				Path:    path,
				Message: "file not found",
				Hint:    "Create config file or use --config flag to specify path. See configs/github-radar.example.yaml for template.",
				Err:     err,
			}
		}
		if os.IsPermission(err) {
			return nil, &ConfigError{
				Path:    path,
				Message: "permission denied",
				Hint:    "Check file permissions. Config file should be readable by current user.",
				Err:     err,
			}
		}
		return nil, &ConfigError{
			Path:    path,
			Message: "failed to read file",
			Err:     err,
		}
	}

	// Expand environment variables before parsing YAML
	expanded, err := ExpandEnvVars(data)
	if err != nil {
		return nil, &ConfigError{
			Path:    path,
			Message: "environment variable expansion failed",
			Hint:    "Set required environment variables or use ${VAR:-default} syntax for optional values.",
			Err:     err,
		}
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(expanded, cfg); err != nil {
		return nil, &ConfigError{
			Path:    path,
			Message: "invalid YAML syntax",
			Hint:    "Check YAML formatting. Common issues: incorrect indentation, missing colons, unquoted special characters.",
			Err:     err,
		}
	}

	return cfg, nil
}

// ResolveConfigPath determines the config file path using priority:
// 1. Explicit path argument (from --config flag)
// 2. GITHUB_RADAR_CONFIG environment variable
// 3. Default path (./github-radar.yaml)
func ResolveConfigPath(explicitPath string) string {
	// Priority 1: Explicit path from flag
	if explicitPath != "" {
		return explicitPath
	}

	// Priority 2: Environment variable
	if envPath := os.Getenv(EnvConfigPath); envPath != "" {
		return envPath
	}

	// Priority 3: Default path
	return DefaultConfigPath
}

// LoadFromPath resolves the config path and loads the configuration.
func LoadFromPath(explicitPath string) (*Config, error) {
	path := ResolveConfigPath(explicitPath)
	return Load(path)
}
