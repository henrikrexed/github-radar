package metrics

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// helper: build a meter wired to an in-memory ManualReader so tests can
// inspect emitted points without involving the OTLP exporter.
func newTestMeter(t *testing.T) (*metric.MeterProvider, *metric.ManualReader) {
	t.Helper()
	r := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(r))
	t.Cleanup(func() {
		_ = mp.Shutdown(context.Background())
	})
	return mp, r
}

func TestNewDiscoveryMeters_NilMeterRejected(t *testing.T) {
	if _, err := NewDiscoveryMeters(nil); err == nil {
		t.Fatal("NewDiscoveryMeters(nil) err = nil, want non-nil")
	}
}

func TestNewDiscoveryMeters_RegistersAllInstruments(t *testing.T) {
	mp, _ := newTestMeter(t)
	dm, err := NewDiscoveryMeters(mp.Meter("github-radar-test"))
	if err != nil {
		t.Fatalf("NewDiscoveryMeters err = %v, want nil", err)
	}

	// All 5 instruments must be non-nil. The spec pins the field set in
	// docs/observability/gharchive-instrumentation-spec.md §Metric inventory.
	if dm.LagSeconds == nil {
		t.Error("LagSeconds = nil, want instrument")
	}
	if dm.CandidatesTotal == nil {
		t.Error("CandidatesTotal = nil, want instrument")
	}
	if dm.QueueDepth == nil {
		t.Error("QueueDepth = nil, want instrument")
	}
	if dm.DedupRatio == nil {
		t.Error("DedupRatio = nil, want instrument")
	}
	if dm.EventsProcessed == nil {
		t.Error("EventsProcessed = nil, want instrument")
	}
}

// gatherMetricNames collects emitted metric names from the reader for
// shape assertions.
func gatherMetricNames(t *testing.T, r *metric.ManualReader) []string {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := r.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect err = %v, want nil", err)
	}
	var names []string
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			names = append(names, m.Name)
		}
	}
	return names
}

func TestDiscoveryMeters_EmissionShape(t *testing.T) {
	mp, r := newTestMeter(t)
	dm, err := NewDiscoveryMeters(mp.Meter("github-radar-test"))
	if err != nil {
		t.Fatalf("NewDiscoveryMeters err = %v, want nil", err)
	}

	ctx := context.Background()
	dm.RecordLagSeconds(ctx, 42.5)
	dm.AddEventsProcessed(ctx, "WatchEvent", 17)
	dm.AddCandidates(ctx, "ForkEvent", 3)
	dm.RecordDedupRatio(ctx, 0.95)
	dm.RecordQueueDepth(ctx, 128)

	names := gatherMetricNames(t, r)
	want := map[string]bool{
		"github_radar.discovery.gharchive.lag_seconds":            false,
		"github_radar.discovery.gharchive.events_processed_total": false,
		"github_radar.discovery.gharchive.candidates_total":       false,
		"github_radar.discovery.gharchive.dedup_ratio":            false,
		"github_radar.discovery.classifier.queue_depth":           false,
	}
	for _, n := range names {
		if _, ok := want[n]; ok {
			want[n] = true
		}
	}
	for n, seen := range want {
		if !seen {
			t.Errorf("metric %q not emitted; got names: %v", n, names)
		}
	}
}

// TestDiscoveryMeters_EventTypeAttributeBound — the cardinality-bound
// guard from the spec §Tests engineering should add #3. We feed 10k
// AddEventsProcessed calls drawn from a 4-element enum and assert the
// EventsProcessed metric only produces 4 series, not 10k.
func TestDiscoveryMeters_EventTypeAttributeBound(t *testing.T) {
	mp, r := newTestMeter(t)
	dm, err := NewDiscoveryMeters(mp.Meter("github-radar-test"))
	if err != nil {
		t.Fatalf("NewDiscoveryMeters err = %v, want nil", err)
	}

	ctx := context.Background()
	types := []string{"WatchEvent", "ForkEvent", "PushEvent", "PullRequestEvent"}
	for i := 0; i < 10_000; i++ {
		dm.AddEventsProcessed(ctx, types[i%len(types)], 1)
	}

	var rm metricdata.ResourceMetrics
	if err := r.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect err = %v, want nil", err)
	}

	var seriesCount int
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "github_radar.discovery.gharchive.events_processed_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("events_processed_total data type = %T, want Sum[int64]", m.Data)
			}
			seriesCount = len(sum.DataPoints)
		}
	}
	if seriesCount != len(types) {
		t.Errorf("events_processed_total series = %d, want %d (one per event_type enum value)", seriesCount, len(types))
	}
}

// TestDiscoveryMeters_NilReceiverSafe — methods on a nil *DiscoveryMeters
// must be no-ops so callers that disable telemetry don't need a guard.
func TestDiscoveryMeters_NilReceiverSafe(t *testing.T) {
	var dm *DiscoveryMeters
	ctx := context.Background()
	// None of these should panic.
	dm.RecordLagSeconds(ctx, 1.0)
	dm.AddEventsProcessed(ctx, "WatchEvent", 1)
	dm.AddCandidates(ctx, "WatchEvent", 1)
	dm.RecordDedupRatio(ctx, 0.5)
	dm.RecordQueueDepth(ctx, 10)
}

// TestDiscoveryMeters_EmptyEventTypeFallsBackToUnknown — callers that
// emit without a per-type breakdown (Story 1 as-shipped scalar hook)
// pass eventType = "" or EventTypeUnknown. Both must produce the same
// series rather than the empty string (which OTel exporters drop or
// flag as malformed).
func TestDiscoveryMeters_EmptyEventTypeFallsBackToUnknown(t *testing.T) {
	mp, r := newTestMeter(t)
	dm, err := NewDiscoveryMeters(mp.Meter("github-radar-test"))
	if err != nil {
		t.Fatalf("NewDiscoveryMeters err = %v, want nil", err)
	}

	ctx := context.Background()
	dm.AddEventsProcessed(ctx, "", 1)
	dm.AddEventsProcessed(ctx, EventTypeUnknown, 1)

	var rm metricdata.ResourceMetrics
	if err := r.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect err = %v, want nil", err)
	}

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "github_radar.discovery.gharchive.events_processed_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("events_processed_total data type = %T, want Sum[int64]", m.Data)
			}
			if len(sum.DataPoints) != 1 {
				t.Errorf("series count = %d, want 1 (empty event_type must fold into the unknown bucket)", len(sum.DataPoints))
			}
			if len(sum.DataPoints) > 0 {
				attr, _ := sum.DataPoints[0].Attributes.Value("event_type")
				if attr.AsString() != EventTypeUnknown {
					t.Errorf("event_type attribute = %q, want %q", attr.AsString(), EventTypeUnknown)
				}
			}
		}
	}
}
