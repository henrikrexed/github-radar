package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/hrexed/github-radar/internal/config"
	"github.com/hrexed/github-radar/internal/discovery"
	"github.com/hrexed/github-radar/internal/metrics"
)

// gharchive_wiring.go bridges the user-facing config block
// `discovery.sources.gharchive.*` (Story 3, [ISI-953]) to the
// in-discoverer collector built in Story 1+2 ([ISI-951], [ISI-952]).
//
// Architecture decisions are locked in [ISI-964 plan](/ISI/issues/ISI-964#document-plan):
//
//   - Decision 1: build a *separate* GHArchiveSource for the discovery
//     pipeline. The metrics-collector branch (`cfg.Collector.GHArchive`)
//     in daemon.go stays untouched.
//   - Decision 2: the mapping helper lives here in `internal/daemon`
//     (the composition root) rather than `internal/config`, which must
//     stay a leaf with no domain-package imports.
//   - Decision 3: wiring runs after `disc` is constructed and before
//     any DiscoverAll cycle.
//
// The two helpers split the user-facing struct along its two
// downstream consumers:
//
//   - mapDiscoveryGHArchiveConfig produces the
//     discovery.GHArchiveSourceConfig that gates Source (5) promotion
//     in DiscoverAll (TopN, ActivityFloor, MinStarsGate). This is the
//     shape that lands in the Discoverer's `Config.Sources.GHArchive`
//     block at NewDiscoverer time.
//   - mapDiscoveryGHArchiveCollectorConfig produces the
//     discovery.GHArchiveConfig consumed by NewGHArchiveSource
//     (Window, EventTypes — every other knob falls through to
//     DefaultGHArchive*).
//
// wireDiscoveryGHArchive composes both: build the collector, register
// it with the Discoverer.

// mapDiscoveryGHArchiveConfig translates the user-facing Story 3
// config shape (config.DiscoveryGHArchiveConfig) into the in-discoverer
// shape (discovery.GHArchiveSourceConfig) consumed by
// DiscoverFromGHArchive.
//
// Only the fields that gate Source (5) promotion travel here. Knobs
// that drive the collector lifecycle (window, event types) go through
// mapDiscoveryGHArchiveCollectorConfig instead.
//
// Named per the [ISI-967 acceptance criteria](/ISI/issues/ISI-967):
// "Mapping helper in internal/daemon translates
// config.DiscoveryGHArchiveConfig → discovery.GHArchiveSourceConfig".
func mapDiscoveryGHArchiveConfig(cfg config.DiscoveryGHArchiveConfig) discovery.GHArchiveSourceConfig {
	return discovery.GHArchiveSourceConfig{
		Enabled:       cfg.Enabled,
		TopN:          cfg.TopNPerHour,
		ActivityFloor: cfg.ActivityFloor,
		MinStarsGate:  cfg.MinStarsGate,
	}
}

// mapDiscoveryGHArchiveCollectorConfig translates the user-facing
// Story 3 config shape into the collector-knob shape
// (discovery.GHArchiveConfig) consumed by NewGHArchiveSource.
//
// Only fields the user can override land here; HTTP timeout, retry
// budget, and base URL fall through to the DefaultGHArchive* values
// inside GHArchiveConfig.withDefaults().
//
// EventTypes is copied so a later mutation of the user config can't
// race with the live collector.
func mapDiscoveryGHArchiveCollectorConfig(cfg config.DiscoveryGHArchiveConfig) discovery.GHArchiveConfig {
	var window time.Duration
	if cfg.WindowHours > 0 {
		window = time.Duration(cfg.WindowHours) * time.Hour
	}
	var eventTypes []string
	if len(cfg.EventTypes) > 0 {
		eventTypes = append(make([]string, 0, len(cfg.EventTypes)), cfg.EventTypes...)
	}
	return discovery.GHArchiveConfig{
		Window:     window,
		EventTypes: eventTypes,
	}
}

// wireDiscoveryGHArchive constructs a *discovery.GHArchiveSource from
// the user-facing config and registers it with the Discoverer.
//
// When cfg.Enabled is false the function is a no-op — the Discoverer
// keeps `nil` for its gharchive source and DiscoverFromGHArchive
// returns `(nil, nil)` per its existing contract, preserving the
// pre-Path-C behaviour.
//
// When cfg.Enabled is true the cursor store must be non-nil; the
// underlying NewGHArchiveSource panics on a nil cursor store, so we
// surface a clear error here instead.
//
// dm is the [ISI-955] discovery-meters registry. A nil value disables
// gharchive discovery telemetry without affecting the source's data
// flow — the source falls back to no-op hooks. Pass the meters built
// from the same Exporter that the rest of the daemon uses so all
// github_radar.* series share service.name + resource attributes.
//
// hookCtx is the lifetime context for the metric-emission closures.
// Pass the daemon root context.
//
// The function is intentionally separate from daemon.New so it can be
// exercised in unit tests with an in-memory cursor store
// (see gharchive_wiring_test.go).
func wireDiscoveryGHArchive(
	hookCtx context.Context,
	disc *discovery.Discoverer,
	cfg config.DiscoveryGHArchiveConfig,
	cursorStore discovery.GHArchiveCursorStore,
	dm *metrics.DiscoveryMeters,
) error {
	if disc == nil {
		return fmt.Errorf("gharchive discovery wiring: nil Discoverer")
	}
	if !cfg.Enabled {
		return nil
	}
	if cursorStore == nil {
		return fmt.Errorf("gharchive discovery wiring: cursor store is required when discovery.sources.gharchive.enabled=true")
	}

	collectorCfg := mapDiscoveryGHArchiveCollectorConfig(cfg)
	hooks := newGHArchiveDiscoveryHooks(hookCtx, dm)
	src := discovery.NewGHArchiveSource(collectorCfg, cursorStore, nil, hooks)
	disc.SetGHArchiveSource(src)
	return nil
}
