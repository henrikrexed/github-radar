package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoCmd_Add_Basic(t *testing.T) {
	cli := New()
	cli.Verbose = false
	repoCmd := NewRepoCmd(cli)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := repoCmd.Add([]string{"kubernetes/kubernetes"})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 0 {
		t.Errorf("Add() returned %d, want 0", exitCode)
	}
	if !strings.Contains(output, "kubernetes/kubernetes") {
		t.Errorf("output should contain repo name: %s", output)
	}
	if !strings.Contains(output, "default") {
		t.Errorf("output should mention default category: %s", output)
	}
}

func TestRepoCmd_Add_WithCategory(t *testing.T) {
	cli := New()
	cli.Verbose = false
	repoCmd := NewRepoCmd(cli)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := repoCmd.Add([]string{"--category", "monitoring", "prometheus/prometheus"})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 0 {
		t.Errorf("Add() returned %d, want 0", exitCode)
	}
	if !strings.Contains(output, "prometheus/prometheus") {
		t.Errorf("output should contain repo name: %s", output)
	}
	if !strings.Contains(output, "monitoring") {
		t.Errorf("output should mention category: %s", output)
	}
}

func TestRepoCmd_Add_URL(t *testing.T) {
	cli := New()
	cli.Verbose = false
	repoCmd := NewRepoCmd(cli)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := repoCmd.Add([]string{"https://github.com/grafana/grafana"})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 0 {
		t.Errorf("Add() returned %d, want 0", exitCode)
	}
	if !strings.Contains(output, "grafana/grafana") {
		t.Errorf("output should contain parsed repo name: %s", output)
	}
}

func TestRepoCmd_Add_NoArgs(t *testing.T) {
	cli := New()
	repoCmd := NewRepoCmd(cli)

	// Capture stderr
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	exitCode := repoCmd.Add([]string{})

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 1 {
		t.Errorf("Add() returned %d, want 1", exitCode)
	}
	if !strings.Contains(output, "repository argument required") {
		t.Errorf("output should mention missing argument: %s", output)
	}
}

func TestRepoCmd_Add_InvalidRepo(t *testing.T) {
	cli := New()
	repoCmd := NewRepoCmd(cli)

	// Capture stderr
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	exitCode := repoCmd.Add([]string{"invalid"})

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 1 {
		t.Errorf("Add() returned %d, want 1", exitCode)
	}
	if !strings.Contains(output, "invalid repository identifier") {
		t.Errorf("output should mention invalid repo: %s", output)
	}
}

func TestRepoCmd_Remove_Basic(t *testing.T) {
	cli := New()
	cli.Verbose = false
	repoCmd := NewRepoCmd(cli)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := repoCmd.Remove([]string{"kubernetes/kubernetes"})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 0 {
		t.Errorf("Remove() returned %d, want 0", exitCode)
	}
	if !strings.Contains(output, "Removed kubernetes/kubernetes") {
		t.Errorf("output should confirm removal: %s", output)
	}
}

func TestRepoCmd_Remove_KeepState(t *testing.T) {
	cli := New()
	cli.Verbose = false
	repoCmd := NewRepoCmd(cli)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := repoCmd.Remove([]string{"--keep-state", "kubernetes/kubernetes"})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 0 {
		t.Errorf("Remove() returned %d, want 0", exitCode)
	}
	if !strings.Contains(output, "State data preserved") {
		t.Errorf("output should mention state preserved: %s", output)
	}
}

func TestRepoCmd_Remove_NoArgs(t *testing.T) {
	cli := New()
	repoCmd := NewRepoCmd(cli)

	// Capture stderr
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	exitCode := repoCmd.Remove([]string{})

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 1 {
		t.Errorf("Remove() returned %d, want 1", exitCode)
	}
	if !strings.Contains(output, "repository argument required") {
		t.Errorf("output should mention missing argument: %s", output)
	}
}

