package discovery

import (
	"context"
	"sync"
	"testing"
	"time"
)

// gharchive_backpressure_test.go covers Story 4 ([ISI-954]) AC:
//
//   - Pause when classifier queue depth crosses threshold
//   - Pause when GitHub rate-limit consumption crosses headroom %
//   - Pause when rolling 24h emission cap (hard) trips, both at cycle
//     entry (CheckEntry) and mid-cycle (RecordEmission)
//   - Warn-state metric tick for daily_cap_warn without pausing
//   - Resume after each gate clears, with pause-duration metric
//   - 24h sliding window (no calendar-day reset)
//   - End-to-end with DiscoverFromGHArchive

// fakeClock is a minimal pinned clock for deterministic backpressure
// math. The tests poke the clock forward in well-known steps so the
// daily-cap window slide is observable without sleeping.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{now: t} }
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// recordedHooks captures hook callbacks so tests assert on the exact
// sequence of fire-once events.
type recordedHooks struct {
	mu             sync.Mutex
	pauses         []pauseEvent
	resumes        []resumeEvent
	dailyStates    []dailyStateEvent
	emissionCounts []int
}

type pauseEvent struct {
	Reason       string
	QueueDepth   int
	RateLimitPct float64
	DailyCount   int
}
type resumeEvent struct {
	Reason   string
	Duration time.Duration
}
type dailyStateEvent struct {
	State string
	Count int
}

func (r *recordedHooks) hooks() GHArchiveBackpressureHooks {
	return GHArchiveBackpressureHooks{
		OnPause: func(reason string, qd int, rl float64, dc int) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.pauses = append(r.pauses, pauseEvent{reason, qd, rl, dc})
		},
		OnResume: func(reason string, dur time.Duration) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.resumes = append(r.resumes, resumeEvent{reason, dur})
		},
		OnDailyCapState: func(state string, count int) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.dailyStates = append(r.dailyStates, dailyStateEvent{state, count})
		},
		OnEmissionCounted: func(count int) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.emissionCounts = append(r.emissionCounts, count)
		},
	}
}

func (r *recordedHooks) snapshot() (pauses []pauseEvent, resumes []resumeEvent, daily []dailyStateEvent, emissions []int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pauses = append(pauses, r.pauses...)
	resumes = append(resumes, r.resumes...)
	daily = append(daily, r.dailyStates...)
	emissions = append(emissions, r.emissionCounts...)
	return
}

// newGate is a tiny constructor that wires the fake clock and recorder.
func newGate(t *testing.T, cfg GHArchiveBackpressureConfig, signals GHArchiveBackpressureSignals, clk *fakeClock, rec *recordedHooks) *GHArchiveBackpressureGate {
	t.Helper()
	g := NewGHArchiveBackpressureGate(cfg, signals, rec.hooks())
	g.SetClock(clk.Now)
	return g
}

func TestBackpressureGate_DefaultsAppliedWhenZero(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	rec := &recordedHooks{}
	g := newGate(t, GHArchiveBackpressureConfig{}, GHArchiveBackpressureSignals{}, clk, rec)

	allow, reason, snap := g.CheckEntry()
	if !allow {
		t.Fatalf("expected allow=true with all-default config and no signals, got reason=%q snap=%+v", reason, snap)
	}
	if got, want := g.cfg.ClassifierQueueDepthThreshold, DefaultGHArchiveBackpressureClassifierQueueDepth; got != want {
		t.Errorf("queue threshold default not applied: got %d want %d", got, want)
	}
	if got, want := g.cfg.RateLimitHeadroomPct, DefaultGHArchiveBackpressureRateLimitHeadroomPct; got != want {
		t.Errorf("rate-limit headroom default not applied: got %v want %v", got, want)
	}
	if got, want := g.cfg.DailyCapWarn, DefaultGHArchiveBackpressureDailyCapWarn; got != want {
		t.Errorf("daily-cap-warn default not applied: got %d want %d", got, want)
	}
	if got, want := g.cfg.DailyCapHard, DefaultGHArchiveBackpressureDailyCapHard; got != want {
		t.Errorf("daily-cap-hard default not applied: got %d want %d", got, want)
	}
}

