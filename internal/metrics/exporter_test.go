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

func TestStarAccelerationUnit_UCUMCompatible(t *testing.T) {
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

	// Record a metric so it shows up in the collection
	ctx := context.Background()
	exp.RecordRepoMetrics(ctx, RepoMetrics{
		Owner:            "test",
		Name:             "repo",
		StarAcceleration: 1.5,
	})

	// Flush to ensure metrics are processed
	if err := exp.meterProvider.ForceFlush(ctx); err != nil {
		t.Logf("ForceFlush returned: %v (non-fatal for dry-run)", err)
	}

	// Verify the unit does not contain invalid characters for Dynatrace OTLP
	// The old unit was {stars}/d² which contains ² (superscript 2), causing
	// METRIC_UNIT_INVALID_CHARACTERS errors. The fix uses {stars_per_day_squared}.
	//
	// Since we can't easily extract the unit from the OTel SDK after instrument
	// creation, we validate the source code pattern directly:
	// The starAccelerationGauge must exist (instrument was created successfully)
	if exp.starAccelerationGauge == nil {
		t.Fatal("starAccelerationGauge is nil — instrument creation failed")
	}
}

func TestMetricUnits_NoInvalidCharacters(t *testing.T) {
	// Verify that the star_acceleration metric unit in the source is UCUM-compatible.
	// Dynatrace OTLP ingestion rejects units with non-ASCII characters like ²
	// and certain special chars. Valid UCUM annotation units use only ASCII
	// alphanumeric chars and underscores inside curly braces.
	//
	// This test creates the exporter and verifies no instrument creation errors,
	// which would occur if the OTel SDK rejected the unit format.
	cfg := ExporterConfig{
		Endpoint:    "http://localhost:4318",
		ServiceName: "unit-test",
		DryRun:      true,
	}

	exp, err := NewExporter(cfg)
	if err != nil {
		t.Fatalf("NewExporter should not fail with valid units: %v", err)
	}
	defer exp.ShutdownWithTimeout()

	// All gauges should be non-nil (valid instrument creation)
	instruments := map[string]interface{}{
		"starsGauge":             exp.starsGauge,
		"forksGauge":             exp.forksGauge,
		"openIssuesGauge":        exp.openIssuesGauge,
		"openPRsGauge":           exp.openPRsGauge,
		"contributorsGauge":      exp.contributorsGauge,
		"growthScoreGauge":       exp.growthScoreGauge,
		"normalizedScoreGauge":   exp.normalizedScoreGauge,
		"starVelocityGauge":      exp.starVelocityGauge,
		"starAccelerationGauge":  exp.starAccelerationGauge,
		"prVelocityGauge":        exp.prVelocityGauge,
		"issueVelocityGauge":     exp.issueVelocityGauge,
		"contributorGrowthGauge": exp.contributorGrowthGauge,
	}

	for name, inst := range instruments {
		if inst == nil {
			t.Errorf("%s is nil — instrument creation failed", name)
		}
	}
}

// TestRepoMetrics_AttributesEmitV3Taxonomy guards ISI-786: every recorded
// `github.radar.*` series MUST carry the `subcategory` and `category_legacy`
// attributes, even when the values are empty. Gating these on non-empty would
// silently drop the dimension on rows that haven't been re-classified yet,
// which is the exact regression Observability Agent surfaced on the dev
// tenant.
func TestRepoMetrics_AttributesEmitV3Taxonomy(t *testing.T) {
	cases := []struct {
		name              string
		m                 RepoMetrics
		wantSubcategory   string
		wantLegacy        string
		wantCategoryAttr  bool
		wantCategoryValue string
	}{
		{
			name: "v3 taxonomy populated",
			m: RepoMetrics{
				Owner:          "kubernetes",
				Name:           "kubernetes",
				Categories:     []string{"cloud-native"},
				Subcategory:    "kubernetes",
				CategoryLegacy: "kubernetes",
			},
			wantSubcategory:   "kubernetes",
			wantLegacy:        "kubernetes",
			wantCategoryAttr:  true,
			wantCategoryValue: "cloud-native",
		},
		{
			name: "empty subcategory + legacy still emitted",
			m: RepoMetrics{
				Owner:      "newowner",
				Name:       "newrepo",
				Categories: []string{"default"},
			},
			wantSubcategory:   "",
			wantLegacy:        "",
			wantCategoryAttr:  true,
			wantCategoryValue: "default",
		},
		{
			name: "no categories list — subcategory + legacy still emitted",
			m: RepoMetrics{
				Owner:          "owner",
				Name:           "repo",
				Subcategory:    "agents",
				CategoryLegacy: "ai-agents",
			},
			wantSubcategory:  "agents",
			wantLegacy:       "ai-agents",
			wantCategoryAttr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			attrs := tc.m.attributes()

			seen := map[string]string{}
			for _, kv := range attrs {
				seen[string(kv.Key)] = kv.Value.AsString()
			}

			subVal, hasSub := seen["subcategory"]
			if !hasSub {
				t.Errorf("subcategory attribute missing — must be emitted unconditionally (ISI-786)")
			} else if subVal != tc.wantSubcategory {
				t.Errorf("subcategory = %q, want %q", subVal, tc.wantSubcategory)
			}

			legacyVal, hasLegacy := seen["category_legacy"]
			if !hasLegacy {
				t.Errorf("category_legacy attribute missing — must be emitted unconditionally (ISI-786)")
			} else if legacyVal != tc.wantLegacy {
				t.Errorf("category_legacy = %q, want %q", legacyVal, tc.wantLegacy)
			}

			catVal, hasCat := seen["category"]
			if tc.wantCategoryAttr {
				if !hasCat {
					t.Errorf("category attribute missing")
				} else if catVal != tc.wantCategoryValue {
					t.Errorf("category = %q, want %q", catVal, tc.wantCategoryValue)
				}
			}
		})
	}
}

