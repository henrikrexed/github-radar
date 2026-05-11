// Package metrics — discovery_meters.go isolates the gharchive-discovery
// pipeline instruments from the data-export instruments in exporter.go.
// The shape is the contract pinned in
// docs/observability/gharchive-instrumentation-spec.md ([ISI-955]).
//
// Namespace policy (per spec): pipeline-health metrics live under
//
//	github_radar.<pipeline>.*   (this file)
//
// not under github.<entity>.* (which is reserved for observations *about*
// GitHub data — stars, forks, rate-limit headers, etc.).
//
// All 5 instruments are registered up-front so the dashboard JSON can
// reference stable names from day one. Wiring to actual emission sites
// is the caller's job — see internal/daemon/gharchive_discovery_hooks.go
// for the discovery-source hooks (lag_seconds, events_processed_total)
// and the Story 2 / Story 4 follow-ups for candidates_total, dedup_ratio,
// classifier.queue_depth.
package metrics

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// DiscoveryMeters holds the gharchive-discovery pipeline instruments.
// Instances are created via NewDiscoveryMeters and wired into the
// hook-callback surface exposed by internal/discovery.GHArchiveHooks.
//
// Field ordering mirrors the spec table in
// docs/observability/gharchive-instrumentation-spec.md §"Metric inventory".
type DiscoveryMeters struct {
	// LagSeconds — gauge of the most-recently-processed archive's age in
	// seconds. Steady-state: ~5-80 min while the cursor keeps up.
	LagSeconds metric.Float64Gauge

	// CandidatesTotal — counter incremented when a repo passes top-N +
	// activity floor in the gharchive pipeline (Story 2). Carries
	// event_type ∈ {WatchEvent, ForkEvent, PushEvent, PullRequestEvent}.
	CandidatesTotal metric.Int64Counter

	// QueueDepth — gauge of pending candidates queued for classification.
	// Emission lives in Story 4 backpressure code (ISI-954).
	QueueDepth metric.Int64Gauge

	// DedupRatio — gauge published per archive, value = dropped /
	// total_candidates_in_archive. Steady-state target > 0.9 (high =
	// healthy dedup).
	DedupRatio metric.Float64Gauge

	// EventsProcessed — counter of raw gharchive events processed after
	// the event-type filter. Carries event_type per the spec; until
	// ISI-961 widens the hook signature to per-type-map, callers without
	// a per-type breakdown should emit with the unknown-type sentinel
	// (see EventTypeUnknown below).
	EventsProcessed metric.Int64Counter
}

// EventTypeUnknown is the sentinel attribute value used when emission
// callers do not yet have a per-type breakdown (e.g. Story 1's
// as-shipped scalar OnEventsProcessed hook, until ISI-961 lands the
// keptPerType signature change). Keeping the attribute key stable
// guarantees the dashboard's "events processed by type" tile renders
// rows even on a degraded emission path.
const EventTypeUnknown = "unknown"

