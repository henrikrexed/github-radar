//go:build loadtest

// Package daemon — T5 / ISI-716 load-test harness.
//
// This file is gated behind `-tags=loadtest` so it does not run as part of
// the normal `go test ./...` suite. Invoke with:
//
//	go test -tags=loadtest -race -count=1 -run TestTieredRefresh_Budget \
//	   ./internal/daemon/...
//
// The harness exercises L0–L8 from the [load-test plan]
// (see /ISI/issues/ISI-716#document-load-test-plan). Each subtest builds
// a [ghstub] HTTP server, wires a real github.Client + Scanner +
// state.Store + ManualReader-backed metrics exporter, runs a simulated
// 24h trace at a compressed wallclock, scrapes the metric counters, and
// asserts on the per-row gates from the plan.
//
// L9 is out of scope here — it is the live 559-repo shadow run handed
// off to the Product Manager after Testing Architect signs off the in-
// process L0–L8 results.
package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/hrexed/github-radar/internal/github"
	"github.com/hrexed/github-radar/internal/metrics"
	"github.com/hrexed/github-radar/internal/state"
	"github.com/hrexed/github-radar/internal/testutil/ghstub"
)

// ----------------------------------------------------------------------
// Fixture
// ----------------------------------------------------------------------

type fixtureRow struct {
	FullName    string  `json:"full_name"`
	GrowthScore float64 `json:"growth_score"`
	FirstSeenAt string  `json:"first_seen_at"`
}

func loadFixture(t *testing.T, name string) []fixtureRow {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile))) // -> repo root
	path := filepath.Join(root, "testdata", "load", name)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("loadFixture %s: %v", path, err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*64), 1024*1024)
	var out []fixtureRow
	for scanner.Scan() {
		var row fixtureRow
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatalf("decode row: %v\nline=%q", err, scanner.Text())
		}
		out = append(out, row)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return out
}

// ----------------------------------------------------------------------
// Simulated clock
// ----------------------------------------------------------------------

type simClock struct {
	mu  sync.Mutex
	now time.Time
}

func newSimClock(start time.Time) *simClock { return &simClock{now: start} }