// TestBackpressureGate_QueueDepthPauseAndResume covers AC: queue gate
// trips at threshold, fires OnPause once, and resumes when queue
// drains, firing OnResume with cumulative duration.
func TestBackpressureGate_QueueDepthPauseAndResume(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	rec := &recordedHooks{}

	queueDepth := 0
	g := newGate(t, GHArchiveBackpressureConfig{ClassifierQueueDepthThreshold: 100},
		GHArchiveBackpressureSignals{
			ClassifierQueueDepth: func() int { return queueDepth },
		}, clk, rec)

	// Below threshold: allow, no pause/resume hooks.
	queueDepth = 99
	if allow, _, _ := g.CheckEntry(); !allow {
		t.Fatalf("expected allow=true at queue=99 (threshold=100)")
	}
	if pauses, resumes, _, _ := rec.snapshot(); len(pauses) != 0 || len(resumes) != 0 {
		t.Errorf("expected no hooks when below threshold, got pauses=%v resumes=%v", pauses, resumes)
	}

	// At threshold: pause; OnPause fires once.
	queueDepth = 100
	allow, reason, snap := g.CheckEntry()
	if allow {
		t.Fatalf("expected allow=false at queue=100 (threshold=100)")
	}
	if reason != BackpressureReasonClassifierQueue {
		t.Errorf("expected reason=%q, got %q", BackpressureReasonClassifierQueue, reason)
	}
	if snap.QueueDepth != 100 {
		t.Errorf("snap.QueueDepth = %d, want 100", snap.QueueDepth)
	}

	// Second check while still over threshold: must NOT fire OnPause again.
	clk.Advance(5 * time.Second)
	queueDepth = 250
	if allow, _, _ := g.CheckEntry(); allow {
		t.Fatalf("expected allow=false on repeat over-threshold check")
	}

	pauses, resumes, _, _ := rec.snapshot()
	if len(pauses) != 1 {
		t.Errorf("expected exactly 1 pause event (fire-once), got %d", len(pauses))
	}
	if len(resumes) != 0 {
		t.Errorf("expected 0 resume events while still paused, got %d", len(resumes))
	}

	// Drain below threshold: resume.
	clk.Advance(10 * time.Second)
	queueDepth = 50
	allow, _, _ = g.CheckEntry()
	if !allow {
		t.Fatalf("expected allow=true after drain")
	}

	pauses, resumes, _, _ = rec.snapshot()
	if len(resumes) != 1 {
		t.Fatalf("expected exactly 1 resume event after drain, got %d", len(resumes))
	}
	wantDur := 15 * time.Second
	if resumes[0].Duration != wantDur {
		t.Errorf("resume duration = %v, want %v", resumes[0].Duration, wantDur)
	}
	if resumes[0].Reason != BackpressureReasonClassifierQueue {
		t.Errorf("resume reason = %q, want %q", resumes[0].Reason, BackpressureReasonClassifierQueue)
	}

	// Second drained check: no extra resume hook.
	if allow, _, _ := g.CheckEntry(); !allow {
		t.Fatalf("expected allow=true on second drained check")
	}
	pauses, resumes, _, _ = rec.snapshot()
	if len(resumes) != 1 {
		t.Errorf("expected resume to remain at 1 after drained checks, got %d", len(resumes))
	}
	if len(pauses) != 1 {
		t.Errorf("expected pauses to stay at 1, got %d", len(pauses))
	}
}

// TestBackpressureGate_RateLimitGate covers AC: rate-limit consumption
// >= headroomPct triggers pause; below clears it.
func TestBackpressureGate_RateLimitGate(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	rec := &recordedHooks{}

	rlPct := 0.0
	g := newGate(t, GHArchiveBackpressureConfig{RateLimitHeadroomPct: 80},
		GHArchiveBackpressureSignals{
			RateLimitConsumptionPct: func() float64 { return rlPct },
		}, clk, rec)

	rlPct = 79.999
	if allow, _, _ := g.CheckEntry(); !allow {
		t.Fatalf("expected allow at 79.999 < 80 headroom")
	}

	rlPct = 80.0
	allow, reason, snap := g.CheckEntry()
	if allow {
		t.Fatalf("expected pause at 80.0 == 80 headroom")
	}
	if reason != BackpressureReasonRateLimit {
		t.Errorf("reason = %q, want %q", reason, BackpressureReasonRateLimit)
	}
	if snap.RateLimitPct != 80.0 {
		t.Errorf("snap.RateLimitPct = %v, want 80", snap.RateLimitPct)
	}

	rlPct = 50.0
	if allow, _, _ := g.CheckEntry(); !allow {
		t.Fatalf("expected resume at 50%% consumption")
	}

	pauses, resumes, _, _ := rec.snapshot()
	if len(pauses) != 1 || len(resumes) != 1 {
		t.Errorf("expected 1 pause/1 resume, got %d/%d", len(pauses), len(resumes))
	}
}