// NewDiscoveryMeters registers the 5 gharchive-discovery instruments on
// the supplied meter. Pass the meter from Exporter.Meter() so the
// instruments land under the same service.name as the rest of
// github-radar telemetry.
//
// A nil meter is invalid — callers should construct the Exporter (and
// therefore the meter) before calling NewDiscoveryMeters.
func NewDiscoveryMeters(meter metric.Meter) (*DiscoveryMeters, error) {
	if meter == nil {
		return nil, fmt.Errorf("metrics: NewDiscoveryMeters requires a non-nil meter")
	}
	dm := &DiscoveryMeters{}
	var err error

	if dm.LagSeconds, err = meter.Float64Gauge(
		"github_radar.discovery.gharchive.lag_seconds",
		metric.WithUnit("s"),
		metric.WithDescription("Age of the most-recently-processed gharchive hourly archive (now - archive_published_at)"),
	); err != nil {
		return nil, fmt.Errorf("lag_seconds: %w", err)
	}

	if dm.CandidatesTotal, err = meter.Int64Counter(
		"github_radar.discovery.gharchive.candidates_total",
		metric.WithUnit("1"),
		metric.WithDescription("Repo candidates surfaced from gharchive event aggregation, tagged by event_type"),
	); err != nil {
		return nil, fmt.Errorf("candidates_total: %w", err)
	}

	if dm.QueueDepth, err = meter.Int64Gauge(
		"github_radar.discovery.classifier.queue_depth",
		metric.WithUnit("1"),
		metric.WithDescription("Pending candidates queued for classifier (co-owned with ISI-954 backpressure)"),
	); err != nil {
		return nil, fmt.Errorf("queue_depth: %w", err)
	}

	if dm.DedupRatio, err = meter.Float64Gauge(
		"github_radar.discovery.gharchive.dedup_ratio",
		metric.WithUnit("1"),
		metric.WithDescription("Fraction of gharchive candidates dropped as already-tracked (dropped / total)"),
	); err != nil {
		return nil, fmt.Errorf("dedup_ratio: %w", err)
	}

	if dm.EventsProcessed, err = meter.Int64Counter(
		"github_radar.discovery.gharchive.events_processed_total",
		metric.WithUnit("1"),
		metric.WithDescription("Raw gharchive events kept after the event-type filter, tagged by event_type"),
	); err != nil {
		return nil, fmt.Errorf("events_processed_total: %w", err)
	}

	return dm, nil
}

// RecordLagSeconds emits a single lag observation. Nil-receiver safe so
// callers in test paths that omit the Exporter wiring don't need a
// special-case branch.
func (dm *DiscoveryMeters) RecordLagSeconds(ctx context.Context, seconds float64) {
	if dm == nil || dm.LagSeconds == nil {
		return
	}
	dm.LagSeconds.Record(ctx, seconds)
}

// AddEventsProcessed increments the events_processed_total counter with
// the supplied event_type attribute. Callers without a per-type
// breakdown (Story 1 as-shipped) should pass EventTypeUnknown — the
// counter still produces a stable series shape for dashboard rendering.
func (dm *DiscoveryMeters) AddEventsProcessed(ctx context.Context, eventType string, count int64) {
	if dm == nil || dm.EventsProcessed == nil || count == 0 {
		return
	}
	if eventType == "" {
		eventType = EventTypeUnknown
	}
	dm.EventsProcessed.Add(ctx, count, metric.WithAttributes(
		attribute.String("event_type", eventType),
	))
}

// AddCandidates increments candidates_total by 1 with the supplied
// event_type attribute. Wired in Story 2 (ISI-952) after the top-N +
// activity-floor selector promotes a repo.
func (dm *DiscoveryMeters) AddCandidates(ctx context.Context, eventType string, count int64) {
	if dm == nil || dm.CandidatesTotal == nil || count == 0 {
		return
	}
	if eventType == "" {
		eventType = EventTypeUnknown
	}
	dm.CandidatesTotal.Add(ctx, count, metric.WithAttributes(
		attribute.String("event_type", eventType),
	))
}

// RecordDedupRatio emits one dedup_ratio gauge value. Callers should
// guard divide-by-zero (totalSelected == 0) and skip emission when
// there's no admitted/dropped sample, since a synthetic 0.0 would be
// indistinguishable from a real "no dedup happened" reading.
func (dm *DiscoveryMeters) RecordDedupRatio(ctx context.Context, ratio float64) {
	if dm == nil || dm.DedupRatio == nil {
		return
	}
	dm.DedupRatio.Record(ctx, ratio)
}

// RecordQueueDepth emits the classifier queue size. Wired in Story 4
// (ISI-954) on each backpressure-gate tick.
func (dm *DiscoveryMeters) RecordQueueDepth(ctx context.Context, depth int64) {
	if dm == nil || dm.QueueDepth == nil {
		return
	}
	dm.QueueDepth.Record(ctx, depth)
}
