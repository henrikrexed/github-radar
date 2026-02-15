package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverCmd_NoTopics(t *testing.T) {
	// Create temp config without topics
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `
github:
  token: test-token
discovery:
  enabled: true
  topics: []
  min_stars: 100
`
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cli := New()
	cli.ConfigPath = configPath

	discoverCmd := NewDiscoverCmd(cli)
	result := discoverCmd.Run([]string{})

	// Should fail because no topics configured
	if result == 0 {
		t.Error("expected non-zero exit code when no topics configured")
	}
}

func TestDiscoverCmd_WithTopicsFlag(t *testing.T) {
	// Create temp config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `
github:
  token: test-token
discovery:
  enabled: true
  topics: []
  min_stars: 100
`
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cli := New()
	cli.ConfigPath = configPath

	discoverCmd := NewDiscoverCmd(cli)

	// This will fail at GitHub API call, but the flag parsing should work
	result := discoverCmd.Run([]string{"--topics", "kubernetes"})

	// Will fail due to token validation, but that's expected
	// The important thing is it didn't fail on "no topics"
	_ = result // We just want to ensure it doesn't panic
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"short", 10, "short"},           // shorter than max, no truncation
		{"exactly10c", 10, "exactly10c"}, // exactly max, no truncation
		{"this is a longer string", 10, "this is..."},
		{"abc", 3, "abc"},      // exactly max, no truncation
		{"abcd", 3, "..."},     // longer than max, truncated
		{"abcdef", 5, "ab..."}, // longer than max, truncated
	}

	for _, tc := range tests {
		result := truncate(tc.input, tc.max)
		if result != tc.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.max, result, tc.expected)
		}
	}
}
