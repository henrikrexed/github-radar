package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_ValidConfig(t *testing.T) {
	// Create a temporary config file
	content := `
github:
  token: test-token
  rate_limit: 3000

otel:
  endpoint: http://test:4318
  service_name: test-service
  headers:
    Authorization: "Bearer test"

discovery:
  enabled: true
  min_stars: 50
  max_age_days: 30
  auto_track_threshold: 75

scoring:
  weights:
    star_velocity: 1.5
    star_acceleration: 2.5
    contributor_growth: 1.0
    pr_velocity: 0.5
    issue_velocity: 0.25

exclusions:
  - org/repo1
  - org/repo2
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify GitHub config
	if cfg.GitHub.Token != "test-token" {
		t.Errorf("GitHub.Token = %q, want %q", cfg.GitHub.Token, "test-token")
	}
	if cfg.GitHub.RateLimit != 3000 {
		t.Errorf("GitHub.RateLimit = %d, want %d", cfg.GitHub.RateLimit, 3000)
	}

	// Verify OTel config
	if cfg.Otel.Endpoint != "http://test:4318" {
		t.Errorf("Otel.Endpoint = %q, want %q", cfg.Otel.Endpoint, "http://test:4318")
	}
	if cfg.Otel.ServiceName != "test-service" {
		t.Errorf("Otel.ServiceName = %q, want %q", cfg.Otel.ServiceName, "test-service")
	}
	if cfg.Otel.Headers["Authorization"] != "Bearer test" {
		t.Errorf("Otel.Headers[Authorization] = %q, want %q", cfg.Otel.Headers["Authorization"], "Bearer test")
	}

	// Verify Discovery config
	if !cfg.Discovery.Enabled {
		t.Error("Discovery.Enabled = false, want true")
	}
	if cfg.Discovery.MinStars != 50 {
		t.Errorf("Discovery.MinStars = %d, want %d", cfg.Discovery.MinStars, 50)
	}
	if cfg.Discovery.MaxAgeDays != 30 {
		t.Errorf("Discovery.MaxAgeDays = %d, want %d", cfg.Discovery.MaxAgeDays, 30)
	}
	if cfg.Discovery.AutoTrackThreshold != 75 {
		t.Errorf("Discovery.AutoTrackThreshold = %d, want %d", cfg.Discovery.AutoTrackThreshold, 75)
	}

	// Verify Scoring config
	if cfg.Scoring.Weights.StarVelocity != 1.5 {
		t.Errorf("Scoring.Weights.StarVelocity = %f, want %f", cfg.Scoring.Weights.StarVelocity, 1.5)
	}
	if cfg.Scoring.Weights.StarAcceleration != 2.5 {
		t.Errorf("Scoring.Weights.StarAcceleration = %f, want %f", cfg.Scoring.Weights.StarAcceleration, 2.5)
	}

	// Verify Exclusions
	if len(cfg.Exclusions) != 2 {
		t.Errorf("len(Exclusions) = %d, want %d", len(cfg.Exclusions), 2)
	}
	if cfg.Exclusions[0] != "org/repo1" {
		t.Errorf("Exclusions[0] = %q, want %q", cfg.Exclusions[0], "org/repo1")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("Load() should return error for nonexistent file")
	}

	configErr, ok := err.(*ConfigError)
	if !ok {
		t.Fatalf("error should be *ConfigError, got %T", err)
	}

	if configErr.Path != "/nonexistent/path/config.yaml" {
		t.Errorf("ConfigError.Path = %q, want %q", configErr.Path, "/nonexistent/path/config.yaml")
	}
	if configErr.Message != "file not found" {
		t.Errorf("ConfigError.Message = %q, want %q", configErr.Message, "file not found")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")
	if err := os.WriteFile(configPath, []byte("invalid: yaml: content: ["), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("Load() should return error for invalid YAML")
	}

	configErr, ok := err.(*ConfigError)
	if !ok {
		t.Fatalf("error should be *ConfigError, got %T", err)
	}

	if configErr.Message != "invalid YAML syntax" {
		t.Errorf("ConfigError.Message = %q, want %q", configErr.Message, "invalid YAML syntax")
	}
}

func TestLoad_PartialConfig_UsesDefaults(t *testing.T) {
	// Config with only some fields - should use defaults for others
	content := `
github:
  token: my-token
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "partial.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Specified value
	if cfg.GitHub.Token != "my-token" {
		t.Errorf("GitHub.Token = %q, want %q", cfg.GitHub.Token, "my-token")
	}

	// Default values
	if cfg.GitHub.RateLimit != 4000 {
		t.Errorf("GitHub.RateLimit = %d, want default %d", cfg.GitHub.RateLimit, 4000)
	}
	if cfg.Otel.Endpoint != "http://localhost:4318" {
		t.Errorf("Otel.Endpoint = %q, want default %q", cfg.Otel.Endpoint, "http://localhost:4318")
	}
	if cfg.Scoring.Weights.StarVelocity != 2.0 {
		t.Errorf("Scoring.Weights.StarVelocity = %f, want default %f", cfg.Scoring.Weights.StarVelocity, 2.0)
	}
}

