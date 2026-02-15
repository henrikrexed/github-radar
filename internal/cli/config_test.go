package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigValidate_ValidConfig(t *testing.T) {
	os.Setenv("TEST_TOKEN", "test-token")
	defer os.Unsetenv("TEST_TOKEN")

	content := `
github:
  token: ${TEST_TOKEN}
  rate_limit: 4000

otel:
  endpoint: http://localhost:4318
  service_name: test

discovery:
  min_stars: 100
  max_age_days: 90
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	c := New()
	exitCode := c.Run([]string{"--config", configPath, "config", "validate"})
	if exitCode != 0 {
		t.Errorf("Run() returned %d, want 0", exitCode)
	}
}

func TestConfigValidate_InvalidConfig(t *testing.T) {
	content := `
github:
  token: ""
  rate_limit: -1

otel:
  endpoint: ""
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	c := New()
	exitCode := c.Run([]string{"--config", configPath, "config", "validate"})
	if exitCode != 1 {
		t.Errorf("Run() returned %d, want 1 for invalid config", exitCode)
	}
}

func TestConfigValidate_FileNotFound(t *testing.T) {
	c := New()
	exitCode := c.Run([]string{"--config", "/nonexistent/config.yaml", "config", "validate"})
	if exitCode != 1 {
		t.Errorf("Run() returned %d, want 1 for missing file", exitCode)
	}
}

func TestConfigShow_ValidConfig(t *testing.T) {
	os.Setenv("TEST_TOKEN", "my-secret-token-12345")
	defer os.Unsetenv("TEST_TOKEN")

	content := `
github:
  token: ${TEST_TOKEN}
  rate_limit: 4000

otel:
  endpoint: http://localhost:4318
  service_name: test

discovery:
  enabled: true
  min_stars: 100
  max_age_days: 90
  auto_track_threshold: 50
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	c := New()
	exitCode := c.Run([]string{"--config", configPath, "config", "show"})
	if exitCode != 0 {
		t.Errorf("Run() returned %d, want 0", exitCode)
	}
}

func TestConfigCommand_UnknownSubcommand(t *testing.T) {
	c := New()
	exitCode := c.Run([]string{"config", "unknown"})
	if exitCode != 1 {
		t.Errorf("Run() returned %d, want 1 for unknown subcommand", exitCode)
	}
}

func TestConfigCommand_NoSubcommand(t *testing.T) {
	c := New()
	exitCode := c.Run([]string{"config"})
	if exitCode != 1 {
		t.Errorf("Run() returned %d, want 1 for missing subcommand", exitCode)
	}
}

func TestMaskSecret(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "(not set)"},
		{"short", "****"},
		{"12345678", "****"},
		{"longer-secret-value", "long****alue"},
		{"my-secret-token-12345", "my-s****2345"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := maskSecret(tt.input)
			if got != tt.want {
				t.Errorf("maskSecret(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestUnknownCommand(t *testing.T) {
	c := New()
	exitCode := c.Run([]string{"unknown"})
	if exitCode != 1 {
		t.Errorf("Run() returned %d, want 1 for unknown command", exitCode)
	}
}

func TestHelpCommand(t *testing.T) {
	c := New()
	exitCode := c.Run([]string{"help"})
	if exitCode != 0 {
		t.Errorf("Run() returned %d, want 0 for help", exitCode)
	}
}
