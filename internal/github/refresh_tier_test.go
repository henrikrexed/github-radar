package github

import (
	"testing"
	"time"
)

func TestClassifyAll_RankTiers(t *testing.T) {
	cfg := TierConfig{
		HotN:          2,
		WarmN:         2,
		NewRepoWindow: 1 * time.Hour,
		HotInterval:   1 * time.Hour,
		WarmInterval:  4 * time.Hour,
		ColdInterval:  12 * time.Hour,
		NewInterval:   30 * time.Minute,
	}

	now := time.Now()
	candidates := []TierCandidate{
		{FullName: "cold1", GrowthScore: 1.0, LastCollectedAt: now.Add(-20 * time.Hour)},
		{FullName: "hot1", GrowthScore: 100.0, LastCollectedAt: now.Add(-90 * time.Minute)},
		{FullName: "hot2", GrowthScore: 90.0, LastCollectedAt: now.Add(-90 * time.Minute)},
		{FullName: "warm1", GrowthScore: 50.0, LastCollectedAt: now.Add(-90 * time.Minute)},
		{FullName: "warm2", GrowthScore: 10.0, LastCollectedAt: now.Add(-90 * time.Minute)},
	}

	assignments := ClassifyAll(candidates, now, cfg)

	want := map[string]RefreshTier{
		"hot1":  TierHot,
		"hot2":  TierHot,
		"warm1": TierWarm,
		"warm2": TierWarm,
		"cold1": TierCold,
	}
	for _, a := range assignments {
		if want[a.FullName] != a.Tier {
			t.Errorf("%s: got tier %s, want %s", a.FullName, a.Tier, want[a.FullName])
		}
	}
}

func TestClassifyAll_NewRepoOverridesRank(t *testing.T) {
	cfg := DefaultTierConfig()
	now := time.Now()

	candidates := []TierCandidate{
		{FullName: "fresh", GrowthScore: 0.01, FirstSeenAt: now.Add(-2 * time.Hour)},
	}
	assignments := ClassifyAll(candidates, now, cfg)
	if assignments[0].Tier != TierNew {
		t.Errorf("expected TierNew, got %s", assignments[0].Tier)
	}
	if assignments[0].Interval != cfg.NewInterval {
		t.Errorf("interval = %s, want %s", assignments[0].Interval, cfg.NewInterval)
	}
}

func TestClassifyAll_NewRepoWindowBoundary(t *testing.T) {
	cfg := DefaultTierConfig()
	now := time.Now()

	// Exactly at the window boundary: should fall back to rank-based tier.
	candidates := []TierCandidate{
		{FullName: "boundary", GrowthScore: 0.01, FirstSeenAt: now.Add(-cfg.NewRepoWindow)},
	}
	assignments := ClassifyAll(candidates, now, cfg)
	if assignments[0].Tier == TierNew {
		t.Errorf("boundary candidate should NOT be TierNew, got %s", assignments[0].Tier)
	}
}

func TestDueRepos_Filtering(t *testing.T) {
	cfg := TierConfig{
		HotN: 10, WarmN: 10,
		HotInterval:  1 * time.Hour,
		WarmInterval: 4 * time.Hour,
		ColdInterval: 12 * time.Hour,
		NewInterval:  1 * time.Hour,
	}
	now := time.Now()

	assignments := ClassifyAll([]TierCandidate{
		{FullName: "due", GrowthScore: 100, LastCollectedAt: now.Add(-2 * time.Hour)},      // hot, due
		{FullName: "notdue", GrowthScore: 99, LastCollectedAt: now.Add(-30 * time.Minute)}, // hot, not due
		{FullName: "never", GrowthScore: 1},                                                // never collected, due
	}, now, cfg)

	due := DueRepos(assignments)
	got := map[string]bool{}
	for _, a := range due {
		got[a.FullName] = true
	}
	if !got["due"] || !got["never"] || got["notdue"] {
		t.Errorf("due set wrong: %+v", got)
	}
}

func TestCount(t *testing.T) {
	assignments := []TierAssignment{
		{Tier: TierHot}, {Tier: TierHot},
		{Tier: TierWarm},
		{Tier: TierCold}, {Tier: TierCold}, {Tier: TierCold},
		{Tier: TierNew},
	}
	h := Count(assignments)
	if h.Hot != 2 || h.Warm != 1 || h.Cold != 3 || h.New != 1 {
		t.Errorf("histogram = %+v", h)
	}
}

func TestDefaultTierConfig_Values(t *testing.T) {
	cfg := DefaultTierConfig()
	if cfg.HotN != 500 || cfg.WarmN != 1500 {
		t.Errorf("rank thresholds changed: HotN=%d WarmN=%d", cfg.HotN, cfg.WarmN)
	}
	if cfg.HotInterval != time.Hour {
		t.Errorf("HotInterval = %s, want 1h", cfg.HotInterval)
	}
	if cfg.WarmInterval != 4*time.Hour {
		t.Errorf("WarmInterval = %s, want 4h", cfg.WarmInterval)
	}
	if cfg.ColdInterval != 12*time.Hour {
		t.Errorf("ColdInterval = %s, want 12h", cfg.ColdInterval)
	}
	if cfg.NewRepoWindow != 48*time.Hour {
		t.Errorf("NewRepoWindow = %s, want 48h", cfg.NewRepoWindow)
	}
}