func (c *simClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *simClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// ----------------------------------------------------------------------
// Harness
// ----------------------------------------------------------------------

type harness struct {
	t        *testing.T
	stub     *ghstub.Stub
	clock    *simClock
	client   *github.Client
	scanner  *github.Scanner
	store    *state.Store
	exporter *metrics.Exporter
	reader   *sdkmetric.ManualReader
	cfg      github.TierConfig

	repos       []github.Repo
	candidates  []github.TierCandidate
	candidateMu sync.Mutex
}

// newHarness wires every dependency of the load-test scenario without
// going through daemon.New (which pulls in classification, discovery,
// the database, and the HTTP server we don't need here).
//
// The metric exporter uses a ManualReader so the test can scrape the
// counter values synchronously. The github.Client is pointed at the
// stub server.
func newHarness(t *testing.T, stubCfg ghstub.Config, fixture []fixtureRow, tierCfg github.TierConfig) *harness {
	t.Helper()

	// Anchor at the same instant the fixture generator uses
	// (testdata/load/gen_fixture.go) so first_seen_at offsets line up
	// with the simulated clock; otherwise repos with future timestamps
	// get spuriously promoted to TierNew via the override.
	clock := newSimClock(time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC))
	stubCfg.Now = clock.Now
	stub := ghstub.New(stubCfg)
	t.Cleanup(stub.Close)

	// Wire metrics exporter on a ManualReader so the harness can scrape
	// counters in-process without spinning up an OTLP receiver.
	reader := sdkmetric.NewManualReader()
	exp, err := metrics.NewExporterForTest(reader, "loadtest")
	if err != nil {
		t.Fatalf("NewExporterForTest: %v", err)
	}
	t.Cleanup(func() { _ = exp.ShutdownWithTimeout() })

	client, err := github.NewClient("test-token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client.SetBaseURL(stub.URL())
	// Install the same observer the production daemon uses so resource/
	// result tagging matches what the gates assume.
	client.SetAPIObserver(newAPIObserver(context.Background(), exp))

	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	scanner := github.NewScanner(client, store)

	repos := make([]github.Repo, 0, len(fixture))
	candidates := make([]github.TierCandidate, 0, len(fixture))
	for _, row := range fixture {
		owner, name := splitFullName(row.FullName)
		repos = append(repos, github.Repo{Owner: owner, Name: name})

		var firstSeen time.Time
		if row.FirstSeenAt != "" {
			if ts, err := time.Parse(time.RFC3339, row.FirstSeenAt); err == nil {
				firstSeen = ts
			}
		}
		candidates = append(candidates, github.TierCandidate{
			FullName:    row.FullName,
			GrowthScore: row.GrowthScore,
			FirstSeenAt: firstSeen,
		})
	}

	return &harness{
		t:          t,
		stub:       stub,
		clock:      clock,
		client:     client,
		scanner:    scanner,
		store:      store,
		exporter:   exp,
		reader:     reader,
		cfg:        tierCfg,
		repos:      repos,
		candidates: candidates,
	}
}

// preWarmLastCollected staggers LastCollectedAt across the past
// `cfg.ColdInterval` so the steady-state cadence is in effect at the
// start of the measurement window. Without this, all 3,000 repos are
// due on tick 0 (cold-start) and the first hour swamps the per-hour
// budget.
func (h *harness) preWarmLastCollected() {
	r := rand.New(rand.NewSource(20260425))
	now := h.clock.Now()
	h.candidateMu.Lock()
	defer h.candidateMu.Unlock()
	for i := range h.candidates {
		// Tier the rank-based way so each repo's offset stays inside
		// its own cadence — so a hot repo's offset is in [0, HotInterval),
		// warm in [0, WarmInterval), etc.
		tier := tierForCandidate(h.candidates, i, h.cfg, now)
		var window time.Duration
		switch tier {
		case github.TierHot, github.TierNew:
			window = h.cfg.HotInterval
		case github.TierWarm:
			window = h.cfg.WarmInterval
		default:
			window = h.cfg.ColdInterval
		}
		offset := time.Duration(r.Int63n(int64(window)))
		h.candidates[i].LastCollectedAt = now.Add(-offset)
	}
}

// tierForCandidate ranks a single candidate among the slice using the
// same logic as github.ClassifyAll, returning its tier. Used by the
// pre-warm helper.
func tierForCandidate(all []github.TierCandidate, idx int, cfg github.TierConfig, now time.Time) github.RefreshTier {
	// Build a sortable rank slice once per call. Cheap at 3k.
	type ranked struct {
		i int
		s float64
		n string
	}
	rs := make([]ranked, len(all))
	for i, c := range all {
		rs[i] = ranked{i: i, s: c.GrowthScore, n: c.FullName}
	}
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].s != rs[j].s {
			return rs[i].s > rs[j].s
		}
		return rs[i].n < rs[j].n
	})
	for rank, r := range rs {
		if r.i != idx {
			continue
		}
		// New-repo override.
		c := all[idx]
		if !c.FirstSeenAt.IsZero() && now.Sub(c.FirstSeenAt) < cfg.NewRepoWindow {
			return github.TierNew
		}
		switch {
		case rank < cfg.HotN:
			return github.TierHot
		case rank < cfg.HotN+cfg.WarmN:
			return github.TierWarm
		default:
			return github.TierCold
		}
	}
	return github.TierCold
}