func TestResolveConfigPath_ExplicitPath(t *testing.T) {
	// Explicit path takes priority
	path := ResolveConfigPath("/explicit/config.yaml")
	if path != "/explicit/config.yaml" {
		t.Errorf("ResolveConfigPath() = %q, want %q", path, "/explicit/config.yaml")
	}
}

func TestResolveConfigPath_EnvVar(t *testing.T) {
	// Set env var
	os.Setenv(EnvConfigPath, "/env/config.yaml")
	defer os.Unsetenv(EnvConfigPath)

	// No explicit path, should use env var
	path := ResolveConfigPath("")
	if path != "/env/config.yaml" {
		t.Errorf("ResolveConfigPath() = %q, want %q", path, "/env/config.yaml")
	}
}

func TestResolveConfigPath_ExplicitOverridesEnv(t *testing.T) {
	// Set env var
	os.Setenv(EnvConfigPath, "/env/config.yaml")
	defer os.Unsetenv(EnvConfigPath)

	// Explicit path should override env var
	path := ResolveConfigPath("/explicit/config.yaml")
	if path != "/explicit/config.yaml" {
		t.Errorf("ResolveConfigPath() = %q, want %q", path, "/explicit/config.yaml")
	}
}

func TestResolveConfigPath_Default(t *testing.T) {
	// Ensure env var is not set
	os.Unsetenv(EnvConfigPath)

	// Should return default
	path := ResolveConfigPath("")
	if path != DefaultConfigPath {
		t.Errorf("ResolveConfigPath() = %q, want %q", path, DefaultConfigPath)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.GitHub.RateLimit != 4000 {
		t.Errorf("Default GitHub.RateLimit = %d, want %d", cfg.GitHub.RateLimit, 4000)
	}
	if cfg.Otel.Endpoint != "http://localhost:4318" {
		t.Errorf("Default Otel.Endpoint = %q, want %q", cfg.Otel.Endpoint, "http://localhost:4318")
	}
	if cfg.Otel.ServiceName != "github-radar" {
		t.Errorf("Default Otel.ServiceName = %q, want %q", cfg.Otel.ServiceName, "github-radar")
	}
	if !cfg.Discovery.Enabled {
		t.Error("Default Discovery.Enabled = false, want true")
	}
	if cfg.Discovery.MinStars != 100 {
		t.Errorf("Default Discovery.MinStars = %d, want %d", cfg.Discovery.MinStars, 100)
	}
	if cfg.Scoring.Weights.StarVelocity != 2.0 {
		t.Errorf("Default Scoring.Weights.StarVelocity = %f, want %f", cfg.Scoring.Weights.StarVelocity, 2.0)
	}
}

func TestConfigError_Error(t *testing.T) {
	// With underlying error
	err := &ConfigError{
		Path:    "/path/config.yaml",
		Message: "test message",
		Err:     os.ErrNotExist,
	}
	expected := "config /path/config.yaml: test message: file does not exist"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}

	// Without underlying error
	err2 := &ConfigError{
		Path:    "/path/config.yaml",
		Message: "test message",
	}
	expected2 := "config /path/config.yaml: test message"
	if err2.Error() != expected2 {
		t.Errorf("Error() = %q, want %q", err2.Error(), expected2)
	}
}

