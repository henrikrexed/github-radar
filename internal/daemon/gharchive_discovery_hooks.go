package daemon

import (
	"context"

	"github.com/hrexed/github-radar/internal/discovery"
	"github.com/hrexed/github-radar/internal/metrics"
)

// gharchive_discovery_hooks.go bridges the discovery.GHArchiveHooks
// callback surface ([ISI-951]) to the OTel instruments registered in
// internal/metrics/discovery_meters.go ([ISI-955]).
//
// Two hooks are wired up here today:
//
//   - OnLagSeconds       → DiscoveryMeters.RecordLagSeconds
//   - OnEventsProcessed  → DiscoveryMeters.AddEventsProcessed (scalar)
//
// OnArchiveStart / OnArchiveComplete / OnArchiveError carry no spec
// instrument today and stay unwired. If a future story adds an
// archive-duration histogram or per-attempt error counter, this is the
// composition point.
//
// Known gap: the events_processed_total counter is emitted with
// event_type = metrics.EventTypeUnknown until the [ISI-961] hook
// signature change lands (keptPerType map[string]int64). The dashboard
// tile "Events processed by type" will render a single "unknown" row
// under that arrangement — that's intentional, the alternative is to
// leave the soak completely un-instrumented while we wait for ISI-961.
// Once ISI-961 lands, swap to a range-over-map emission and the same
// dashboard tile starts rendering one row per real event_type with no
// further dashboard JSON change.

// newGHArchiveDiscoveryHooks returns a discovery.GHArchiveHooks
// populated with closures that emit telemetry via the supplied meters.
// A nil DiscoveryMeters is treated as a "no telemetry this run"
// configuration — the returned hooks struct is empty and the
// gharchive source falls back to no-op callbacks.
//
// ctx is the daemon root context; it is captured by the closures so
// individual archive callbacks don't have to thread a context through
// the hook surface (which is intentionally context-free per Story 1's
// design).
func newGHArchiveDiscoveryHooks(ctx context.Context, dm *metrics.DiscoveryMeters) discovery.GHArchiveHooks {
	if dm == nil {
		return discovery.GHArchiveHooks{}
	}
	return discovery.GHArchiveHooks{
		OnLagSeconds: func(seconds float64) {
			dm.RecordLagSeconds(ctx, seconds)
		},
		OnEventsProcessed: func(_ string, kept, _ int64) {
			// Scalar `kept` cannot drive a per-event_type breakdown.
			// Emit under the EventTypeUnknown sentinel so the counter
			// still produces a stable series shape; ISI-961 will widen
			// the signature to keptPerType and unblock the real
			// breakdown without touching this file's call site.
			dm.AddEventsProcessed(ctx, metrics.EventTypeUnknown, kept)
		},
	}
}