// tick runs one classification + dispatch pass at the current clock.
// The bulk batch covers hot/warm/new tiers; the cold batch goes through
// the conditional-GET REST path.
//
// The scanner stamps state.LastCollected with real wallclock time on
// successful fetches; the harness ignores that and instead overlays the
// simulated clock on the *due* repos for this tick so the tier
// classifier uses the same time-base on subsequent ticks.
func (h *harness) tick(ctx context.Context) {
	h.candidateMu.Lock()
	cands := make([]github.TierCandidate, len(h.candidates))
	copy(cands, h.candidates)
	h.candidateMu.Unlock()

	now := h.clock.Now()
	assignments := github.ClassifyAll(cands, now, h.cfg)
	due := github.DueRepos(assignments)

	requested := make(map[string]github.Repo, len(h.repos))
	indexByName := make(map[string]int, len(h.candidates))
	for _, r := range h.repos {
		requested[r.Owner+"/"+r.Name] = r
	}
	for i, c := range h.candidates {
		indexByName[c.FullName] = i
	}

	var bulk, cold []github.Repo
	dueIdx := make([]int, 0, len(due))
	for _, a := range due {
		r, ok := requested[a.FullName]
		if !ok {
			continue
		}
		if a.Tier == github.TierCold {
			cold = append(cold, r)
		} else {
			bulk = append(bulk, r)
		}
		if idx, ok := indexByName[a.FullName]; ok {
			dueIdx = append(dueIdx, idx)
		}
	}

	if len(cold) > 0 {
		_, _ = h.scanner.Scan(ctx, cold)
	}
	if len(bulk) > 0 {
		_, _ = h.scanner.ScanBulk(ctx, bulk)
	}

	// Stamp simulated LastCollectedAt only for repos the harness actually
	// dispatched this tick. This preserves the tier-cadence logic across
	// ticks (a repo that wasn't due stays not-due).
	h.candidateMu.Lock()
	for _, idx := range dueIdx {
		h.candidates[idx].LastCollectedAt = now
	}
	h.candidateMu.Unlock()
}

// runFor advances the simulated clock by `dur`, ticking every
// `tickInterval`. Each tick performs one classify+dispatch pass.
func (h *harness) runFor(ctx context.Context, dur, tickInterval time.Duration) {
	end := h.clock.Now().Add(dur)
	for h.clock.Now().Before(end) {
		h.tick(ctx)
		h.clock.Advance(tickInterval)
		if ctx.Err() != nil {
			return
		}
	}
}

// callCounts scrapes the in-process ManualReader and returns the
// summed `github.api.calls_total` counters keyed by (resource, result).
func (h *harness) callCounts() map[string]int64 {
	rm := metricdata.ResourceMetrics{}
	if err := h.reader.Collect(context.Background(), &rm); err != nil {
		h.t.Fatalf("reader.Collect: %v", err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "github.api.calls_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				resource, result := tagPair(dp.Attributes)
				key := resource + "|" + result
				out[key] += dp.Value
			}
		}
	}
	return out
}

func tagPair(set attribute.Set) (string, string) {
	var resource, result string
	iter := set.Iter()
	for iter.Next() {
		kv := iter.Attribute()
		switch kv.Key {
		case "resource":
			resource = kv.Value.AsString()
		case "result":
			result = kv.Value.AsString()
		}
	}
	return resource, result
}

// totalCalls returns the sum across every (resource, result) cell.
func (h *harness) totalCalls(counts map[string]int64) int64 {
	var sum int64
	for _, v := range counts {
		sum += v
	}
	return sum
}

// callsByResource returns the sum across all results for the given resource.
func (h *harness) callsByResource(counts map[string]int64, resource string) int64 {
	var sum int64
	for k, v := range counts {
		// keys are "resource|result"
		if len(k) < len(resource)+1 || k[len(resource)] != '|' || k[:len(resource)] != resource {
			continue
		}
		sum += v
	}
	return sum
}

func splitFullName(fn string) (owner, name string) {
	for i := 0; i < len(fn); i++ {
		if fn[i] == '/' {
			return fn[:i], fn[i+1:]
		}
	}
	return fn, ""
}

// ----------------------------------------------------------------------
// Top-level orchestrator — TestTieredRefresh_Budget
// ----------------------------------------------------------------------

