package daemon

import (
	"testing"
	"time"

	"github.com/hrexed/github-radar/internal/config"
	"github.com/hrexed/github-radar/internal/github"
)

func TestTierConfigFromYAML_Defaults(t *testing.T) {
	cfg := tierConfigFromYAML(config.TieringConfig{})
	def := github.DefaultTierConfig()
	if cfg.HotN != def.HotN || cfg.WarmN != def.WarmN {
		t.Errorf("zero YAML should fall back to defaults")
	}
	if cfg.HotInterval != def.HotInterval {
		t.Errorf("HotInterval = %s, want %s", cfg.HotInterval, def.HotInterval)
	}
}

func TestTierConfigFromYAML_Overrides(t *testing.T) {
	cfg := tierConfigFromYAML(config.TieringConfig{
		HotN:               50,
		WarmN:              100,
		NewRepoWindowHours: 24,
		HotIntervalMin:     30,
		WarmIntervalMin:    120,
		ColdIntervalMin:    720,
		NewIntervalMin:     15,
	})
	if cfg.HotN != 50 || cfg.WarmN != 100 {
		t.Errorf("rank overrides lost: %+v", cfg)
	}
	if cfg.NewRepoWindow != 24*time.Hour {
		t.Errorf("NewRepoWindow = %s", cfg.NewRepoWindow)
	}
	if cfg.HotInterval != 30*time.Minute {
		t.Errorf("HotInterval = %s", cfg.HotInterval)
	}
	if cfg.WarmInterval != 2*time.Hour {
		t.Errorf("WarmInterval = %s", cfg.WarmInterval)
	}
	if cfg.ColdInterval != 12*time.Hour {
		t.Errorf("ColdInterval = %s", cfg.ColdInterval)
	}
	if cfg.NewInterval != 15*time.Minute {
		t.Errorf("NewInterval = %s", cfg.NewInterval)
	}
}

func TestAPIObserver_NilExporter(t *testing.T) {
	// Observer with nil exporter should be a no-op; we just verify it
	// doesn't panic.
	obs := newAPIObserver(nil, nil)
	obs.ObserveCall("repo", "ok")
	obs.ObserveRateLimit(5000, 4000, time.Now())
}
