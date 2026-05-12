# gharchive Discovery — Instrumentation Spec

**Owner:** Observability Agent
**Status:** ready for engineering
**Story:** [ISI-955](/ISI/issues/ISI-955)
**Consumers:** [ISI-951](/ISI/issues/ISI-951) (collector), [ISI-952](/ISI/issues/ISI-952) (pipeline wiring), [ISI-954](/ISI/issues/ISI-954) (backpressure)

This is the durable instrumentation contract for the gharchive **discovery** source. The dashboard tile in [ISI-955](/ISI/issues/ISI-955) and the soak-validation gates in [ISI-956](/ISI/issues/ISI-956) both depend on these names, units, and attribute shapes. If you have to change one, raise it on [ISI-955](/ISI/issues/ISI-955) before merging — the dashboard JSON and the schema registry both reference these names directly.

## Naming policy

Two metric namespaces in the repo. Don't mix them.

| Prefix | Meaning | Example |
|---|---|---|
| `github.<entity>.*` | Observations about *GitHub data* (the things we scrape). | `github.repo.stars`, `github.api.rate_limit.remaining` |
| `github_radar.<pipeline>.*` | Observations about the *GitHub Radar service itself* (its pipeline health). | `github_radar.discovery.gharchive.lag_seconds` |

The 5 new metrics in this spec are all `github_radar.*` because they describe the discovery pipeline, not GitHub data.

