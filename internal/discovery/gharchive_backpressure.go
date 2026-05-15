package discovery

import (
	"sync"
	"time"
)

// gharchive_backpressure.go implements Story 4 of the Path C epic
// ([ISI-954](/ISI/issues/ISI-954)): three independent gates that the
// gharchive discovery step consults before promoting candidates into the
// classifier flow. The gates protect classifier capacity from a runaway
// firehose without coupling the discovery loop to OTel — hooks emit
// metrics out-of-band so [ISI-955](/ISI/issues/ISI-955) can wire the
// dashboard signals without re-touching this file.
//
// Three gates, evaluated together at the top of each discovery cycle:
//
//  1. Classifier queue depth — pause when the (excluded=false,
//     force_category_set=false) bucket of `radar.repos.pending` exceeds
//     ClassifierQueueDepthThreshold. ISI-775 established that bucket as
//     the operationally meaningful "stuck rows" signal; it's also the
//     right brake for "classifier saturated".
//
//  2. Live GitHub API rate-limit consumption — pause when the core REST
//     budget hits RateLimitHeadroomPct (default 80). gharchive hydration
//     uses the core REST quota (separate from Search API), so this gate
//     keeps the budget for live classification rather than discovery.
//
//  3. Daily candidate cap — a 24h rolling counter of emitted candidates
//     with two thresholds per the architect's Q5 refinement (in
//     [ISI-950 plan doc](/ISI/issues/ISI-950#document-plan)):
//
//     - daily_cap_warn (default 4000): emit a warn metric tick; do NOT
//       pause emission. Yellow-light signal for dashboards.
//     - daily_cap_hard (default 5000): pause emission. Circuit-breaker
//       for runaway emission (e.g. gharchive surge, classifier slowdown).
//
//     The counter is rolling (24h sliding window), not calendar-day, so
//     emission resumes naturally as the window slides past historical
//     hours rather than on a wall-clock midnight reset.
//
// The gate is consulted in two places by DiscoverFromGHArchive:
//   - Once at cycle entry (CheckEntry) to short-circuit the whole step
//     when queue depth or rate-limit gates trip.
//   - Once per emitted candidate (RecordEmission) so the daily cap can
//     trip mid-cycle without doing extra REST hydration.

// Default backpressure thresholds. Each is a "soak default" — tunable
// after Stage C metrics land. None of these defaults is "off"; an
// operator must explicitly raise a threshold to weaken protection.
const (
	// DefaultGHArchiveBackpressureClassifierQueueDepth is the default
	// pending-classification queue threshold above which gharchive
	// emission pauses. 1000 is the AC default; matches ~6h of classifier
	// throughput at the soak-default ~150/h cadence.
	DefaultGHArchiveBackpressureClassifierQueueDepth = 1000

	// DefaultGHArchiveBackpressureRateLimitHeadroomPct is the live
	// GitHub core REST consumption percent above which gharchive
	// emission pauses. 80 leaves 20% (1000 req/h authenticated)
	// headroom for live classification + tiered refresh.
	DefaultGHArchiveBackpressureRateLimitHeadroomPct = 80.0

	// DefaultGHArchiveBackpressureDailyCapWarn is the warn threshold
	// for the rolling 24h emission counter. Crossing it fires
	// OnDailyCapState("warn", ...) but does not pause emission.
	DefaultGHArchiveBackpressureDailyCapWarn = 4000

	// DefaultGHArchiveBackpressureDailyCapHard is the hard cap for the
	// rolling 24h emission counter. Crossing it pauses emission until
	// the rolling window slides enough events out to drop back below
	// the threshold.
	DefaultGHArchiveBackpressureDailyCapHard = 5000
)

// Pause-reason tags. Stable strings — emitted into hook callbacks and
// surfaced as a label dimension on the OTel metric in
// [ISI-955](/ISI/issues/ISI-955), so changing them is a dashboard-
// breaking change.
const (
	BackpressureReasonClassifierQueue = "classifier_queue_depth"
	BackpressureReasonRateLimit       = "rate_limit_headroom"
	BackpressureReasonDailyCapHard    = "daily_cap_hard"
)

// Daily-cap state tags. Stable — see the same caveat as the pause-reason
// tags above.
const (
	DailyCapStateOK   = "ok"
	DailyCapStateWarn = "warn"
	DailyCapStateHard = "hard"
)

