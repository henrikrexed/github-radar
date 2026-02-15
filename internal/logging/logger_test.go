package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNew_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, false)

	logger.Info("test message", "key", "value")

	output := buf.String()

	// Verify it's valid JSON
	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(output), &logEntry); err != nil {
		t.Fatalf("log output is not valid JSON: %v\nOutput: %s", err, output)
	}

	// Check required fields
	if _, ok := logEntry["time"]; !ok {
		t.Error("log entry missing 'time' field")
	}
	if _, ok := logEntry["level"]; !ok {
		t.Error("log entry missing 'level' field")
	}
	if _, ok := logEntry["msg"]; !ok {
		t.Error("log entry missing 'msg' field")
	}
	if logEntry["msg"] != "test message" {
		t.Errorf("msg = %v, want %q", logEntry["msg"], "test message")
	}
	if logEntry["key"] != "value" {
		t.Errorf("key = %v, want %q", logEntry["key"], "value")
	}
}

func TestNew_VerboseEnablesDebug(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, true) // verbose = true

	logger.Debug("debug message")
	logger.Info("info message")

	output := buf.String()
	if !strings.Contains(output, "debug message") {
		t.Error("verbose mode should include DEBUG messages")
	}
	if !strings.Contains(output, "info message") {
		t.Error("verbose mode should include INFO messages")
	}
}

func TestNew_NonVerboseFiltersDebug(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, false) // verbose = false

	logger.Debug("debug message")
	logger.Info("info message")

	output := buf.String()
	if strings.Contains(output, "debug message") {
		t.Error("non-verbose mode should NOT include DEBUG messages")
	}
	if !strings.Contains(output, "info message") {
		t.Error("non-verbose mode should include INFO messages")
	}
}

func TestLogLevels(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, true) // Enable all levels

	logger.Debug("debug")
	logger.Info("info")
	logger.Warn("warn")
	logger.Error("error")

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 log lines, got %d", len(lines))
	}

	expectedLevels := []string{"DEBUG", "INFO", "WARN", "ERROR"}
	for i, line := range lines {
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %d is not valid JSON: %v", i, err)
		}
		if entry["level"] != expectedLevels[i] {
			t.Errorf("line %d: level = %v, want %s", i, entry["level"], expectedLevels[i])
		}
	}
}

func TestRepo_Attributes(t *testing.T) {
	attrs := Repo("kubernetes", "kubernetes")

	if len(attrs) != 4 {
		t.Fatalf("Repo() returned %d elements, want 4", len(attrs))
	}

	if attrs[0] != AttrRepoOwner {
		t.Errorf("attrs[0] = %v, want %s", attrs[0], AttrRepoOwner)
	}
	if attrs[1] != "kubernetes" {
		t.Errorf("attrs[1] = %v, want %s", attrs[1], "kubernetes")
	}
	if attrs[2] != AttrRepoName {
		t.Errorf("attrs[2] = %v, want %s", attrs[2], AttrRepoName)
	}
	if attrs[3] != "kubernetes" {
		t.Errorf("attrs[3] = %v, want %s", attrs[3], "kubernetes")
	}
}

func TestRepoFull_Attributes(t *testing.T) {
	attrs := RepoFull("kubernetes", "kubernetes")

	if len(attrs) != 2 {
		t.Fatalf("RepoFull() returned %d elements, want 2", len(attrs))
	}

	if attrs[0] != AttrRepoFull {
		t.Errorf("attrs[0] = %v, want %s", attrs[0], AttrRepoFull)
	}
	if attrs[1] != "kubernetes/kubernetes" {
		t.Errorf("attrs[1] = %v, want %s", attrs[1], "kubernetes/kubernetes")
	}
}

func TestScan_Attributes(t *testing.T) {
	attrs := Scan("scan-123", 47)

	if len(attrs) != 4 {
		t.Fatalf("Scan() returned %d elements, want 4", len(attrs))
	}

	if attrs[0] != AttrScanID {
		t.Errorf("attrs[0] = %v, want %s", attrs[0], AttrScanID)
	}
	if attrs[1] != "scan-123" {
		t.Errorf("attrs[1] = %v, want %s", attrs[1], "scan-123")
	}
}

func TestDuration_Attributes(t *testing.T) {
	attrs := Duration(1500)

	if len(attrs) != 2 {
		t.Fatalf("Duration() returned %d elements, want 2", len(attrs))
	}

	if attrs[0] != AttrDurationMS {
		t.Errorf("attrs[0] = %v, want %s", attrs[0], AttrDurationMS)
	}
	if attrs[1] != int64(1500) {
		t.Errorf("attrs[1] = %v, want %d", attrs[1], 1500)
	}
}

func TestErr_Attributes(t *testing.T) {
	err := errors.New("test error")
	attrs := Err(err)

	if len(attrs) != 2 {
		t.Fatalf("Err() returned %d elements, want 2", len(attrs))
	}

	if attrs[0] != AttrError {
		t.Errorf("attrs[0] = %v, want %s", attrs[0], AttrError)
	}
	if attrs[1] != "test error" {
		t.Errorf("attrs[1] = %v, want %s", attrs[1], "test error")
	}
}

func TestErr_NilError(t *testing.T) {
	attrs := Err(nil)

	if attrs != nil {
		t.Errorf("Err(nil) = %v, want nil", attrs)
	}
}

func TestAttributeNames_SnakeCase(t *testing.T) {
	// Verify all attribute constants use snake_case
	attrs := []string{
		AttrRepoOwner,
		AttrRepoName,
		AttrRepoFull,
		AttrScanID,
		AttrReposTotal,
		AttrReposScanned,
		AttrDurationMS,
		AttrStars,
		AttrForks,
		AttrGrowthScore,
		AttrStarVelocity,
		AttrError,
		AttrConfigPath,
	}

	for _, attr := range attrs {
		if strings.Contains(attr, "-") {
			t.Errorf("attribute %q uses hyphens, should use snake_case", attr)
		}
		if attr != strings.ToLower(attr) {
			t.Errorf("attribute %q contains uppercase, should use snake_case", attr)
		}
	}
}

func TestWith_CreatesChildLogger(t *testing.T) {
	var buf bytes.Buffer
	Logger = New(&buf, false)

	childLogger := With(AttrScanID, "test-scan")
	childLogger.Info("child message")

	output := buf.String()
	var entry map[string]interface{}
	if err := json.Unmarshal([]byte(output), &entry); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if entry["scan_id"] != "test-scan" {
		t.Errorf("scan_id = %v, want %s", entry["scan_id"], "test-scan")
	}
}

func TestInit_SetsDefaultLogger(t *testing.T) {
	var buf bytes.Buffer

	// Save original
	original := Logger
	defer func() { Logger = original }()

	// Create logger with buffer and set it
	Logger = New(&buf, true)

	// Use the package functions
	Debug("test debug")

	output := buf.String()
	if !strings.Contains(output, "test debug") {
		t.Error("Logger with verbose=true should enable DEBUG level")
	}
}