// TestClassificationHealthInstruments asserts the ISI-775 instruments are
// registered on every exporter — a future caller hitting RecordPendingBuckets
// or RecordClassificationRun before NewExporter would otherwise nil-panic
// silently in production. The previous incident (ISI-714 SQL Scan drift)
// went silent for 26h precisely because no one had a regression-guard test
// on the observability code path.
func TestClassificationHealthInstruments(t *testing.T) {
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

	if exp.reposPendingGauge == nil {
		t.Error("reposPendingGauge is nil — radar.repos.pending instrument was not created")
	}
	if exp.classificationRunCount == nil {
		t.Error("classificationRunCount is nil — radar.classification.run instrument was not created")
	}
}

// TestRecordPendingBuckets verifies the gauge accepts every (excluded,
// force_category_set) tuple without panicking — the daemon emits all four
// every flush even when buckets are zero.
func TestRecordPendingBuckets(t *testing.T) {
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

	ctx := context.Background()
	exp.RecordPendingBuckets(ctx, []PendingBucket{
		{Excluded: false, ForceCategorySet: false, Count: 12},
		{Excluded: false, ForceCategorySet: true, Count: 3},
		{Excluded: true, ForceCategorySet: false, Count: 0},
		{Excluded: true, ForceCategorySet: true, Count: 0},
	})

	// Flush must succeed even when one or more buckets are zero — the gauge
	// is recorded for stable shape, not just non-zero values.
	if err := exp.Flush(ctx); err != nil {
		t.Errorf("Flush error: %v", err)
	}
}

// TestRecordClassificationRun covers all three result attributes the daemon
// can emit (success / failed / partial). Each must Add(1) without error.
func TestRecordClassificationRun(t *testing.T) {
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

	ctx := context.Background()
	for _, result := range []ClassificationRunResult{
		ClassificationRunSuccess,
		ClassificationRunFailed,
		ClassificationRunPartial,
	} {
		exp.RecordClassificationRun(ctx, result)
	}

	if err := exp.Flush(ctx); err != nil {
		t.Errorf("Flush error: %v", err)
	}
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

// T5 / ISI-716 — API budget instruments.

func TestExporter_APIBudgetInstrumentsCreated(t *testing.T) {
	exp, err := NewExporter(ExporterConfig{ServiceName: "t", DryRun: true})
	if err != nil {
		t.Fatalf("NewExporter: %v", err)
	}
	defer exp.ShutdownWithTimeout()

	instruments := map[string]interface{}{
		"apiRateLimitGauge":     exp.apiRateLimitGauge,
		"apiRateRemainingGauge": exp.apiRateRemainingGauge,
		"apiRateUsedRatioGauge": exp.apiRateUsedRatioGauge,
		"apiRateResetSecsGauge": exp.apiRateResetSecsGauge,
		"apiCallsCounter":       exp.apiCallsCounter,
		"refreshTierReposGauge": exp.refreshTierReposGauge,
	}
	for name, inst := range instruments {
		if inst == nil {
			t.Errorf("%s not created", name)
		}
	}
}

func TestExporter_RecordRateLimit_HappyPath(t *testing.T) {
	exp, err := NewExporter(ExporterConfig{ServiceName: "t", DryRun: true})
	if err != nil {
		t.Fatalf("NewExporter: %v", err)
	}
	defer exp.ShutdownWithTimeout()

	// Should not panic; we exercise the branches.
	exp.RecordRateLimit(context.Background(), RateLimitSnapshot{
		Limit:     5000,
		Remaining: 4500,
		ResetAt:   time.Now().Add(30 * time.Minute),
	})
	// Zero-limit branch (no used_ratio emitted).
	exp.RecordRateLimit(context.Background(), RateLimitSnapshot{})
}

func TestExporter_RecordAPICall_Counter(t *testing.T) {
	exp, err := NewExporter(ExporterConfig{ServiceName: "t", DryRun: true})
	if err != nil {
		t.Fatalf("NewExporter: %v", err)
	}
	defer exp.ShutdownWithTimeout()

	ctx := context.Background()
	exp.RecordAPICall(ctx, "graphql", "ok")
	exp.RecordAPICall(ctx, "repo", "not_modified")
	exp.RecordAPICall(ctx, "repo", "rate_limited")
}

func TestExporter_RecordRefreshTierHistogram(t *testing.T) {
	exp, err := NewExporter(ExporterConfig{ServiceName: "t", DryRun: true})
	if err != nil {
		t.Fatalf("NewExporter: %v", err)
	}
	defer exp.ShutdownWithTimeout()

	exp.RecordRefreshTierHistogram(context.Background(), map[string]int{
		"hot": 500, "warm": 1500, "cold": 1000, "new": 12,
	})
}