// GHArchiveBackpressureConfig contains the four thresholds. Embedded
// under GHArchiveSourceConfig so a single yaml block configures the
// whole gate.
//
// All zero values fall back to the soak defaults rather than disabling
// the gate — an operator who omits the block gets full protection. Use
// the parent GHArchiveSourceConfig.Enabled flag, or pass nil to
// SetGHArchiveBackpressure, to disable the whole feature.
type GHArchiveBackpressureConfig struct {
	// ClassifierQueueDepthThreshold pauses emission when the pending-
	// classification queue depth equals or exceeds this value. Zero
	// falls back to DefaultGHArchiveBackpressureClassifierQueueDepth.
	ClassifierQueueDepthThreshold int `yaml:"classifier_queue_depth_threshold"`

	// RateLimitHeadroomPct pauses emission when the live GitHub core
	// REST consumption equals or exceeds this percent (0-100). Zero
	// falls back to DefaultGHArchiveBackpressureRateLimitHeadroomPct.
	RateLimitHeadroomPct float64 `yaml:"rate_limit_headroom_pct"`

	// DailyCapWarn fires the warn metric tick when the rolling 24h
	// emission counter equals or exceeds this value. Does NOT pause
	// emission. Zero falls back to
	// DefaultGHArchiveBackpressureDailyCapWarn.
	DailyCapWarn int `yaml:"daily_cap_warn"`

	// DailyCapHard pauses emission when the rolling 24h counter
	// equals or exceeds this value. Zero falls back to
	// DefaultGHArchiveBackpressureDailyCapHard.
	DailyCapHard int `yaml:"daily_cap_hard"`
}

// withDefaults populates empty fields with soak defaults.
func (c GHArchiveBackpressureConfig) withDefaults() GHArchiveBackpressureConfig {
	if c.ClassifierQueueDepthThreshold <= 0 {
		c.ClassifierQueueDepthThreshold = DefaultGHArchiveBackpressureClassifierQueueDepth
	}
	if c.RateLimitHeadroomPct <= 0 {
		c.RateLimitHeadroomPct = DefaultGHArchiveBackpressureRateLimitHeadroomPct
	}
	if c.DailyCapWarn <= 0 {
		c.DailyCapWarn = DefaultGHArchiveBackpressureDailyCapWarn
	}
	if c.DailyCapHard <= 0 {
		c.DailyCapHard = DefaultGHArchiveBackpressureDailyCapHard
	}
	return c
}

// GHArchiveBackpressureSignals provides the live signals the gate reads
// every cycle. Each func may be nil — a nil signal means the gate is
// "off" (degrades to allow rather than pause), so a misconfigured
// daemon never starves discovery on missing telemetry. Setting a nil
// signal is the right answer in unit tests for the gates we don't
// exercise; setting a constant function is the right answer for the
// gate we do.
type GHArchiveBackpressureSignals struct {
	// ClassifierQueueDepth returns the current pending-classification
	// queue depth. The daemon binds this to db.PendingCountsByDimension's
	// (excluded=false, force_category_set=false) bucket per ISI-775.
	ClassifierQueueDepth func() int

	// RateLimitConsumptionPct returns the percentage of the current
	// GitHub core REST rate-limit window already consumed (0-100). The
	// daemon binds this to client.RateLimitInfo:
	// 100 * (Limit - Remaining) / Limit. Returns 0 when no rate-limit
	// info has been seen yet (so the gate is permissive on cold start).
	RateLimitConsumptionPct func() float64
}

// GHArchiveBackpressureHooks emits per-event metrics. All callbacks are
// optional and must be non-blocking + safe for concurrent invocation;
// the gate calls them while holding its internal mutex, so a slow hook
// stalls all subsequent decisions. [ISI-955](/ISI/issues/ISI-955) wires
// these to OTel.
//
// Fire-once semantics:
//   - OnPause fires once on the OK→paused transition. It also fires
//     when the pause reason changes while still paused (e.g. queue
//     gate clears but daily-cap-hard takes over) so dashboards see the
//     reason rotation.
//   - OnResume fires once on the paused→OK transition with the
//     duration the gate spent in any paused state.
//   - OnDailyCapState fires only on state transitions
//     ({ok→warn, warn→hard, hard→warn, warn→ok, hard→ok, ok→hard}); it
//     does NOT fire on each emission while the state is unchanged.
//   - OnEmissionCounted fires on every RecordEmission call with the
//     post-increment rolling 24h count — useful for a continuous
//     `daily_cap_utilization` gauge per the architect's Story 5 spec.
type GHArchiveBackpressureHooks struct {
	// OnPause fires on OK→paused transitions and on reason changes
	// while paused. Receives the reason tag plus a snapshot of all
	// three signals at decision time so a single metric line carries
	// enough context to triage.
	OnPause func(reason string, queueDepth int, rateLimitPct float64, dailyCount int)

	// OnResume fires on paused→OK transitions with the cumulative
	// pause duration.
	OnResume func(reason string, pauseDuration time.Duration)

	// OnDailyCapState fires on state transitions in the {ok, warn,
	// hard} ladder.
	OnDailyCapState func(state string, dailyCount int)

	// OnEmissionCounted fires on every RecordEmission with the new
	// 24h-rolling total.
	OnEmissionCounted func(dailyCount int)
}

