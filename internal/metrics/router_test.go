package metrics

import (
	"testing"
	"time"

	"github.com/hrexed/github-radar/internal/state"
)

func TestRouterConfig_Defaults(t *testing.T) {
	cfg := DefaultRouterConfig()

	if cfg.GHArchiveEnabled != false {
		t.Errorf("GHArchiveEnabled = %v, want false", cfg.GHArchiveEnabled)
	}
	if cfg.GHArchiveBaseURL != "https://data.gharchive.org" {
		t.Errorf("GHArchiveBaseURL = %v, want https://data.gharchive.org", cfg.GHArchiveBaseURL)
	}
	if cfg.GHArchiveTimeout != 60*1e9 {
		t.Errorf("GHArchiveTimeout = %v, want 60s", cfg.GHArchiveTimeout)
	}
	if cfg.FallbackThresholdPct != 0.25 {
		t.Errorf("FallbackThresholdPct = %v, want 0.25", cfg.FallbackThresholdPct)
	}
}

func TestRouter_ShouldFallback_Disabled(t *testing.T) {
	cfg := DefaultRouterConfig()
	r := &Router{
		enabled:   cfg.GHArchiveEnabled,
		threshold: cfg.FallbackThresholdPct,
	}

	if r.shouldFallback() {
		t.Error("shouldFallback() = true when gharchive disabled, want false")
	}
}