func TestTieredRefresh_Budget(t *testing.T) {
	fixture := loadFixture(t, "3k_repos.jsonl")
	if len(fixture) != 3000 {
		t.Fatalf("fixture rows = %d, want 3000", len(fixture))
	}

	t.Run("L0_ColdStart_NoRateLimitHeadersYet", func(t *testing.T) {
		caseL0(t, fixture)
	})
	t.Run("L1_NominalSteadyState", func(t *testing.T) {
		caseL1(t, fixture)
	})
	t.Run("L1.5_ActivityOnlyIsolation", func(t *testing.T) {
		caseL1_5(t, fixture)
	})
	t.Run("L2_ColdStart_NoEtagsPersisted", func(t *testing.T) {
		caseL2(t, fixture)
	})
	t.Run("L3_ETagMismatchPersistence", func(t *testing.T) {
		caseL3(t, fixture)
	})
	t.Run("L4_DiscoveryBurst", func(t *testing.T) {
		t.Skip("L4 deferred — orthogonal to L1/L2 budget verification; needs a discovery-trigger inject point in the harness")
	})
	t.Run("L5a_PrimaryRateLimit", func(t *testing.T) {
		caseL5a(t, fixture)
	})
	t.Run("L5b_SecondaryRateLimit_RetryAfter", func(t *testing.T) {
		caseL5b(t, fixture)
	})
	t.Run("L6_GraphQLPartialNotFound", func(t *testing.T) {
		caseL6(t, fixture)
	})
	t.Run("L7_MidRunTierPromotion", func(t *testing.T) {
		caseL7(t, fixture)
	})
	t.Run("L8_GraphQLTransient5xx", func(t *testing.T) {
		caseL8(t, fixture)
	})
}

// ----------------------------------------------------------------------
// Per-scenario implementations
// ----------------------------------------------------------------------

// caseL0 — single tick before any rate-limit observation. The
// `apiRateUsedRatioGauge` defensive default in metrics.RecordRateLimit
// must NOT report 1.0 when Limit==0.
func caseL0(t *testing.T, fixture []fixtureRow) {
	tierCfg := github.DefaultTierConfig()
	h := newHarness(t, ghstub.Config{}, fixture, tierCfg)

	// Manually emit a zero-Limit snapshot; this exercises the same
	// branch the github.Client takes before its first response.
	h.exporter.RecordRateLimit(context.Background(), metrics.RateLimitSnapshot{})

	// Scrape: used_ratio gauge must NOT be present (no observation when Limit==0).
	rm := metricdata.ResourceMetrics{}
	if err := h.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "github.api.rate_limit.used_ratio" {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[float64])
			if !ok {
				continue
			}
			for _, dp := range gauge.DataPoints {
				if dp.Value >= 0.99 {
					t.Errorf("used_ratio = %v at cold start with Limit=0; defensive default broken", dp.Value)
				}
			}
		}
	}
}

// caseL1 — nominal steady state (hot + warm + new + cold) over a
// simulated 1h window. Per the plan the steady-state model is ~1,050
// calls/hr; the gate is ≤ 1,500/hr.
func caseL1(t *testing.T, fixture []fixtureRow) {
	tierCfg := github.DefaultTierConfig()
	h := newHarness(t, ghstub.Config{}, fixture, tierCfg)
	h.preWarmLastCollected()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1h sim, tick every 10 simulated minutes (6 ticks).
	h.runFor(ctx, time.Hour, 10*time.Minute)

	counts := h.callCounts()
	total := h.totalCalls(counts)
	t.Logf("L1 totals: total=%d  by_resource=%v", total, counts)

	if total > 1500 {
		t.Errorf("L1 calls/hr = %d > gate 1500", total)
	}
}

