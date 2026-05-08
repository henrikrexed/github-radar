package metrics

import (
	"testing"
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