// TestBackpressureGate_DailyCapWarnDoesNotPause covers Q5 refinement:
// crossing the warn threshold fires OnDailyCapState("warn") but the gate
// keeps allowing emission until the hard cap is reached.
func TestBackpressureGate_DailyCapWarnDoesNotPause(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	rec := &recordedHooks{}
	g := newGate(t, GHArchiveBackpressureConfig{
		DailyCapWarn: 5,
		DailyCapHard: 10,
	}, GHArchiveBackpressureSignals{}, clk, rec)

	for i := 0; i < 5; i++ {
		allow, _ := g.RecordEmission()
		if !allow {
			t.Fatalf("expected allow at emission %d (warn=5, hard=10)", i+1)
		}
	}
	// At this point we've emitted 5; warn threshold should have triggered.
	_, _, daily, emissions := rec.snapshot()
	if got := len(emissions); got != 5 {
		t.Errorf("expected 5 emission counts, got %d", got)
	}
	if len(daily) == 0 || daily[len(daily)-1].State != DailyCapStateWarn {
		t.Errorf("expected last daily state = %q, got %v", DailyCapStateWarn, daily)
	}
	pauses, _, _, _ := rec.snapshot()
	if len(pauses) != 0 {
		t.Errorf("expected NO pause events at warn threshold, got %d", len(pauses))
	}
}

// TestBackpressureGate_DailyCapHardPausesEmission covers AC:
// hard-cap trip fires OnPause and RecordEmission returns allow=false.
func TestBackpressureGate_DailyCapHardPausesEmission(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	rec := &recordedHooks{}
	g := newGate(t, GHArchiveBackpressureConfig{
		DailyCapWarn: 2,
		DailyCapHard: 3,
	}, GHArchiveBackpressureSignals{}, clk, rec)

	allow, _ := g.RecordEmission() // 1
	if !allow {
		t.Fatalf("emission 1 should pass")
	}
	allow, _ = g.RecordEmission() // 2
	if !allow {
		t.Fatalf("emission 2 should pass")
	}
	allow, reason := g.RecordEmission() // 3 == hard cap
	if allow {
		t.Fatalf("emission 3 (hard cap) should be denied")
	}
	if reason != BackpressureReasonDailyCapHard {
		t.Errorf("reason = %q, want %q", reason, BackpressureReasonDailyCapHard)
	}

	pauses, _, _, _ := rec.snapshot()
	if len(pauses) != 1 {
		t.Fatalf("expected 1 pause, got %d", len(pauses))
	}
	if pauses[0].Reason != BackpressureReasonDailyCapHard {
		t.Errorf("pause reason = %q, want %q", pauses[0].Reason, BackpressureReasonDailyCapHard)
	}
	if pauses[0].DailyCount != 3 {
		t.Errorf("pause daily count = %d, want 3", pauses[0].DailyCount)
	}

	// Subsequent CheckEntry should also report paused (hard cap holds).
	if allow, reason, _ := g.CheckEntry(); allow || reason != BackpressureReasonDailyCapHard {
		t.Errorf("CheckEntry after hard cap: allow=%v reason=%q want allow=false reason=%q",
			allow, reason, BackpressureReasonDailyCapHard)
	}
}

