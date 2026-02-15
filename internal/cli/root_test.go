package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFlags_ConfigPath(t *testing.T) {
	c := New()
	err := c.ParseFlags([]string{"--config", "/path/to/config.yaml"})
	if err != nil {
		t.Fatalf("ParseFlags() failed: %v", err)
	}

	if c.ConfigPath != "/path/to/config.yaml" {
		t.Errorf("ConfigPath = %q, want %q", c.ConfigPath, "/path/to/config.yaml")
	}
}

func TestParseFlags_Verbose(t *testing.T) {
	c := New()
	err := c.ParseFlags([]string{"--verbose"})
	if err != nil {
		t.Fatalf("ParseFlags() failed: %v", err)
	}

	if !c.Verbose {
		t.Error("Verbose = false, want true")
	}
}

func TestParseFlags_DryRun(t *testing.T) {
	c := New()
	err := c.ParseFlags([]string{"--dry-run"})
	if err != nil {
		t.Fatalf("ParseFlags() failed: %v", err)
	}

	if !c.DryRun {
		t.Error("DryRun = false, want true")
	}
}

func TestParseFlags_AllFlags(t *testing.T) {
	c := New()
	err := c.ParseFlags([]string{
		"--config", "/my/config.yaml",
		"--verbose",
		"--dry-run",
	})
	if err != nil {
		t.Fatalf("ParseFlags() failed: %v", err)
	}

	if c.ConfigPath != "/my/config.yaml" {
		t.Errorf("ConfigPath = %q, want %q", c.ConfigPath, "/my/config.yaml")
	}
	if !c.Verbose {
		t.Error("Verbose = false, want true")
	}
	if !c.DryRun {
		t.Error("DryRun = false, want true")
	}
}

func TestLoadConfig_ValidFile(t *testing.T) {
	// Create temp config file
	content := `
github:
  token: test-token
  rate_limit: 3000
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	c := New()
	c.ConfigPath = configPath
	err := c.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}

	if c.Config == nil {
		t.Fatal("Config is nil after LoadConfig()")
	}
	if c.Config.GitHub.Token != "test-token" {
		t.Errorf("Config.GitHub.Token = %q, want %q", c.Config.GitHub.Token, "test-token")
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	c := New()
	c.ConfigPath = "/nonexistent/config.yaml"
	err := c.LoadConfig()
	if err == nil {
		t.Fatal("LoadConfig() should return error for nonexistent file")
	}
}

func TestRun_NoConfig(t *testing.T) {
	c := New()
	// Run without config file - should succeed (config is optional for version output)
	exitCode := c.Run([]string{})
	if exitCode != 0 {
		t.Errorf("Run() returned %d, want 0", exitCode)
	}
}

func TestRun_WithValidConfig(t *testing.T) {
	// Create temp config file
	content := `
github:
  token: test-token
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	c := New()
	exitCode := c.Run([]string{"--config", configPath})
	if exitCode != 0 {
		t.Errorf("Run() returned %d, want 0", exitCode)
	}
	if c.Config == nil {
		t.Error("Config should be loaded")
	}
}