func TestRepoCmd_List_Empty(t *testing.T) {
	// Create a temp config with no repositories
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

	cli := New()
	cli.ConfigPath = configPath
	cli.Verbose = false
	repoCmd := NewRepoCmd(cli)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := repoCmd.List([]string{})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 0 {
		t.Errorf("List() returned %d, want 0", exitCode)
	}
	if !strings.Contains(output, "No repositories tracked") {
		t.Errorf("output should indicate no repos: %s", output)
	}
}

func TestRepoCmd_List_WithRepos(t *testing.T) {
	// Create a temp config with repositories
	content := `
github:
  token: test-token
repositories:
  - repo: kubernetes/kubernetes
    categories:
      - cncf
  - repo: prometheus/prometheus
    categories:
      - monitoring
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cli := New()
	cli.ConfigPath = configPath
	cli.Verbose = false
	repoCmd := NewRepoCmd(cli)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := repoCmd.List([]string{})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 0 {
		t.Errorf("List() returned %d, want 0", exitCode)
	}
	if !strings.Contains(output, "kubernetes/kubernetes") {
		t.Errorf("output should contain kubernetes: %s", output)
	}
	if !strings.Contains(output, "prometheus/prometheus") {
		t.Errorf("output should contain prometheus: %s", output)
	}
	if !strings.Contains(output, "Total: 2") {
		t.Errorf("output should show total count: %s", output)
	}
}

func TestRepoCmd_List_FilterByCategory(t *testing.T) {
	// Create a temp config with repositories
	content := `
github:
  token: test-token
repositories:
  - repo: kubernetes/kubernetes
    categories:
      - cncf
  - repo: prometheus/prometheus
    categories:
      - monitoring
  - repo: grafana/grafana
    categories:
      - monitoring
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cli := New()
	cli.ConfigPath = configPath
	cli.Verbose = false
	repoCmd := NewRepoCmd(cli)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := repoCmd.List([]string{"--category", "monitoring"})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 0 {
		t.Errorf("List() returned %d, want 0", exitCode)
	}
	// Should NOT contain kubernetes (different category)
	if strings.Contains(output, "kubernetes/kubernetes") {
		t.Errorf("output should NOT contain kubernetes: %s", output)
	}
	// Should contain monitoring repos
	if !strings.Contains(output, "prometheus/prometheus") {
		t.Errorf("output should contain prometheus: %s", output)
	}
	if !strings.Contains(output, "grafana/grafana") {
		t.Errorf("output should contain grafana: %s", output)
	}
	if !strings.Contains(output, "Total: 2") {
		t.Errorf("output should show total count: %s", output)
	}
}

func TestRepoCmd_List_JSONFormat(t *testing.T) {
	content := `
github:
  token: test-token
repositories:
  - repo: kubernetes/kubernetes
    categories:
      - cncf
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cli := New()
	cli.ConfigPath = configPath
	cli.Verbose = false
	repoCmd := NewRepoCmd(cli)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := repoCmd.List([]string{"--format", "json"})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 0 {
		t.Errorf("List() returned %d, want 0", exitCode)
	}
	if !strings.Contains(output, `"repo":`) {
		t.Errorf("output should be JSON: %s", output)
	}
	if !strings.Contains(output, `"kubernetes/kubernetes"`) {
		t.Errorf("output should contain repo: %s", output)
	}
}

func TestRepoCmd_List_CSVFormat(t *testing.T) {
	content := `
github:
  token: test-token
repositories:
  - repo: kubernetes/kubernetes
    categories:
      - cncf
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cli := New()
	cli.ConfigPath = configPath
	cli.Verbose = false
	repoCmd := NewRepoCmd(cli)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := repoCmd.List([]string{"--format", "csv"})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if exitCode != 0 {
		t.Errorf("List() returned %d, want 0", exitCode)
	}
	if !strings.Contains(output, "repo,owner,name,categories") {
		t.Errorf("output should have CSV header: %s", output)
	}
	if !strings.Contains(output, "kubernetes/kubernetes,kubernetes,kubernetes") {
		t.Errorf("output should contain CSV data: %s", output)
	}
}