// BackpressureSnapshot captures the gate's input signals at decision
// time so callers can log a single line per decision without re-fetching.
type BackpressureSnapshot struct {
	QueueDepth   int
	RateLimitPct float64
	DailyCount   int
}

// GHArchiveBackpressureGate centralises the three gates plus a rolling
// 24h emission counter. Safe for concurrent use; the discovery cycle
// reads it once at top-of-cycle and once per emission, so contention is
// bounded.
type GHArchiveBackpressureGate struct {
	cfg     GHArchiveBackpressureConfig
	signals GHArchiveBackpressureSignals
	hooks   GHArchiveBackpressureHooks

	mu             sync.Mutex
	paused         bool
	pausedSince    time.Time
	pauseReason    string
	counter        dailyCapCounter
	lastDailyState string

	nowFn func() time.Time
}

// NewGHArchiveBackpressureGate constructs a gate with the given config,
// signals, and hooks. Config defaults are applied; nil signal funcs
// degrade to "gate off" for that signal. Hooks are optional. The clock
// defaults to time.Now().UTC(); SetClock pins it for tests.
func NewGHArchiveBackpressureGate(cfg GHArchiveBackpressureConfig, signals GHArchiveBackpressureSignals, hooks GHArchiveBackpressureHooks) *GHArchiveBackpressureGate {
	return &GHArchiveBackpressureGate{
		cfg:            cfg.withDefaults(),
		signals:        signals,
		hooks:          hooks,
		lastDailyState: DailyCapStateOK,
		nowFn:          func() time.Time { return time.Now().UTC() },
	}
}

// SetClock overrides the clock used for daily-cap window math. Tests
// pin this to a deterministic value; production callers don't need it.
func (g *GHArchiveBackpressureGate) SetClock(now func() time.Time) {
	if now != nil {
		g.nowFn = now
	}
}

// CheckEntry evaluates the queue-depth and rate-limit gates plus the
// daily-cap hard threshold. Called once at the top of
// DiscoverFromGHArchive. Returns allow=true when emission may proceed,
// false otherwise. Fires OnPause / OnResume on state transitions and
// OnDailyCapState on cap-state transitions.
//
// allow=false implies the discovery step should return early with an
// empty result; the caller should skip TopActiveRepos and the per-
// candidate REST hydration entirely.
func (g *GHArchiveBackpressureGate) CheckEntry() (allow bool, reason string, snap BackpressureSnapshot) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.nowFn()
	snap = BackpressureSnapshot{
		QueueDepth:   g.queueDepth(),
		RateLimitPct: g.rateLimitPct(),
		DailyCount:   g.counter.total(now),
	}
	g.transitionDailyStateLocked(snap.DailyCount)

	newReason := g.evaluateReasonLocked(snap)
	if newReason != "" {
		g.enterPausedLocked(now, newReason, snap)
		return false, newReason, snap
	}

	g.exitPausedLocked(now)
	return true, "", snap
}

// RecordEmission counts one candidate against the rolling 24h cap and
// returns whether emission should continue. Called once per candidate
// AFTER dedup and hydration succeed but BEFORE the candidate is
// appended to the discovery result. Fires OnEmissionCounted always and
// OnDailyCapState / OnPause on transitions.
//
// allow=false means the cycle hit the hard cap mid-loop; the caller
// should break out of the candidate loop.
func (g *GHArchiveBackpressureGate) RecordEmission() (allow bool, reason string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.nowFn()
	g.counter.inc(now, 1)
	count := g.counter.total(now)
	if g.hooks.OnEmissionCounted != nil {
		g.hooks.OnEmissionCounted(count)
	}
	g.transitionDailyStateLocked(count)

	if count >= g.cfg.DailyCapHard {
		snap := BackpressureSnapshot{
			QueueDepth:   g.queueDepth(),
			RateLimitPct: g.rateLimitPct(),
			DailyCount:   count,
		}
		g.enterPausedLocked(now, BackpressureReasonDailyCapHard, snap)
		return false, BackpressureReasonDailyCapHard
	}
	return true, ""
}

// IsPaused reports whether the gate's last decision left it paused.
// Useful for tests and dashboards; production code should rely on the
// allow boolean returned by CheckEntry/RecordEmission.
func (g *GHArchiveBackpressureGate) IsPaused() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.paused
}

// DailyCount returns the current rolling 24h emission count.
func (g *GHArchiveBackpressureGate) DailyCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.counter.total(g.nowFn())
}

