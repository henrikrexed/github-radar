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
<<<<<<< HEAD
		OnEventsProcessed: func(_ string, keptByType map[string]int64, _ int64) {
			for eventType, count := range keptByType {
				dm.AddEventsProcessed(ctx, eventType, count)
			}
		},
	}
}

// newGHArchivePipelineHooks returns a discovery.GHArchivePipelineHooks
// populated with closures that emit pipeline-level telemetry via the
// supplied meters ([ISI-1005]). A nil DiscoveryMeters produces empty
// hooks (no-ops).
//
// Two closures are wired:
//   - OnCandidateAdmitted → DiscoveryMeters.AddCandidates(ctx, eventType, 1)
//   - OnDedupComplete → computes ratio = dropped / total (guarding
//     divide-by-zero), then DiscoveryMeters.RecordDedupRatio(ctx, ratio)
func newGHArchivePipelineHooks(ctx context.Context, dm *metrics.DiscoveryMeters) discovery.GHArchivePipelineHooks {
	if dm == nil {
		return discovery.GHArchivePipelineHooks{}
	}
	return discovery.GHArchivePipelineHooks{
		OnCandidateAdmitted: func(eventType string) {
			dm.AddCandidates(ctx, eventType, 1)
		},
		OnDedupComplete: func(total, dropped int) {
			if total > 0 {
				ratio := float64(dropped) / float64(total)
				dm.RecordDedupRatio(ctx, ratio)
			}
		},
	}
}