// TestBackpressureGate_RollingWindowSlide covers Q5 resume semantics:
// the cap is a 24h rolling counter (not calendar-day) — emissions age
// out as the window slides, so a paused gate naturally resumes without
// a wall-clock reset.
func TestBackpressureGate_RollingWindowSlide(t *testing.T) {
	t0 := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	clk := newFakeClock(t0)
	rec := &recordedHooks{}
	g := newGate(t, GHArchiveBackpressureConfig{
		DailyCapWarn: 10,
		DailyCapHard: 20,
	}, GHArchiveBackpressureSignals{}, clk, rec)

	// Emit 20 candidates at hour 0 (t0). Hard cap trips on the 20th.
	for i := 0; i < 19; i++ {
		if allow, _ := g.RecordEmission(); !allow {
			t.Fatalf("expected allow at emission %d/20", i+1)
		}
	}
	if allow, reason := g.RecordEmission(); allow || reason != BackpressureReasonDailyCapHard {
		t.Fatalf("expected hard cap at emission 20, got allow=%v reason=%q", allow, reason)
	}

	// Advance 23h59m — still inside the 24h window; cap still trips.
	clk.Advance(23*time.Hour + 59*time.Minute)
	if got := g.DailyCount(); got != 20 {
		t.Errorf("DailyCount before slide = %d, want 20", got)
	}
	if allow, reason, _ := g.CheckEntry(); allow || reason != BackpressureReasonDailyCapHard {
		t.Errorf("at +23h59m: expected hard cap still tripped, got allow=%v reason=%q", allow, reason)
	}

	// Advance to +24h01m — now the t0 emissions are outside the window.
	// Counter drops to 0; gate resumes; OnResume fires.
	clk.Advance(2 * time.Minute)
	if got := g.DailyCount(); got != 0 {
		t.Errorf("DailyCount after slide = %d, want 0", got)
	}
	if allow, _, _ := g.CheckEntry(); !allow {
		t.Fatalf("expected allow=true after window slid past all emissions")
	}

	_, resumes, dailyStates, _ := rec.snapshot()
	if len(resumes) != 1 {
		t.Errorf("expected 1 resume after slide, got %d", len(resumes))
	}
	// Daily state should have transitioned ok→warn→hard→… on the slide,
	// at minimum eventually returning to ok.
	if dailyStates[len(dailyStates)-1].State != DailyCapStateOK {
		t.Errorf("last daily state after slide = %q, want %q",
			dailyStates[len(dailyStates)-1].State, DailyCapStateOK)
	}
}

// TestBackpressureGate_NilSignalDegradesToOff verifies that a nil
// signal func means "gate off" rather than "treat as zero pause".
// Critical: a misconfigured daemon that fails to wire the queue-depth
// signal must not starve discovery.
func TestBackpressureGate_NilSignalDegradesToOff(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	rec := &recordedHooks{}
	// Both signals nil — only the daily cap remains active. A 0-cap
	// world should always allow until emissions accumulate.
	g := newGate(t, GHArchiveBackpressureConfig{}, GHArchiveBackpressureSignals{}, clk, rec)

	allow, reason, _ := g.CheckEntry()
	if !allow {
		t.Fatalf("expected allow=true with nil signals, got reason=%q", reason)
	}
}

// TestBackpressureGate_MultipleGatesPickFirstReason covers stable
// reason ordering when multiple gates trip at once: queue > rate-limit
// > daily-cap-hard. A reason rotation while paused emits a fresh
// OnPause hook so dashboards see the change.
func TestBackpressureGate_MultipleGatesPickFirstReason(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	rec := &recordedHooks{}
	queueDepth := 0
	rlPct := 0.0
	g := newGate(t, GHArchiveBackpressureConfig{
		ClassifierQueueDepthThreshold: 100,
		RateLimitHeadroomPct:          80,
		DailyCapHard:                  10,
	}, GHArchiveBackpressureSignals{
		ClassifierQueueDepth:    func() int { return queueDepth },
		RateLimitConsumptionPct: func() float64 { return rlPct },
	}, clk, rec)

	// Trip rate-limit only.
	rlPct = 90
	if allow, reason, _ := g.CheckEntry(); allow || reason != BackpressureReasonRateLimit {
		t.Fatalf("rate-limit alone: allow=%v reason=%q", allow, reason)
	}

	// Now also trip queue-depth — reason rotates to queue (higher priority).
	queueDepth = 200
	if allow, reason, _ := g.CheckEntry(); allow || reason != BackpressureReasonClassifierQueue {
		t.Errorf("queue+rl together: allow=%v reason=%q want=%q",
			allow, reason, BackpressureReasonClassifierQueue)
	}

	// Hooks: 2 pause events (one per reason), each with the correct reason.
	pauses, _, _, _ := rec.snapshot()
	if len(pauses) != 2 {
		t.Fatalf("expected 2 pause events for reason rotation, got %d (%v)", len(pauses), pauses)
	}
	if pauses[0].Reason != BackpressureReasonRateLimit {
		t.Errorf("first pause reason = %q, want %q", pauses[0].Reason, BackpressureReasonRateLimit)
	}
	if pauses[1].Reason != BackpressureReasonClassifierQueue {
		t.Errorf("second pause reason = %q, want %q", pauses[1].Reason, BackpressureReasonClassifierQueue)
	}
}