// evaluateReasonLocked returns the pause reason matching the current
// snapshot, or "" when no gate trips. Caller must hold g.mu.
//
// Ordering: queue-depth wins ties, then rate-limit, then daily-cap-hard.
// Stable order keeps dashboards readable when multiple gates trip
// simultaneously — the most-acute classifier signal (queue full) is
// surfaced first.
func (g *GHArchiveBackpressureGate) evaluateReasonLocked(snap BackpressureSnapshot) string {
	if g.signals.ClassifierQueueDepth != nil && snap.QueueDepth >= g.cfg.ClassifierQueueDepthThreshold {
		return BackpressureReasonClassifierQueue
	}
	if g.signals.RateLimitConsumptionPct != nil && snap.RateLimitPct >= g.cfg.RateLimitHeadroomPct {
		return BackpressureReasonRateLimit
	}
	if snap.DailyCount >= g.cfg.DailyCapHard {
		return BackpressureReasonDailyCapHard
	}
	return ""
}

// enterPausedLocked transitions to (or stays in) paused state, firing
// OnPause on either OK→paused or paused-with-different-reason
// transitions. Caller must hold g.mu.
func (g *GHArchiveBackpressureGate) enterPausedLocked(now time.Time, reason string, snap BackpressureSnapshot) {
	if g.paused && g.pauseReason == reason {
		return
	}
	if !g.paused {
		g.pausedSince = now
	}
	g.paused = true
	g.pauseReason = reason
	if g.hooks.OnPause != nil {
		g.hooks.OnPause(reason, snap.QueueDepth, snap.RateLimitPct, snap.DailyCount)
	}
}

// exitPausedLocked transitions out of paused state, firing OnResume
// with the cumulative pause duration. No-op when not paused. Caller
// must hold g.mu.
func (g *GHArchiveBackpressureGate) exitPausedLocked(now time.Time) {
	if !g.paused {
		return
	}
	dur := now.Sub(g.pausedSince)
	reason := g.pauseReason
	g.paused = false
	g.pauseReason = ""
	g.pausedSince = time.Time{}
	if g.hooks.OnResume != nil {
		g.hooks.OnResume(reason, dur)
	}
}

// transitionDailyStateLocked fires OnDailyCapState only when the cap
// state changes. Caller must hold g.mu.
func (g *GHArchiveBackpressureGate) transitionDailyStateLocked(count int) {
	state := DailyCapStateOK
	switch {
	case count >= g.cfg.DailyCapHard:
		state = DailyCapStateHard
	case count >= g.cfg.DailyCapWarn:
		state = DailyCapStateWarn
	}
	if state == g.lastDailyState {
		return
	}
	g.lastDailyState = state
	if g.hooks.OnDailyCapState != nil {
		g.hooks.OnDailyCapState(state, count)
	}
}

func (g *GHArchiveBackpressureGate) queueDepth() int {
	if g.signals.ClassifierQueueDepth == nil {
		return 0
	}
	return g.signals.ClassifierQueueDepth()
}

func (g *GHArchiveBackpressureGate) rateLimitPct() float64 {
	if g.signals.RateLimitConsumptionPct == nil {
		return 0
	}
	return g.signals.RateLimitConsumptionPct()
}

// dailyCapCounter is a 24h sliding-window emission counter, bucketed by
// UTC hour. Total is the sum of buckets whose hour is within the last
// 24h of the supplied "now"; older buckets age out naturally without an
// explicit prune cycle. Bounded memory: at most 25 entries (24h plus
// the current partial hour) before prune.
type dailyCapCounter struct {
	entries map[time.Time]int
}

// inc records n events at the bucket containing now. Prunes older
// buckets opportunistically.
func (d *dailyCapCounter) inc(now time.Time, n int) {
	if d.entries == nil {
		d.entries = make(map[time.Time]int, 24)
	}
	hour := now.Truncate(time.Hour).UTC()
	d.entries[hour] += n
	d.pruneOlderThan(now)
}

// total sums all bucket counts within 24h of now. Idempotent prune.
func (d *dailyCapCounter) total(now time.Time) int {
	if d.entries == nil {
		return 0
	}
	d.pruneOlderThan(now)
	sum := 0
	for _, c := range d.entries {
		sum += c
	}
	return sum
}

// pruneOlderThan drops bucket entries whose hour is more than 24h
// before now. Cheap: at most a handful of map iterations per call
// because the bucket count is bounded by the window.
func (d *dailyCapCounter) pruneOlderThan(now time.Time) {
	cutoff := now.Add(-24 * time.Hour)
	for h := range d.entries {
		if h.Before(cutoff) {
			delete(d.entries, h)
		}
	}
}