// caseL1_5 — activity-tier-only isolation. The architect's N1 note
// requires resource="activity" be tagged independently so a metadata
// regression cannot hide behind activity noise. Gate: ≤ 1,200/hr.
func caseL1_5(t *testing.T, fixture []fixtureRow) {
	tierCfg := github.DefaultTierConfig()
	h := newHarness(t, ghstub.Config{}, fixture, tierCfg)
	h.preWarmLastCollected()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	h.runFor(ctx, time.Hour, 10*time.Minute)

	counts := h.callCounts()
	activity := h.callsByResource(counts, "activity")
	t.Logf("L1.5 activity calls/hr = %d (gate 1200)  full=%v", activity, counts)

	if activity > 1200 {
		t.Errorf("L1.5 activity calls/hr = %d > gate 1200", activity)
	}
}

// caseL2 — steady state with no ETag rows persisted. Per the plan
// table, "0% conditional-GET hit ratio" means every cold-tier request
// returns 200 because state.RepoState.ETag is empty so the client
// cannot send If-None-Match. The harness reaches this by pre-warming
// LastCollected (so we're in steady state) but starting from a fresh
// store with zero ETag rows. Gate: combined ≤ 3,000/hr.
func caseL2(t *testing.T, fixture []fixtureRow) {
	tierCfg := github.DefaultTierConfig()
	h := newHarness(t, ghstub.Config{}, fixture, tierCfg)
	h.preWarmLastCollected()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	h.runFor(ctx, time.Hour, 10*time.Minute)

	counts := h.callCounts()
	total := h.totalCalls(counts)
	notMod := counts["repo|not_modified"]
	t.Logf("L2 totals: total=%d  not_modified=%d  by_resource=%v", total, notMod, counts)

	if total > 3000 {
		t.Errorf("L2 calls/hr = %d > gate 3000", total)
	}
	// "0% conditional-GET hit ratio" — assert no 304s served. The store
	// starts fresh and we don't run a cycle 1 to populate ETags, so the
	// REST path cannot send If-None-Match.
	if notMod != 0 {
		t.Errorf("L2 expected 0 not_modified responses, got %d (state shouldn't have ETag rows)", notMod)
	}
}

// caseL3 — ETag-mismatch persistence (write-back). Cycle 1 every cold-
// tier request returns 200 because no ETag is stored; the conditional-
// GET write-back path persists the server's ETag into state.RepoState.
// Cycle 2 sends If-None-Match with the persisted ETag and the stub
// returns 304.
//
// To keep the run fast the harness restricts the fixture so every repo
// lands in TierCold (intervals shrunk so each cycle finishes inside the
// runFor window). Hot/warm/new tiers go through GraphQL bulk fetch
// which is not part of the conditional-GET path; L1 covers that path.
func caseL3(t *testing.T, fixture []fixtureRow) {
	tierCfg := github.DefaultTierConfig()
	tierCfg.HotN = 0          // force every repo to TierCold
	tierCfg.WarmN = 0         // ditto
	tierCfg.NewRepoWindow = 0 // disable new-repo override (would route via GraphQL bulk)
	tierCfg.ColdInterval = time.Minute
	smallFixture := fixture[:100]

	h := newHarness(t, ghstub.Config{}, smallFixture, tierCfg)
	// All cold, all due immediately on tick 1.

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Cycle 1: every cold-tier request returns 200 + persists ETag.
	h.tick(ctx)
	c1 := h.callCounts()
	c1Total := h.totalCalls(c1)
	c1NotMod := c1["repo|not_modified"]
	t.Logf("L3 cycle1 total=%d not_modified=%d  full=%v", c1Total, c1NotMod, c1)

	if c1NotMod != 0 {
		t.Fatalf("L3 cycle1 not_modified=%d, want 0 (no ETags persisted yet)", c1NotMod)
	}
	if got := c1["repo|ok"]; got != int64(len(smallFixture)) {
		t.Fatalf("L3 cycle1 repo|ok=%d, want %d", got, len(smallFixture))
	}

	// Force every repo due again for cycle 2.
	h.candidateMu.Lock()
	for i := range h.candidates {
		h.candidates[i].LastCollectedAt = time.Time{}
	}
	h.candidateMu.Unlock()
	// Advance simulated clock past the cold interval so fresh due-time
	// kicks in even for repos with non-zero LastCollectedAt mirrored from
	// the store's internal updates.
	h.clock.Advance(2 * tierCfg.ColdInterval)

	// Cycle 2: ETags now persisted; stub returns 304 on If-None-Match.
	h.tick(ctx)
	c2 := h.callCounts()
	c2NotMod := c2["repo|not_modified"] - c1NotMod
	c2Ok := c2["repo|ok"] - c1["repo|ok"]
	t.Logf("L3 cycle2 delta: not_modified=%d  repo|ok=%d  full=%v", c2NotMod, c2Ok, c2)

	// Plan acceptance: cycle 2 issues If-None-Match and gets 304 for
	// every repo whose ETag persisted from cycle 1.
	if c2NotMod < int64(len(smallFixture)) {
		t.Errorf("L3 cycle2 not_modified=%d, want %d (write-back path broken)",
			c2NotMod, len(smallFixture))
	}
	if c2Ok != 0 {
		t.Errorf("L3 cycle2 fresh repo|ok=%d, want 0 (every cold repo should 304)", c2Ok)
	}
}