func TestLoad_EnvVarSubstitution(t *testing.T) {
	// Set env var
	os.Setenv("TEST_GITHUB_TOKEN", "secret-token-123")
	os.Setenv("TEST_RATE_LIMIT", "3500")
	defer os.Unsetenv("TEST_GITHUB_TOKEN")
	defer os.Unsetenv("TEST_RATE_LIMIT")

	content := `
github:
  token: ${TEST_GITHUB_TOKEN}
  rate_limit: 3500
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.GitHub.Token != "secret-token-123" {
		t.Errorf("GitHub.Token = %q, want %q", cfg.GitHub.Token, "secret-token-123")
	}
}

func TestLoad_EnvVarWithDefault(t *testing.T) {
	os.Unsetenv("UNSET_ENDPOINT")

	content := `
otel:
  endpoint: ${UNSET_ENDPOINT:-http://fallback:4318}
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Otel.Endpoint != "http://fallback:4318" {
		t.Errorf("Otel.Endpoint = %q, want %q", cfg.Otel.Endpoint, "http://fallback:4318")
	}
}

func TestLoad_MissingEnvVar(t *testing.T) {
	os.Unsetenv("REQUIRED_VAR")

	content := `
github:
  token: ${REQUIRED_VAR}
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("Load() should return error for missing env var")
	}

	configErr, ok := err.(*ConfigError)
	if !ok {
		t.Fatalf("error should be *ConfigError, got %T", err)
	}

	if configErr.Message != "environment variable expansion failed" {
		t.Errorf("ConfigError.Message = %q, want %q", configErr.Message, "environment variable expansion failed")
	}
}

func TestLoad_FileNotFound_HasHint(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("Load() should return error")
	}

	configErr, ok := err.(*ConfigError)
	if !ok {
		t.Fatalf("error should be *ConfigError, got %T", err)
	}

	if configErr.Hint == "" {
		t.Error("ConfigError.Hint should not be empty for file not found")
	}

	verbose := configErr.Verbose()
	if !strings.Contains(verbose, "Hint:") {
		t.Errorf("Verbose() should contain Hint: %s", verbose)
	}
}

func TestLoad_InvalidYAML_HasHint(t *testing.T) {
	content := `invalid: yaml: [unclosed`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("Load() should return error")
	}

	configErr, ok := err.(*ConfigError)
	if !ok {
		t.Fatalf("error should be *ConfigError, got %T", err)
	}

	if configErr.Hint == "" {
		t.Error("ConfigError.Hint should not be empty for invalid YAML")
	}

	if configErr.Message != "invalid YAML syntax" {
		t.Errorf("ConfigError.Message = %q, want %q", configErr.Message, "invalid YAML syntax")
	}
}

func TestConfigError_Verbose(t *testing.T) {
	err := &ConfigError{
		Path:    "/path/config.yaml",
		Message: "file not found",
		Hint:    "Create config file",
		Err:     os.ErrNotExist,
	}

	verbose := err.Verbose()
	if !strings.Contains(verbose, "Error:") {
		t.Errorf("Verbose() should contain 'Error:': %s", verbose)
	}
	if !strings.Contains(verbose, "Cause:") {
		t.Errorf("Verbose() should contain 'Cause:': %s", verbose)
	}
	if !strings.Contains(verbose, "Hint:") {
		t.Errorf("Verbose() should contain 'Hint:': %s", verbose)
	}
}

func TestConfigError_Verbose_NoHint(t *testing.T) {
	err := &ConfigError{
		Path:    "/path/config.yaml",
		Message: "test error",
	}

	verbose := err.Verbose()
	if strings.Contains(verbose, "Hint:") {
		t.Errorf("Verbose() should not contain 'Hint:' when hint is empty: %s", verbose)
	}
}

func TestLoad_RepositoriesWithCategories(t *testing.T) {
	content := `
github:
  token: test-token

repositories:
  - repo: kubernetes/kubernetes
    categories:
      - cncf
      - container-orchestration
  - repo: prometheus/prometheus
    categories:
      - cncf
      - monitoring
  - repo: grafana/grafana
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Should have 3 repositories
	if len(cfg.Repositories) != 3 {
		t.Errorf("len(Repositories) = %d, want 3", len(cfg.Repositories))
	}

	// First repo with categories
	if cfg.Repositories[0].Repo != "kubernetes/kubernetes" {
		t.Errorf("Repositories[0].Repo = %q, want %q", cfg.Repositories[0].Repo, "kubernetes/kubernetes")
	}
	if len(cfg.Repositories[0].Categories) != 2 {
		t.Errorf("Repositories[0].Categories len = %d, want 2", len(cfg.Repositories[0].Categories))
	}
	if cfg.Repositories[0].Categories[0] != "cncf" {
		t.Errorf("Repositories[0].Categories[0] = %q, want %q", cfg.Repositories[0].Categories[0], "cncf")
	}

	// Repo without categories (should have empty list, handled by tracker)
	if cfg.Repositories[2].Repo != "grafana/grafana" {
		t.Errorf("Repositories[2].Repo = %q, want %q", cfg.Repositories[2].Repo, "grafana/grafana")
	}
	// Categories will be nil/empty from YAML, normalized by tracker
	if len(cfg.Repositories[2].Categories) != 0 {
		t.Errorf("Repositories[2].Categories len = %d, want 0 (before normalization)", len(cfg.Repositories[2].Categories))
	}
}

func TestLoad_RepositoriesEmpty(t *testing.T) {
	content := `
github:
  token: test-token

repositories: []
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if len(cfg.Repositories) != 0 {
		t.Errorf("len(Repositories) = %d, want 0", len(cfg.Repositories))
	}
}

func TestDefaultConfig_HasEmptyRepositories(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Repositories == nil {
		t.Error("Default Repositories should not be nil")
	}
	if len(cfg.Repositories) != 0 {
		t.Errorf("Default len(Repositories) = %d, want 0", len(cfg.Repositories))
	}
}