// TestBackpressureGate_DailyCapStateLadder verifies OnDailyCapState
// fires only on transitions: ok→warn→hard→warn→ok. Repeated calls in
// the same state must not re-fire.
func TestBackpressureGate_DailyCapStateLadder(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC))
	rec := &recordedHooks{}
	g := newGate(t, GHArchiveBackpressureConfig{
		DailyCapWarn: 3,
		DailyCapHard: 6,
	}, GHArchiveBackpressureSignals{}, clk, rec)

	for i := 0; i < 6; i++ {
		_, _ = g.RecordEmission()
	}

	_, _, daily, _ := rec.snapshot()
	wantStates := []string{DailyCapStateWarn, DailyCapStateHard}
	if len(daily) != len(wantStates) {
		t.Fatalf("expected %d daily-state events, got %d (%v)", len(wantStates), len(daily), daily)
	}
	for i, e := range daily {
		if e.State != wantStates[i] {
			t.Errorf("daily[%d].State = %q, want %q", i, e.State, wantStates[i])
		}
	}

	// Slide the window to force ok again. Advance 25h, prune, re-check.
	clk.Advance(25 * time.Hour)
	if got := g.DailyCount(); got != 0 {
		t.Errorf("DailyCount after 25h slide = %d, want 0", got)
	}
	// Force a transition by calling CheckEntry, which evaluates daily state.
	g.CheckEntry()

	_, _, daily, _ = rec.snapshot()
	if daily[len(daily)-1].State != DailyCapStateOK {
		t.Errorf("after slide, last state = %q, want %q",
			daily[len(daily)-1].State, DailyCapStateOK)
	}
}