// caseL5a — primary rate-limit injection. After the injector fires the
// `github.api.calls_total{result="rate_limited"}` cell should be ≥ 1
// and the harness should not crash.
//
// The harness pre-advances the simulated clock past the trigger before
// the first tick so the very first request fires the injector — this
// avoids the "no repos due in tick 2 because tick 1 just collected
// them all" problem.
func caseL5a(t *testing.T, fixture []fixtureRow) {
	tierCfg := github.DefaultTierConfig()
	tierCfg.HotN = 50
	tierCfg.WarmN = 100
	smallFixture := fixture[:200]
	h := newHarness(t, ghstub.Config{
		PrimaryRateLimitInjectAt: 1 * time.Minute,
	}, smallFixture, tierCfg)
	// Advance past the trigger so the very first request is injection-eligible.
	h.clock.Advance(2 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// One tick — every repo is due at IsZero LastCollectedAt, so a fan
	// of REST + GraphQL requests fires; the very first one trips the
	// 403 injector.
	h.tick(ctx)

	counts := h.callCounts()
	t.Logf("L5a counts: %v  stub.RateLimitedHits=%d", counts, h.stub.RateLimitedHits.Load())

	if h.stub.RateLimitedHits.Load() < 1 {
		t.Errorf("L5a expected at least 1 rate-limited response, got 0")
	}
	if total := h.totalCalls(counts); total < 1 {
		t.Errorf("L5a total calls = %d; expected > 0 even after injection", total)
	}
}

// caseL5b — secondary rate-limit (429 + Retry-After). Plan acceptance:
// zero crash; Retry-After honoured by next scheduled call.
func caseL5b(t *testing.T, fixture []fixtureRow) {
	tierCfg := github.DefaultTierConfig()
	tierCfg.HotN = 50
	tierCfg.WarmN = 100
	smallFixture := fixture[:200]
	h := newHarness(t, ghstub.Config{
		SecondaryRateLimitInjectAt:   1 * time.Minute,
		SecondaryRateLimitRetryAfter: 1, // 1s — keeps the test fast
	}, smallFixture, tierCfg)
	h.clock.Advance(2 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	h.tick(ctx)

	counts := h.callCounts()
	t.Logf("L5b counts: %v  stub.RateLimitedHits=%d", counts, h.stub.RateLimitedHits.Load())

	if h.stub.RateLimitedHits.Load() < 1 {
		t.Errorf("L5b expected at least 1 secondary rate-limit response, got 0")
	}
}

// caseL6 — GraphQL partial NOT_FOUND. 5 of 50 aliases resolve to null;
// the scanner must not retry-storm and must surface the 5 missing slugs
// via BulkFetchResult.NotFound (visible as `repo` `error` records in
// the FailedRepos slice).
func caseL6(t *testing.T, fixture []fixtureRow) {
	tierCfg := github.DefaultTierConfig()
	// Trim to one batch (50 repos); set 5 nulls.
	smallFixture := fixture[:50]
	h := newHarness(t, ghstub.Config{
		GraphQLNullAliases: 5,
	}, smallFixture, tierCfg)
	h.preWarmLastCollected()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Force every repo into the bulk path so the GraphQL handler is
	// the only path exercised. Make every repo hot, due immediately,
	// and run a single tick.
	for i := range h.candidates {
		h.candidates[i].LastCollectedAt = time.Time{}
	}
	h.tick(ctx)

	counts := h.callCounts()
	t.Logf("L6 counts: %v  stub.GraphQLCalls=%d", counts, h.stub.GraphQLCalls.Load())

	// Exactly 1 GraphQL batch call expected for 50 repos.
	if got := h.stub.GraphQLCalls.Load(); got != 1 {
		t.Errorf("L6 GraphQL calls = %d, want 1", got)
	}
	// No retry-storm: total calls bounded.
	if total := h.totalCalls(counts); total > 60 { // 1 graphql + 45 activity
		t.Errorf("L6 total calls = %d; possible retry-storm (gate ≤ 60)", total)
	}
}

// caseL7 — mid-run tier promotion. A repo starts in TierCold (rank
// outside the hot/warm thresholds), then has its growth_score lifted
// to a hot rank between ticks. The next tick should classify it as
// hot and fetch it once on its newly-due cadence — never twice in the
// same cycle.
//
// The classifier already short-circuits on `LastCollectedAt.IsZero()`
// for the promoted-from-discovery corner case (a brand-new repo whose
// first_seen_at puts it in the new-repo window AND whose growth_score
// puts it in hot rank). The harness asserts that on a single tick the
// stub records exactly one graphql request for the promoted repo —
// the dispatcher does not place the same repo into both bulk and cold
// batches.
func caseL7(t *testing.T, fixture []fixtureRow) {
	tierCfg := github.DefaultTierConfig()
	tierCfg.HotN = 2
	tierCfg.WarmN = 0
	tierCfg.NewRepoWindow = 0 // suppress the new-repo override so rank wins
	tierCfg.HotInterval = time.Minute
	tierCfg.ColdInterval = 12 * time.Hour
	smallFixture := fixture[:10]

	h := newHarness(t, ghstub.Config{}, smallFixture, tierCfg)

	// Seed the candidate scores so ranks are deterministic before the
	// promotion: candidates[0] = 1e6 (hot), candidates[1] = 1e6-1 (hot),
	// rest = 0 (cold).
	h.candidateMu.Lock()
	for i := range h.candidates {
		switch i {
		case 0:
			h.candidates[i].GrowthScore = 1_000_000
		case 1:
			h.candidates[i].GrowthScore = 999_999
		default:
			h.candidates[i].GrowthScore = 0
		}
	}
	promotedFullName := h.candidates[5].FullName
	h.candidateMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Tick 1: every repo due (LastCollectedAt zero). Hot tier collects
	// candidates[0] + candidates[1] via GraphQL bulk + activity; cold
	// tier collects the other 8 via REST conditional.
	h.tick(ctx)
	c1 := h.callCounts()
	t.Logf("L7 tick1 counts=%v stub.GraphQLCalls=%d stub.RepoCalls=%d",
		c1, h.stub.GraphQLCalls.Load(), h.stub.RepoCalls.Load())

	// Promote candidates[5] into the top tier and reset the harness
	// counters for the next tick so the assertion isolates the
	// promotion cycle.
	h.candidateMu.Lock()
	for i := range h.candidates {
		if h.candidates[i].FullName == promotedFullName {
			h.candidates[i].GrowthScore = 2_000_000 // now top rank
		}
	}
	h.candidateMu.Unlock()

	// Reset stub counters; the candidate's LastCollectedAt is the t1
	// timestamp from the previous tick.
	gqBefore := h.stub.GraphQLCalls.Load()
	repoBefore := h.stub.RepoCalls.Load()

	// Advance past the hot interval so the promoted repo is due.
	h.clock.Advance(2 * tierCfg.HotInterval)
	h.tick(ctx)

	gqAfter := h.stub.GraphQLCalls.Load()
	repoAfter := h.stub.RepoCalls.Load()
	t.Logf("L7 tick2 deltas: graphql+%d  repo+%d  promoted=%s",
		gqAfter-gqBefore, repoAfter-repoBefore, promotedFullName)

	// Hot tier (3 repos: original 2 + promoted) due → 1 graphql batch.
	if delta := gqAfter - gqBefore; delta != 1 {
		t.Errorf("L7 tick2 graphql delta = %d, want 1 (single bulk batch for the 3 hot repos)", delta)
	}
	// Cold tier still on 12h interval — no REST calls expected this tick.
	if delta := repoAfter - repoBefore; delta != 0 {
		t.Errorf("L7 tick2 repo (REST) delta = %d, want 0 (cold not due)", delta)
	}

	// And confirm the promoted repo was processed exactly once via the
	// bulk path (not double-fetched as both bulk and cold).
	c2 := h.callCounts()
	if got := c2["activity|ok"] - c1["activity|ok"]; got < 1 {
		t.Errorf("L7 tick2 activity delta = %d, want >= 1 (promoted repo's activity)", got)
	}
}

// caseL8 — GraphQL transient 5xx on one batch. Per the G3 fix
// (replayable POST body), DoWithRetry retries the 502 with a fresh
// body and the second attempt succeeds. The L8 acceptance is twofold:
//
//  1. The stub serves more requests than there are batches (proving
//     the per-batch retry actually fired).
//  2. Every batch reports `result="ok"` from the client's perspective,
//     because the retry recovered. Activity for every repo still
//     fires (no batch was lost).
//
// If G3 (replayable body) regressed, the retry would silently send an
// empty body, the server would 200 with empty data, and `activity|ok`
// would drop. If G1 (per-batch failure containment) regressed, the
// outer loop would bail on the first batch error and `activity|ok`
// would also drop. Either failure mode trips the assertion below.
func caseL8(t *testing.T, fixture []fixtureRow) {
	tierCfg := github.DefaultTierConfig()
	// 100 repos -> 2 batches at MaxGraphQLBatchSize=50.
	smallFixture := fixture[:100]
	h := newHarness(t, ghstub.Config{
		GraphQLTransient502BatchIndex: 1,
		GraphQLTransient502MaxFires:   1,
	}, smallFixture, tierCfg)

	for i := range h.candidates {
		h.candidates[i].LastCollectedAt = time.Time{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	h.tick(ctx)

	counts := h.callCounts()
	stubReqs := h.stub.GraphQLCalls.Load()
	t.Logf("L8 counts: %v  stub.GraphQLCalls=%d", counts, stubReqs)

	// 100 repos / batch=50 = 2 logical batches. With one transient 502
	// the stub should see >= 3 graphql requests (proving the retry).
	if stubReqs < 3 {
		t.Errorf("L8 stub.GraphQLCalls = %d; expected ≥ 3 (proves G3 replay-retry fired)", stubReqs)
	}
	// Every repo should have its activity collected (proves G1
	// per-batch failure containment kept the second batch flowing).
	if got := counts["activity|ok"]; got != int64(len(smallFixture)) {
		t.Errorf("L8 activity|ok = %d, want %d (one per fixture repo)", got, len(smallFixture))
	}
	// Eventually-successful batches surface as `graphql|ok` from the
	// client's view; the 502 itself was swallowed by DoWithRetry.
	if got := counts["graphql|ok"]; got != 2 {
		t.Errorf("L8 graphql|ok = %d, want 2 (both batches recover)", got)
	}
}
