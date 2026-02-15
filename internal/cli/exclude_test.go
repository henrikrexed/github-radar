package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExcludeCmd_Add(t *testing.T) {
	cli := New()
	cli.Verbose = false
	excludeCmd := NewExcludeCmd(cli)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := excludeCmd.Add([]string{"owner/repo"})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 0 {
		t.Errorf("Add() returned %d, want 0", exitCode)
	}
	if !strings.Contains(output, "Added exclusion: owner/repo") {
		t.Errorf("output should confirm addition: %s", output)
	}
}

func TestExcludeCmd_Add_Wildcard(t *testing.T) {
	cli := New()
	cli.Verbose = false
	excludeCmd := NewExcludeCmd(cli)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := excludeCmd.Add([]string{"example-org/*"})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 0 {
		t.Errorf("Add() returned %d, want 0", exitCode)
	}
	if !strings.Contains(output, "example-org/*") {
		t.Errorf("output should contain pattern: %s", output)
	}
}

func TestExcludeCmd_Add_InvalidPattern(t *testing.T) {
	cli := New()
	excludeCmd := NewExcludeCmd(cli)

	// Capture stderr
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	exitCode := excludeCmd.Add([]string{"invalid"})

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 1 {
		t.Errorf("Add() returned %d, want 1", exitCode)
	}
	if !strings.Contains(output, "invalid exclusion pattern") {
		t.Errorf("output should mention invalid pattern: %s", output)
	}
}

func TestExcludeCmd_Add_NoArgs(t *testing.T) {
	cli := New()
	excludeCmd := NewExcludeCmd(cli)

	// Capture stderr
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	exitCode := excludeCmd.Add([]string{})

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 1 {
		t.Errorf("Add() returned %d, want 1", exitCode)
	}
	if !strings.Contains(output, "pattern required") {
		t.Errorf("output should mention pattern required: %s", output)
	}
}

func TestExcludeCmd_Remove(t *testing.T) {
	cli := New()
	cli.Verbose = false
	excludeCmd := NewExcludeCmd(cli)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := excludeCmd.Remove([]string{"owner/repo"})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 0 {
		t.Errorf("Remove() returned %d, want 0", exitCode)
	}
	if !strings.Contains(output, "Removed exclusion: owner/repo") {
		t.Errorf("output should confirm removal: %s", output)
	}
}

func TestExcludeCmd_Remove_NoArgs(t *testing.T) {
	cli := New()
	excludeCmd := NewExcludeCmd(cli)

	// Capture stderr
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	exitCode := excludeCmd.Remove([]string{})

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 1 {
		t.Errorf("Remove() returned %d, want 1", exitCode)
	}
	if !strings.Contains(output, "pattern required") {
		t.Errorf("output should mention pattern required: %s", output)
	}
}

func TestExcludeCmd_List_Empty(t *testing.T) {
	content := `
github:
  token: test-token
exclusions: []
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cli := New()
	cli.ConfigPath = configPath
	cli.Verbose = false
	excludeCmd := NewExcludeCmd(cli)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := excludeCmd.List()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 0 {
		t.Errorf("List() returned %d, want 0", exitCode)
	}
	if !strings.Contains(output, "No exclusions configured") {
		t.Errorf("output should indicate no exclusions: %s", output)
	}
}

func TestExcludeCmd_List_WithPatterns(t *testing.T) {
	content := `
github:
  token: test-token
exclusions:
  - owner/repo
  - example-org/*
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cli := New()
	cli.ConfigPath = configPath
	cli.Verbose = false
	excludeCmd := NewExcludeCmd(cli)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := excludeCmd.List()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 0 {
		t.Errorf("List() returned %d, want 0", exitCode)
	}
	if !strings.Contains(output, "owner/repo") {
		t.Errorf("output should contain exact pattern: %s", output)
	}
	if !strings.Contains(output, "example-org/*") {
		t.Errorf("output should contain wildcard pattern: %s", output)
	}
	if !strings.Contains(output, "Total: 2 exclusions") {
		t.Errorf("output should show total count: %s", output)
	}
}

func TestExcludeCmd_Run_NoArgs(t *testing.T) {
	cli := New()
	excludeCmd := NewExcludeCmd(cli)

	// Capture stderr
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	exitCode := excludeCmd.Run([]string{})

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 1 {
		t.Errorf("Run() returned %d, want 1", exitCode)
	}
	if !strings.Contains(output, "action required") {
		t.Errorf("output should mention action required: %s", output)
	}
}

func TestExcludeCmd_Run_InvalidAction(t *testing.T) {
	cli := New()
	excludeCmd := NewExcludeCmd(cli)

	// Capture stderr
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	exitCode := excludeCmd.Run([]string{"invalid"})

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 1 {
		t.Errorf("Run() returned %d, want 1", exitCode)
	}
	if !strings.Contains(output, "Unknown exclude action") {
		t.Errorf("output should mention unknown action: %s", output)
	}
}