// TestDiscoverFromGHArchive_PausedAtEntry exercises the full discovery
// path with a tripped backpressure gate. Expectation: TopActiveRepos is
// not consulted (no candidates returned), no REST hydration happens,
// Result is empty but non-error.
func TestDiscoverFromGHArchive_PausedAtEntry(t *testing.T) {
	// Seed the gharchive aggregate with one repo so we'd otherwise
	// promote it. The gate must short-circuit before TopActiveRepos.
	src := gharchiveTestSource(t, map[string]int{"alpha/repo": 50})

	// REST server returns 404 for everything: if hydration ran, we'd
	// see it as a "hydration failed" log line and 0 emissions; our
	// expectation is the gate cuts in earlier so REST is never called.
	rest := fakeRESTServer(t, map[string]repoMetricsResponse{})
	defer rest.Close()

	cfg := Config{
		Sources: SourcesConfig{
			GHArchive: GHArchiveSourceConfig{
				Enabled:       true,
				TopN:          10,
				ActivityFloor: 1,
			},
		},
	}
	d := newPipelineDiscoverer(t, rest.URL, cfg)
	d.SetGHArchiveSource(src)

	// Gate trips on classifier queue depth.
	clk := newFakeClock(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	rec := &recordedHooks{}
	queueDepth := 9999
	g := newGate(t, GHArchiveBackpressureConfig{ClassifierQueueDepthThreshold: 100},
		GHArchiveBackpressureSignals{
			ClassifierQueueDepth: func() int { return queueDepth },
		}, clk, rec)
	d.SetGHArchiveBackpressure(g)

	res, err := d.DiscoverFromGHArchive(context.Background())
	if err != nil {
		t.Fatalf("DiscoverFromGHArchive: %v", err)
	}
	if res == nil {
		t.Fatalf("expected non-nil result even when paused")
	}
	if res.TotalFound != 0 || res.NewRepos != 0 || len(res.Repos) != 0 {
		t.Errorf("paused result should be empty: TotalFound=%d NewRepos=%d Repos=%d",
			res.TotalFound, res.NewRepos, len(res.Repos))
	}
	pauses, _, _, _ := rec.snapshot()
	if len(pauses) != 1 || pauses[0].Reason != BackpressureReasonClassifierQueue {
		t.Errorf("expected single pause for queue-depth, got %v", pauses)
	}
}

// TestDiscoverFromGHArchive_HardCapBreaksMidCycle exercises the case
// where the cycle starts in OK state and the daily cap trips mid-loop.
// AC: emission stops at the cap; metrics fire; remaining candidates
// are not REST-hydrated.
func TestDiscoverFromGHArchive_HardCapBreaksMidCycle(t *testing.T) {
	repos := map[string]int{
		"alpha/one": 50, "alpha/two": 40, "alpha/three": 30,
		"alpha/four": 20, "alpha/five": 10,
	}
	src := gharchiveTestSource(t, repos)

	restRepos := map[string]repoMetricsResponse{
		"alpha/one":   repoEntry("alpha/one", 100),
		"alpha/two":   repoEntry("alpha/two", 100),
		"alpha/three": repoEntry("alpha/three", 100),
		"alpha/four":  repoEntry("alpha/four", 100),
		"alpha/five":  repoEntry("alpha/five", 100),
	}
	rest := fakeRESTServer(t, restRepos)
	defer rest.Close()

	cfg := Config{
		Sources: SourcesConfig{
			GHArchive: GHArchiveSourceConfig{
				Enabled:       true,
				TopN:          10,
				ActivityFloor: 1,
			},
		},
	}
	d := newPipelineDiscoverer(t, rest.URL, cfg)
	d.SetGHArchiveSource(src)

	clk := newFakeClock(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	rec := &recordedHooks{}
	g := newGate(t, GHArchiveBackpressureConfig{
		DailyCapWarn: 1,
		DailyCapHard: 2, // hard cap after 2 emissions
	}, GHArchiveBackpressureSignals{}, clk, rec)
	d.SetGHArchiveBackpressure(g)

	res, err := d.DiscoverFromGHArchive(context.Background())
	if err != nil {
		t.Fatalf("DiscoverFromGHArchive: %v", err)
	}
	if res.NewRepos != 2 {
		t.Errorf("NewRepos = %d, want 2 (hard cap at 2)", res.NewRepos)
	}
	if len(res.Repos) != 2 {
		t.Errorf("len(Repos) = %d, want 2 (loop broke at hard cap)", len(res.Repos))
	}

	pauses, _, dailyStates, emissions := rec.snapshot()
	if len(emissions) != 2 {
		t.Errorf("expected 2 emission counts, got %d", len(emissions))
	}
	if len(pauses) != 1 || pauses[0].Reason != BackpressureReasonDailyCapHard {
		t.Errorf("expected 1 hard-cap pause, got %v", pauses)
	}
	// Daily-cap state should have walked ok→warn→hard.
	wantStates := []string{DailyCapStateWarn, DailyCapStateHard}
	if len(dailyStates) != len(wantStates) {
		t.Fatalf("expected daily-state events %v, got %v", wantStates, dailyStates)
	}
	for i, e := range dailyStates {
		if e.State != wantStates[i] {
			t.Errorf("dailyStates[%d].State = %q, want %q", i, e.State, wantStates[i])
		}
	}
}

// TestDiscoverFromGHArchive_NoBackpressureFallback sanity: when no
// backpressure gate is wired, behaviour matches Story 2 (all
// candidates flow through, no pause/resume metrics fire). Guards
// against regressions on the nil-gate fast path.
func TestDiscoverFromGHArchive_NoBackpressureFallback(t *testing.T) {
	src := gharchiveTestSource(t, map[string]int{"alpha/repo": 50})
	rest := fakeRESTServer(t, map[string]repoMetricsResponse{
		"alpha/repo": repoEntry("alpha/repo", 100),
	})
	defer rest.Close()

	cfg := Config{
		Sources: SourcesConfig{
			GHArchive: GHArchiveSourceConfig{
				Enabled:       true,
				TopN:          10,
				ActivityFloor: 1,
			},
		},
	}
	d := newPipelineDiscoverer(t, rest.URL, cfg)
	d.SetGHArchiveSource(src)

	res, err := d.DiscoverFromGHArchive(context.Background())
	if err != nil {
		t.Fatalf("DiscoverFromGHArchive: %v", err)
	}
	if res.NewRepos != 1 {
		t.Errorf("NewRepos = %d, want 1 (no gate)", res.NewRepos)
	}
}

// TestDailyCapCounter_ConcurrentInc exercises the gate under concurrent
// emission counting. Bounded contention is fine; correctness must hold.
func TestDailyCapCounter_ConcurrentInc(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	rec := &recordedHooks{}
	g := newGate(t, GHArchiveBackpressureConfig{
		DailyCapWarn: 100_000,
		DailyCapHard: 100_001,
	}, GHArchiveBackpressureSignals{}, clk, rec)

	const n, workers = 1000, 20
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < n; j++ {
				_, _ = g.RecordEmission()
			}
		}()
	}
	wg.Wait()

	if got, want := g.DailyCount(), n*workers; got != want {
		t.Errorf("DailyCount after concurrent inc = %d, want %d", got, want)
	}
}