func TestRouter_Status(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		expected string
	}{
		{"disabled returns live_only", false, "live_only"},
		{"enabled returns live_active", true, "live_active"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Router{enabled: tt.enabled, threshold: 0.25}
			if got := r.Status(); got != tt.expected {
				t.Errorf("Status() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRouter_ThresholdClamp(t *testing.T) {
	tests := []struct {
		name      string
		threshold float64
		expected  float64
	}{
		{"zero clamped to 0.25", 0, 0.25},
		{"negative clamped to 0.25", -1, 0.25},
		{"above 1 clamped to 0.25", 1.5, 0.25},
		{"valid 0.1 preserved", 0.1, 0.1},
		{"valid 0.5 preserved", 0.5, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Router{threshold: tt.threshold}
			if r.threshold <= 0 || r.threshold >= 1 {
				r.threshold = 0.25
			}
			if r.threshold != tt.expected {
				t.Errorf("threshold = %v, want %v", r.threshold, tt.expected)
			}
		})
	}
}

func TestShouldFallback_ThresholdMath(t *testing.T) {
	tests := []struct {
		name      string
		limit     int
		remaining int
		threshold float64
		expected  bool
	}{
		{"plenty of headroom", 5000, 4000, 0.25, false},
		{"exactly at threshold boundary", 5000, 1250, 0.25, false},
		{"just below threshold", 5000, 1249, 0.25, true},
		{"low remaining", 5000, 100, 0.25, true},
		{"zero limit means no data", 0, 0, 0.25, false},
		{"10 percent threshold - safe", 5000, 600, 0.10, false},
		{"10 percent threshold - tripped", 5000, 400, 0.10, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headroom := float64(0)
			if tt.limit > 0 {
				headroom = float64(tt.remaining) / float64(tt.limit)
			}
			got := headroom < tt.threshold && tt.limit > 0
			if got != tt.expected {
				t.Errorf("headroom=%v < threshold=%v, limit=%v => got %v, want %v",
					headroom, tt.threshold, tt.limit, got, tt.expected)
			}
		})
	}
}

func TestBuildRepoRefs(t *testing.T) {
	repos := []struct{ Owner, Name string }{
		{"kubernetes", "kubernetes"},
		{"prometheus", "prometheus"},
	}
	refs := BuildRepoRefs(repos)

	if len(refs) != 2 {
		t.Fatalf("len(refs) = %d, want 2", len(refs))
	}
	if refs[0].Owner != "kubernetes" || refs[0].Name != "kubernetes" {
		t.Errorf("refs[0] = %+v, want {kubernetes kubernetes}", refs[0])
	}
	if refs[1].Owner != "prometheus" || refs[1].Name != "prometheus" {
		t.Errorf("refs[1] = %+v, want {prometheus prometheus}", refs[1])
	}
}

func TestBuildRepoRefsFromConfig(t *testing.T) {
	refs := BuildRepoRefsFromConfig([]struct{ Repo string }{
		{"kubernetes/kubernetes"},
		{"prometheus/prometheus"},
		{"invalid"},
	})

	if len(refs) != 2 {
		t.Fatalf("len(refs) = %d, want 2 (invalid skipped)", len(refs))
	}
}

func TestSplitRepo(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"kubernetes/kubernetes", []string{"kubernetes", "kubernetes"}},
		{"owner/repo/name", []string{"owner", "repo/name"}},
		{"noslash", nil},
		{"", nil},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := splitRepo(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("splitRepo(%q) = %v, want %v", tt.input, got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("splitRepo(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

// ISI-922 regression test: UpdateStoreFromCollected with Partial=true must
// preserve absolute values (Stars, Forks, Contributors, GrowthScore) from
// the previous state instead of overwriting them with gharchive delta counts.
func TestUpdateStoreFromCollected_PartialPreservesAbsoluteValues(t *testing.T) {
	store := state.NewStore("")

	store.SetRepoState("kubernetes/kubernetes", state.RepoState{
		Owner:                 "kubernetes",
		Name:                  "kubernetes",
		Stars:                 105000,
		Forks:                 38000,
		Contributors:          4200,
		GrowthScore:           87.5,
		StarVelocity:          150.0,
		StarAcceleration:      2.1,
		ForkVelocity:          30.0,
		PRVelocity:            12.0,
		IssueVelocity:         8.0,
		ContributorGrowth:     0.05,
		NormalizedGrowthScore: 0.92,
		RecentReleaseDates:    []time.Time{time.Now()},
		LatestReleaseAt:       time.Now().Add(-24 * time.Hour),
		ETag:                  "abc123",
		LastModified:          "Wed, 08 May 2026 00:00:00 GMT",
		LastCollected:         time.Now().Add(-1 * time.Hour),
	})

	partialResults := []CollectedMetrics{
		{
			Owner:         "kubernetes",
			Name:          "kubernetes",
			Stars:         5,
			Forks:         2,
			StarVelocity:  7.2,
			ForkVelocity:  2.8,
			PRVelocity:    1.5,
			IssueVelocity: 0.9,
			CollectedAt:   time.Now(),
			Partial:       true,
		},
	}

	UpdateStoreFromCollected(store, partialResults, func(fullName string) *state.RepoState {
		return store.GetRepoState(fullName)
	})

	result := store.GetRepoState("kubernetes/kubernetes")
	if result == nil {
		t.Fatal("repo state is nil after update")
	}

	if result.Stars != 105000 {
		t.Errorf("Stars = %d, want 105000 (absolute preserved, not overwritten by delta 5)", result.Stars)
	}
	if result.Forks != 38000 {
		t.Errorf("Forks = %d, want 38000 (absolute preserved)", result.Forks)
	}
	if result.Contributors != 4200 {
		t.Errorf("Contributors = %d, want 4200 (preserved from previous state)", result.Contributors)
	}
	if result.GrowthScore != 87.5 {
		t.Errorf("GrowthScore = %v, want 87.5 (preserved from previous state)", result.GrowthScore)
	}
	if result.NormalizedGrowthScore != 0.92 {
		t.Errorf("NormalizedGrowthScore = %v, want 0.92 (preserved)", result.NormalizedGrowthScore)
	}
	if result.StarVelocity != 7.2 {
		t.Errorf("StarVelocity = %v, want 7.2 (updated from partial)", result.StarVelocity)
	}
	if result.ForkVelocity != 2.8 {
		t.Errorf("ForkVelocity = %v, want 2.8 (updated from partial)", result.ForkVelocity)
	}
	if result.PRVelocity != 1.5 {
		t.Errorf("PRVelocity = %v, want 1.5 (updated from partial)", result.PRVelocity)
	}
	if result.IssueVelocity != 0.9 {
		t.Errorf("IssueVelocity = %v, want 0.9 (updated from partial)", result.IssueVelocity)
	}
	if result.ETag != "abc123" {
		t.Errorf("ETag = %q, want abc123 (preserved)", result.ETag)
	}
}

// ISI-922: Non-partial (live API) results should still overwrite completely.
func TestUpdateStoreFromCollected_FullOverwritesCompletely(t *testing.T) {
	store := state.NewStore("")

	store.SetRepoState("kubernetes/kubernetes", state.RepoState{
		Owner:         "kubernetes",
		Name:          "kubernetes",
		Stars:         105000,
		Forks:         38000,
		ETag:          "old-etag",
		LastCollected: time.Now().Add(-2 * time.Hour),
	})

	fullResults := []CollectedMetrics{
		{
			Owner:       "kubernetes",
			Name:        "kubernetes",
			Stars:       105100,
			Forks:       38010,
			CollectedAt: time.Now(),
		},
	}

	UpdateStoreFromCollected(store, fullResults, func(fullName string) *state.RepoState {
		return store.GetRepoState(fullName)
	})

	result := store.GetRepoState("kubernetes/kubernetes")
	if result == nil {
		t.Fatal("repo state is nil after update")
	}

	if result.Stars != 105100 {
		t.Errorf("Stars = %d, want 105100 (full overwrite)", result.Stars)
	}
	if result.Forks != 38010 {
		t.Errorf("Forks = %d, want 38010 (full overwrite)", result.Forks)
	}
	if result.ETag != "old-etag" {
		t.Errorf("ETag = %q, want old-etag (preserved from prev)", result.ETag)
	}
}