> **AC reconciliation note.** [ISI-955](/ISI/issues/ISI-955) AC text writes `candidates_per_hour`, `dedup_rate`, `events_processed` (no `_total` suffix). OTel convention requires monotonic counters to end in `_total` and to publish raw counts (rate is computed at query time, not at emission, so we don't lose data across restarts). This spec uses the OTel-correct names; the dashboard tile labels keep the human-friendly "Candidates / hour" wording.

## Metric inventory

| Instrument name | Type | Unit | Owner story | Notes |
|---|---|---|---|---|
| `github_radar.discovery.gharchive.lag_seconds` | Float64Gauge | `s` | [ISI-951](/ISI/issues/ISI-951) | Set on every archive fetch: `now - archive_published_at`. |
| `github_radar.discovery.gharchive.candidates_total` | Int64Counter | `1` | [ISI-952](/ISI/issues/ISI-952) | Increment when a repo passes top-N + activity floor. Carries `event_type`. |
| `github_radar.discovery.classifier.queue_depth` | Int64Gauge | `1` | [ISI-954](/ISI/issues/ISI-954) | Co-owned: emit from wherever the queue lives, not from the gharchive collector. |
| `github_radar.discovery.gharchive.dedup_ratio` | Float64Gauge | `1` | [ISI-952](/ISI/issues/ISI-952) | One emission per processed archive, value = `dropped / total_candidates_in_archive`. |
| `github_radar.discovery.gharchive.events_processed_total` | Int64Counter | `1` | [ISI-951](/ISI/issues/ISI-951) | Per-event counter, attribute `event_type`. |

## Attribute policy

Only one attribute is allowed in scope: `event_type`.

| Attribute | Allowed values | Cardinality | Carry on |
|---|---|---|---|
| `event_type` | `WatchEvent`, `ForkEvent`, `PushEvent`, `PullRequestEvent` | 4 | `candidates_total`, `events_processed_total` |

**Do not** add `repo_owner`, `repo_name`, or `repo_full_name` to these metrics. Those would explode cardinality (~1k–10k tracked repos × 4 event types = up to 40k series per metric). Per-repo telemetry stays on the existing `github.repo.*` metrics, which already carry those attributes.

## Where to emit

```
internal/
  discovery/
    gharchive_source.go        # ISI-951 — emit lag_seconds, events_processed_total
    discovery.go               # ISI-952 — emit candidates_total, dedup_ratio
    classifier_queue.go        # ISI-954 — emit classifier.queue_depth (new file or wherever queue lives)
  metrics/
    discovery_meters.go        # ISI-951 NEW — define the 5 instruments, mirrors exporter.go pattern
```

A new file `internal/metrics/discovery_meters.go` keeps the discovery instruments separate from the data-export instruments in `exporter.go`. The pattern follows what `exporter.go` already does for the `github.*` namespace.

## Go instrument registration (reference)

Mirror the existing pattern in `internal/metrics/exporter.go:515` (where `github.radar.*` instruments are registered).

```go
package metrics

import (
    "context"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"
)

type DiscoveryMeters struct {
    LagSeconds       metric.Float64Gauge
    CandidatesTotal  metric.Int64Counter
    QueueDepth       metric.Int64Gauge
    DedupRatio       metric.Float64Gauge
    EventsProcessed  metric.Int64Counter
}

func NewDiscoveryMeters(meterName string) (*DiscoveryMeters, error) {
    meter := otel.Meter(meterName)
    var dm DiscoveryMeters
    var err error

    if dm.LagSeconds, err = meter.Float64Gauge(
        "github_radar.discovery.gharchive.lag_seconds",
        metric.WithUnit("s"),
        metric.WithDescription("Age of the most-recently-processed gharchive hourly archive"),
    ); err != nil {
        return nil, err
    }

    if dm.CandidatesTotal, err = meter.Int64Counter(
        "github_radar.discovery.gharchive.candidates_total",
        metric.WithUnit("1"),
        metric.WithDescription("Repo candidates surfaced from gharchive event aggregation"),
    ); err != nil {
        return nil, err
    }

    if dm.QueueDepth, err = meter.Int64Gauge(
        "github_radar.discovery.classifier.queue_depth",
        metric.WithUnit("1"),
        metric.WithDescription("Pending candidates in classifier queue"),
    ); err != nil {
        return nil, err
    }

    if dm.DedupRatio, err = meter.Float64Gauge(
        "github_radar.discovery.gharchive.dedup_ratio",
        metric.WithUnit("1"),
        metric.WithDescription("Fraction of gharchive candidates dropped as already-tracked"),
    ); err != nil {
        return nil, err
    }

    if dm.EventsProcessed, err = meter.Int64Counter(
        "github_radar.discovery.gharchive.events_processed_total",
        metric.WithUnit("1"),
        metric.WithDescription("Raw gharchive events processed after event-type filter"),
    ); err != nil {
        return nil, err
    }

    return &dm, nil
}
```

## Known upstream contract gap — [ISI-961](/ISI/issues/ISI-961)

Story 1 ([ISI-951](/ISI/issues/ISI-951)) landed in `in_review` (`feat/isi-951-gharchive-collector` branch, commit `a45a72d`) with this hook signature in `internal/discovery/gharchive_source.go:224`:

```go
// As-shipped in ISI-951 — INSUFFICIENT for events_processed_total{event_type=...}
type GHArchiveHooks struct {
    OnEventsProcessed func(archive string, kept, discarded int64)
    // ...
}
```

Code review (Code Reviewer, [ISI-955 thread](/ISI/issues/ISI-955)) flagged that scalar `kept` cannot drive the per-`event_type` breakdown the spec locks in below. [ISI-961](/ISI/issues/ISI-961) tracks the signature change to:

```go
// Required for the spec to wire correctly
type GHArchiveHooks struct {
    OnEventsProcessed func(archive string, keptPerType map[string]int64, discarded int64)
    // ...
}
```

**Wire-up sequencing:** wait for [ISI-961](/ISI/issues/ISI-961) to merge before binding the discovery meters into the hooks. Wiring against the as-shipped scalar would emit `events_processed_total` with no `event_type` attribute and break the dashboard's "Events processed by type" tile + the cardinality-bound test (#3 below).

The four other instruments (`lag_seconds`, `dedup_ratio`, `queue_depth`, `candidates_total`) are unaffected by this gap — they pull from different call sites that already have the right shape.

## Emission points (suggested call sites)

```go
// In gharchive_source.go after fetching/parsing one archive:
ageSec := time.Since(archive.PublishedAt).Seconds()
dm.LagSeconds.Record(ctx, ageSec)

// Per archive after ISI-961's signature change lands:
hooks.OnEventsProcessed = func(archive string, keptPerType map[string]int64, discarded int64) {
    for evtType, n := range keptPerType {
        dm.EventsProcessed.Add(ctx, n, metric.WithAttributes(
            attribute.String("event_type", evtType),
        ))
    }
    // discarded counter is informational — keep separate or drop it on the floor
}

// After the per-archive top-N + activity-floor selector runs:
for _, candidate := range selectedCandidates {
    dm.CandidatesTotal.Add(ctx, 1, metric.WithAttributes(
        attribute.String("event_type", candidate.PrimaryEventType),
    ))
}

// After dedup pass on the archive's selected set:
dropped := totalSelected - admitted
ratio := float64(dropped) / float64(totalSelected) // guard divide-by-zero
dm.DedupRatio.Record(ctx, ratio)

// In the classifier loop (ISI-954) on every tick:
dm.QueueDepth.Record(ctx, int64(len(queue)))
```

## Tests engineering should add (Story 1)

1. **Instrument registration test.** Construct `NewDiscoveryMeters`, assert no error, assert all 5 fields non-nil. (Mirror `exporter_test.go` pattern.)
2. **Emission shape test.** Use the OTel SDK manual reader to capture emitted metric points, assert names + unit + cardinality keys exactly match the table above. Catch attribute drift early.
3. **Cardinality bound test.** Emit 10k synthetic events and assert the `events_processed_total` series count is exactly `4` (one per `event_type` enum value), not 10k.

## Validation plan (Observability Agent — once Story 1 lands)

- Run `validate-telemetry-data` against a 1h staging window. Confirm:
  - All 5 names appear in OTLP traffic.
  - Units arrive as `s` and `1` per spec.
  - `event_type` attribute resolves only to the 4 enum values.
  - No `github_radar.*` series carry per-repo attributes.
- Update `dashboards/dt-radar-discovery.json` — flip `DRAFT-PENDING-VALIDATION` markers off.
- Deploy via `dtctl apply -f dashboards/dt-radar-discovery.json` (the `dt-app-dashboards` skill is the preferred path per `AGENTS.md` but isn't installed locally; fallback to direct `dtctl` is acceptable per the skill's deploy-script contract).
- Capture dashboard URL on [ISI-955](/ISI/issues/ISI-955) and link from [ISI-956](/ISI/issues/ISI-956).
