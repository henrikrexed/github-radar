package metrics

import (
	"context"
	"testing"
	"time"
)

func TestDefaultExporterConfig(t *testing.T) {
	cfg := DefaultExporterConfig()

	if cfg.Endpoint != "http://localhost:4318" {
		t.Errorf("Endpoint = %v, want http://localhost:4318", cfg.Endpoint)
	}
	if cfg.ServiceName != "github-radar" {
		t.Errorf("ServiceName = %v, want github-radar", cfg.ServiceName)
	}
	if cfg.FlushTimeout != DefaultFlushTimeout {
		t.Errorf("FlushTimeout = %v, want %v", cfg.FlushTimeout, DefaultFlushTimeout)
	}
}

func TestNewExporter_DryRun(t *testing.T) {
	cfg := ExporterConfig{
		Endpoint:    "http://localhost:4318",
		ServiceName: "test-service",
		DryRun:      true,
	}

	exp, err := NewExporter(cfg)
	if err != nil {
		t.Fatalf("NewExporter error: %v", err)
	}
	defer exp.ShutdownWithTimeout()

	if !exp.IsDryRun() {
		t.Error("IsDryRun() = false, want true")
	}
}

func TestExporter_RecordRepoMetrics(t *testing.T) {
	cfg := ExporterConfig{
		Endpoint:    "http://localhost:4318",
		ServiceName: "test-service",
		DryRun:      true, // Don't actually export
	}

	exp, err := NewExporter(cfg)
	if err != nil {
		t.Fatalf("NewExporter error: %v", err)
	}
	defer exp.ShutdownWithTimeout()

	// Record metrics (should not panic)
	ctx := context.Background()
	m := RepoMetrics{
		Owner:             "test",
		Name:              "repo",
		Language:          "Go",
		Categories:        []string{"cli", "tools"},
		Stars:             1000,
		Forks:             100,
		OpenIssues:        50,
		OpenPRs:           10,
		Contributors:      25,
		GrowthScore:       45.5,
		NormalizedScore:   78.3,
		StarVelocity:      5.2,
		StarAcceleration:  1.1,
		PRVelocity:        2.0,
		IssueVelocity:     1.5,
		ContributorGrowth: 0.3,
	}

	exp.RecordRepoMetrics(ctx, m)

	// Flush should succeed
	if err := exp.Flush(ctx); err != nil {
		t.Errorf("Flush error: %v", err)
	}
}

func TestExporter_Shutdown(t *testing.T) {
	cfg := ExporterConfig{
		Endpoint:     "http://localhost:4318",
		ServiceName:  "test-service",
		DryRun:       true,
		FlushTimeout: 5 * time.Second,
	}

	exp, err := NewExporter(cfg)
	if err != nil {
		t.Fatalf("NewExporter error: %v", err)
	}

	// Shutdown should succeed
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := exp.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown error: %v", err)
	}
}

func TestExporter_WithHeaders(t *testing.T) {
	cfg := ExporterConfig{
		Endpoint:    "http://localhost:4318",
		ServiceName: "test-service",
		Headers: map[string]string{
			"Authorization": "Api-Token test-token",
		},
		DryRun: true,
	}

	exp, err := NewExporter(cfg)
	if err != nil {
		t.Fatalf("NewExporter error: %v", err)
	}
	defer exp.ShutdownWithTimeout()
}

func TestExporter_Meter(t *testing.T) {
	cfg := ExporterConfig{
		Endpoint:    "http://localhost:4318",
		ServiceName: "test-service",
		DryRun:      true,
	}

	exp, err := NewExporter(cfg)
	if err != nil {
		t.Fatalf("NewExporter error: %v", err)
	}
	defer exp.ShutdownWithTimeout()

	meter := exp.Meter()
	if meter == nil {
		t.Error("Meter() returned nil")
	}
}
