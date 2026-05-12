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
//   - OnEventsProcessed  → DiscoveryMeters.AddEventsProcessed
//     (one emission per event_type in the keptByType map, post-ISI-961)
//
// OnArchiveStart / OnArchiveComplete / OnArchiveError carry no spec
// instrument today and stay unwired. If a future story adds an
// archive-duration histogram or per-attempt error counter, this is the
// composition point.

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
		OnEventsProcessed: func(_ string, keptByType map[string]int64, _ int64) {
			// Range over the per-event-type counts so the
			// events_processed_total counter renders one series per
			// real event_type. AddEventsProcessed skips zero-count
			// emissions internally, so an empty/nil map is a no-op.
			for eventType, count := range keptByType {
				dm.AddEventsProcessed(ctx, eventType, count)
			}
		},
	}
}
