package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hrexed/github-radar/internal/github"
	"github.com/hrexed/github-radar/internal/state"
)

// gharchiveTestSource builds a *GHArchiveSource pre-populated with the
// given (repo → eventCount) mapping by issuing one ProcessArchive call
// against a fake gharchive server. Reuses the package's existing test
// helpers (gzipNDJSON, fakeArchiveServer, newTestSource) so the source
// reaches its public state via the public ProcessArchive path.
func gharchiveTestSource(t *testing.T, repoEvents map[string]int) *GHArchiveSource {
	t.Helper()
	hour := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	archive := hour.Format(gharchiveArchiveLayout)

	events := make([]map[string]any, 0, 64)
	for repo, count := range repoEvents {
		for i := 0; i < count; i++ {
			events = append(events, map[string]any{
				"type": "WatchEvent",
				"repo": map[string]any{"name": repo},
			})
		}
	}
	body := gzipNDJSON(t, events)
	srv := fakeArchiveServer(t, map[string][]byte{archive: body})
	t.Cleanup(srv.Close)

	src := newTestSource(t, srv.URL, hour.Add(2*time.Hour), NewMemoryCursorStore(), nil, GHArchiveHooks{})
	if err := src.ProcessArchive(context.Background(), archive); err != nil {
		t.Fatalf("seed ProcessArchive: %v", err)
	}
	return src
}

// repoMetricsResponse is the minimal shape of a /repos/{owner}/{repo}
// JSON payload that github.Client.GetRepository decodes.
type repoMetricsResponse struct {
	Owner       map[string]string `json:"owner"`
	Name        string            `json:"name"`
	FullName    string            `json:"full_name"`
	Stars       int               `json:"stargazers_count"`
	Forks       int               `json:"forks_count"`
	OpenIssues  int               `json:"open_issues_count"`
	Language    string            `json:"language"`
	Topics      []string          `json:"topics"`
	Description string            `json:"description"`
}

// fakeRESTServer serves a minimal /repos/{owner}/{repo} endpoint backed
// by an in-memory map. Missing entries return 404 so we can exercise
// the hydration-failure-skipped path.
func fakeRESTServer(t *testing.T, repos map[string]repoMetricsResponse) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path: /repos/{owner}/{repo}
		const prefix = "/repos/"
		path := strings.TrimPrefix(r.URL.Path, prefix)
		entry, ok := repos[path]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(entry)
	}))
	return srv
}

// newPipelineDiscoverer wires a Discoverer + GitHub client + state.Store
// against the given REST server and config. Throttle is disabled.
func newPipelineDiscoverer(t *testing.T, restURL string, cfg Config) *Discoverer {
	t.Helper()
	client, err := github.NewClient("test-token")
	if err != nil {
		t.Fatalf("github.NewClient: %v", err)
	}
	client.SetBaseURL(restURL)

	store := state.NewStore("")
	d := NewDiscoverer(client, store, cfg)
	d.SetSearchThrottle(0)
	return d
}

// repoEntry returns a repoMetricsResponse for a "owner/name" key with
// the given star count.
func repoEntry(fullName string, stars int) repoMetricsResponse {
	owner, name, ok := splitRepoName(fullName)
	if !ok {
		owner, name = "owner", fullName
	}
	return repoMetricsResponse{
		Owner:       map[string]string{"login": owner},
		Name:        name,
		FullName:    fullName,
		Stars:       stars,
		Forks:       1,
		Language:    "Go",
		Topics:      []string{"trending"},
		Description: "fixture",
	}
}

// TestDiscoverFromGHArchive_DisabledReturnsEmpty — Sources.GHArchive.Enabled
// false short-circuits cleanly: non-nil empty result, nil error, no REST
// calls. M1 ([ISI-965]) made disabled return a zero-value Result rather
// than nil so callers don't need a nil guard.
func TestDiscoverFromGHArchive_DisabledReturnsEmpty(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	t.Cleanup(srv.Close)

	d := newPipelineDiscoverer(t, srv.URL, Config{
		Sources: SourcesConfig{GHArchive: GHArchiveSourceConfig{Enabled: false}},
	})
	d.SetGHArchiveSource(gharchiveTestSource(t, map[string]int{"a/b": 100}))

	result, err := d.DiscoverFromGHArchive(context.Background())
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if result == nil {
		t.Fatal("result = nil; want non-nil empty Result (M1)")
	}
	if result.Topic != "gharchive" {
		t.Errorf("Topic = %q, want gharchive", result.Topic)
	}
	if len(result.Repos) != 0 || result.TotalFound != 0 || result.NewRepos != 0 {
		t.Errorf("expected zero-value Result counters, got %+v", result)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Errorf("REST calls = %d, want 0 (disabled must short-circuit before hydration)", got)
	}
}

// TestDiscoverFromGHArchive_NoCollectorReturnsEmpty — Enabled but the
// daemon hasn't called SetGHArchiveSource. Same M1 short-circuit:
// non-nil empty Result, nil error.
func TestDiscoverFromGHArchive_NoCollectorReturnsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(srv.Close)

	d := newPipelineDiscoverer(t, srv.URL, Config{
		Sources: SourcesConfig{GHArchive: GHArchiveSourceConfig{Enabled: true, TopN: 10}},
	})
	// Note: not calling SetGHArchiveSource.

	result, err := d.DiscoverFromGHArchive(context.Background())
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if result == nil {
		t.Fatal("result = nil; want non-nil empty Result (M1)")
	}
	if result.Topic != "gharchive" {
		t.Errorf("Topic = %q, want gharchive", result.Topic)
	}
	if len(result.Repos) != 0 || result.TotalFound != 0 {
		t.Errorf("expected zero-value Result, got %+v", result)
	}
}

// TestDiscoverFromGHArchive_TopNAndFloorPassthrough — verifies the floor
// drops sub-threshold repos and TopN caps the candidate set. Five repos
// with descending event counts; floor=20 drops the bottom two; TopN=2
// caps the top three to two. Expected: only the top two repos make it.
func TestDiscoverFromGHArchive_TopNAndFloorPassthrough(t *testing.T) {
	repoEvents := map[string]int{
		"a/top":    100,
		"a/second": 50,
		"a/third":  25, // above floor (20), but TopN=2 caps before this
		"a/fourth": 15, // below floor (20)
		"a/fifth":  5,  // below floor (20)
	}
	src := gharchiveTestSource(t, repoEvents)

	rest := fakeRESTServer(t, map[string]repoMetricsResponse{
		"a/top":    repoEntry("a/top", 1000),
		"a/second": repoEntry("a/second", 500),
	})
	t.Cleanup(rest.Close)

	d := newPipelineDiscoverer(t, rest.URL, Config{
		Sources: SourcesConfig{GHArchive: GHArchiveSourceConfig{
			Enabled: true, TopN: 2, ActivityFloor: 20,
		}},
		AutoTrackThreshold: 1000, // disable auto-track
	})
	d.SetGHArchiveSource(src)

	result, err := d.DiscoverFromGHArchive(context.Background())
	if err != nil {
		t.Fatalf("DiscoverFromGHArchive: %v", err)
	}
	if result.TotalFound != 2 {
		t.Errorf("TotalFound = %d, want 2 (TopN cap)", result.TotalFound)
	}
	if result.NewRepos != 2 {
		t.Errorf("NewRepos = %d, want 2", result.NewRepos)
	}
	if len(result.Repos) != 2 {
		t.Fatalf("got %d repos, want 2: %+v", len(result.Repos), result.Repos)
	}

	gotNames := map[string]bool{}
	for _, r := range result.Repos {
		gotNames[r.FullName] = true
	}
	if !gotNames["a/top"] || !gotNames["a/second"] {
		t.Errorf("repos = %v, want a/top + a/second", gotNames)
	}
}

// TestDiscoverFromGHArchive_DedupAgainstStore — preseed the store with
// one repo; the gharchive step must skip it (counts AlreadyTracked) and
// not pay the REST hydration cost.
func TestDiscoverFromGHArchive_DedupAgainstStore(t *testing.T) {
	src := gharchiveTestSource(t, map[string]int{
		"a/tracked": 50,
		"a/fresh":   50,
	})

	var hydratedNames []string
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/repos/")
		hydratedNames = append(hydratedNames, path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(repoEntry(path, 100))
	}))
	t.Cleanup(rest.Close)

	d := newPipelineDiscoverer(t, rest.URL, Config{
		Sources: SourcesConfig{GHArchive: GHArchiveSourceConfig{
			Enabled: true, TopN: 10, ActivityFloor: 1,
		}},
		AutoTrackThreshold: 1000,
	})
	d.SetGHArchiveSource(src)
	// Preseed the tracked repo. Use the Discoverer's underlying store.
	d.store.SetRepoState("a/tracked", state.RepoState{
		Owner: "a", Name: "tracked", Stars: 5, LastCollected: time.Now(),
	})

	result, err := d.DiscoverFromGHArchive(context.Background())
	if err != nil {
		t.Fatalf("DiscoverFromGHArchive: %v", err)
	}
	if result.AlreadyTracked != 1 {
		t.Errorf("AlreadyTracked = %d, want 1", result.AlreadyTracked)
	}
	// M2 ([ISI-965]): AlreadyTrackedSkipped mirrors the early-skip path
	// so dedup-parity dashboards can separate it from generic
	// AlreadyTracked counts.
	if result.AlreadyTrackedSkipped != 1 {
		t.Errorf("AlreadyTrackedSkipped = %d, want 1", result.AlreadyTrackedSkipped)
	}
	if result.NewRepos != 1 {
		t.Errorf("NewRepos = %d, want 1", result.NewRepos)
	}
	for _, n := range hydratedNames {
		if n == "a/tracked" {
			t.Errorf("hydrated %q despite tracked-dedup; should skip before REST", n)
		}
	}
}

// TestDiscoverFromGHArchive_Excluded — exclusions short-circuit before
// hydration. Verify both AlreadyTracked and Excluded counters are
// independent: an excluded repo isn't tracked and vice versa.
func TestDiscoverFromGHArchive_Excluded(t *testing.T) {
	src := gharchiveTestSource(t, map[string]int{
		"spammer/repo": 50,
		"clean/repo":   50,
	})

	var hydratedNames []string
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/repos/")
		hydratedNames = append(hydratedNames, path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(repoEntry(path, 100))
	}))
	t.Cleanup(rest.Close)

	d := newPipelineDiscoverer(t, rest.URL, Config{
		Sources: SourcesConfig{GHArchive: GHArchiveSourceConfig{
			Enabled: true, TopN: 10, ActivityFloor: 1,
		}},
		Exclusions:         []string{"spammer/*"},
		AutoTrackThreshold: 1000,
	})
	d.SetGHArchiveSource(src)

	result, err := d.DiscoverFromGHArchive(context.Background())
	if err != nil {
		t.Fatalf("DiscoverFromGHArchive: %v", err)
	}
	if result.Excluded != 1 {
		t.Errorf("Excluded = %d, want 1", result.Excluded)
	}
	if result.NewRepos != 1 {
		t.Errorf("NewRepos = %d, want 1", result.NewRepos)
	}
	for _, n := range hydratedNames {
		if n == "spammer/repo" {
			t.Errorf("hydrated %q despite exclusion; should skip before REST", n)
		}
	}
}

// TestDiscoverFromGHArchive_MinStarsGateOff — gate=0 (default) means
// event volume is the sole signal: a low-star repo passes through.
func TestDiscoverFromGHArchive_MinStarsGateOff(t *testing.T) {
	src := gharchiveTestSource(t, map[string]int{"new/burst": 100})
	rest := fakeRESTServer(t, map[string]repoMetricsResponse{
		"new/burst": repoEntry("new/burst", 3), // 3 stars — well below typical gates
	})
	t.Cleanup(rest.Close)

	d := newPipelineDiscoverer(t, rest.URL, Config{
		Sources: SourcesConfig{GHArchive: GHArchiveSourceConfig{
			Enabled: true, TopN: 10, ActivityFloor: 1, MinStarsGate: 0,
		}},
		AutoTrackThreshold: 1000,
	})
	d.SetGHArchiveSource(src)

	result, err := d.DiscoverFromGHArchive(context.Background())
	if err != nil {
		t.Fatalf("DiscoverFromGHArchive: %v", err)
	}
	if result.NewRepos != 1 {
		t.Errorf("NewRepos = %d, want 1 (gate=0 must let low-star repo through)", result.NewRepos)
	}
}

// TestDiscoverFromGHArchive_MinStarsGateOn — gate=100 filters repos
// below the threshold post-hydration.
func TestDiscoverFromGHArchive_MinStarsGateOn(t *testing.T) {
	src := gharchiveTestSource(t, map[string]int{
		"low/repo":  50,
		"high/repo": 50,
	})
	rest := fakeRESTServer(t, map[string]repoMetricsResponse{
		"low/repo":  repoEntry("low/repo", 5),    // below gate
		"high/repo": repoEntry("high/repo", 500), // above gate
	})
	t.Cleanup(rest.Close)

	d := newPipelineDiscoverer(t, rest.URL, Config{
		Sources: SourcesConfig{GHArchive: GHArchiveSourceConfig{
			Enabled: true, TopN: 10, ActivityFloor: 1, MinStarsGate: 100,
		}},
		AutoTrackThreshold: 1000,
	})
	d.SetGHArchiveSource(src)

	result, err := d.DiscoverFromGHArchive(context.Background())
	if err != nil {
		t.Fatalf("DiscoverFromGHArchive: %v", err)
	}
	if result.NewRepos != 1 {
		t.Errorf("NewRepos = %d, want 1 (gate=100 must filter low/repo)", result.NewRepos)
	}
	if len(result.Repos) != 1 || result.Repos[0].FullName != "high/repo" {
		t.Errorf("repos = %+v, want only high/repo", result.Repos)
	}
}

// TestDiscoverFromGHArchive_MaxAgeDaysIsZeroSafe — when RepoMetrics has
// no UpdatedAt (gharchive's current reality, see inline TODO in
// buildDiscoveredFromGHArchive), MaxAgeDays>0 must NOT silently drop
// the repo. The IsZero guard makes the filter a no-op for unknown ages
// — the gharchive sliding window already enforces recency.
func TestDiscoverFromGHArchive_MaxAgeDaysIsZeroSafe(t *testing.T) {
	src := gharchiveTestSource(t, map[string]int{"a/repo": 100})
	rest := fakeRESTServer(t, map[string]repoMetricsResponse{
		"a/repo": repoEntry("a/repo", 50),
	})
	t.Cleanup(rest.Close)

	d := newPipelineDiscoverer(t, rest.URL, Config{
		Sources: SourcesConfig{GHArchive: GHArchiveSourceConfig{
			Enabled: true, TopN: 10, ActivityFloor: 1,
		}},
		MaxAgeDays:         90,
		AutoTrackThreshold: 1000,
	})
	d.SetGHArchiveSource(src)

	result, err := d.DiscoverFromGHArchive(context.Background())
	if err != nil {
		t.Fatalf("DiscoverFromGHArchive: %v", err)
	}
	if result.NewRepos != 1 {
		t.Errorf("NewRepos = %d, want 1 (zero UpdatedAt must skip the MaxAgeDays filter)", result.NewRepos)
	}
}

// TestDiscoverFromGHArchive_HydrationFailureSkipped — a per-repo REST
// 404 should NOT fail the whole gharchive step; the bad repo is
// skipped and the next one is processed normally.
func TestDiscoverFromGHArchive_HydrationFailureSkipped(t *testing.T) {
	src := gharchiveTestSource(t, map[string]int{
		"missing/repo": 50,
		"good/repo":    50,
	})
	rest := fakeRESTServer(t, map[string]repoMetricsResponse{
		// missing/repo intentionally absent → 404.
		"good/repo": repoEntry("good/repo", 100),
	})
	t.Cleanup(rest.Close)

	d := newPipelineDiscoverer(t, rest.URL, Config{
		Sources: SourcesConfig{GHArchive: GHArchiveSourceConfig{
			Enabled: true, TopN: 10, ActivityFloor: 1,
		}},
		AutoTrackThreshold: 1000,
	})
	d.SetGHArchiveSource(src)

	ctx := context.Background()
	result, err := d.DiscoverFromGHArchive(ctx)
	if err != nil {
		t.Fatalf("DiscoverFromGHArchive: %v (per-repo hydration failure must not bubble up)", err)
	}
	// M3 ([ISI-965]): the per-repo hydration error must NOT surface
	// via the context — DiscoverFromGHArchive swallows the error and
	// keeps going, so ctx.Err() must remain nil and the loop must have
	// processed both candidates (good + missing → len >= 2).
	if ctxErr := ctx.Err(); ctxErr != nil {
		t.Errorf("ctx.Err() = %v after hydration-error swallow; want nil", ctxErr)
	}
	if result.NewRepos != 1 {
		t.Errorf("NewRepos = %d, want 1 (good/repo only)", result.NewRepos)
	}
	if len(result.Repos) != 1 || result.Repos[0].FullName != "good/repo" {
		t.Errorf("repos = %+v, want only good/repo", result.Repos)
	}
}

// TestDiscoverFromGHArchive_EmptyCollector — empty aggregate yields a
// non-nil Result with zero counts and no Repos. No REST traffic.
func TestDiscoverFromGHArchive_EmptyCollector(t *testing.T) {
	src := gharchiveTestSource(t, map[string]int{}) // no events

	var calls int32
	rest := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	t.Cleanup(rest.Close)

	d := newPipelineDiscoverer(t, rest.URL, Config{
		Sources: SourcesConfig{GHArchive: GHArchiveSourceConfig{
			Enabled: true, TopN: 10, ActivityFloor: 1,
		}},
		AutoTrackThreshold: 1000,
	})
	d.SetGHArchiveSource(src)

	result, err := d.DiscoverFromGHArchive(context.Background())
	if err != nil {
		t.Fatalf("DiscoverFromGHArchive: %v", err)
	}
	if result.TotalFound != 0 {
		t.Errorf("TotalFound = %d, want 0", result.TotalFound)
	}
	if len(result.Repos) != 0 {
		t.Errorf("Repos = %+v, want empty", result.Repos)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Errorf("REST calls = %d, want 0 (empty collector must not hydrate)", got)
	}
}

// TestDiscoverFromGHArchive_DefaultsApplied — TopN=0 + ActivityFloor=0
// must fall back to package defaults rather than treating zero as "no
// candidates". This guards against a config-omission regression.
func TestDiscoverFromGHArchive_DefaultsApplied(t *testing.T) {
	src := gharchiveTestSource(t, map[string]int{
		"a/repo": 50, // above DefaultGHArchiveActivityFloor (10)
		"b/tiny": 5,  // below DefaultGHArchiveActivityFloor (10)
	})
	rest := fakeRESTServer(t, map[string]repoMetricsResponse{
		"a/repo": repoEntry("a/repo", 100),
		// b/tiny intentionally absent — should be filtered by floor before hydration.
	})
	t.Cleanup(rest.Close)

	d := newPipelineDiscoverer(t, rest.URL, Config{
		Sources: SourcesConfig{GHArchive: GHArchiveSourceConfig{
			Enabled: true, // TopN and ActivityFloor left at zero
		}},
		AutoTrackThreshold: 1000,
	})
	d.SetGHArchiveSource(src)

	result, err := d.DiscoverFromGHArchive(context.Background())
	if err != nil {
		t.Fatalf("DiscoverFromGHArchive: %v", err)
	}
	if result.NewRepos != 1 {
		t.Errorf("NewRepos = %d, want 1 (default floor=10 must filter b/tiny)", result.NewRepos)
	}
}

// TestDiscoverFromGHArchive_ScoreSeededFromEventVolume — verifies the
// gharchive scoring path: GrowthScore is seeded from TotalEvents, not
// from the search-API stars-velocity heuristic. With one repo,
// normalizeScores yields NormalizedScore=100 (single-repo case).
func TestDiscoverFromGHArchive_ScoreSeededFromEventVolume(t *testing.T) {
	src := gharchiveTestSource(t, map[string]int{"a/repo": 73})
	rest := fakeRESTServer(t, map[string]repoMetricsResponse{
		"a/repo": repoEntry("a/repo", 100),
	})
	t.Cleanup(rest.Close)

	d := newPipelineDiscoverer(t, rest.URL, Config{
		Sources: SourcesConfig{GHArchive: GHArchiveSourceConfig{
			Enabled: true, TopN: 10, ActivityFloor: 1,
		}},
		AutoTrackThreshold: 1000,
	})
	d.SetGHArchiveSource(src)

	result, err := d.DiscoverFromGHArchive(context.Background())
	if err != nil {
		t.Fatalf("DiscoverFromGHArchive: %v", err)
	}
	if len(result.Repos) != 1 {
		t.Fatalf("got %d repos, want 1", len(result.Repos))
	}
	if got := result.Repos[0].GrowthScore; got != 73 {
		t.Errorf("GrowthScore = %v, want 73 (TotalEvents)", got)
	}
}

// TestDiscoverFromGHArchive_AutoTrackThresholdHonored — when the
// normalized score crosses AutoTrackThreshold, ShouldAutoTrack flips
// and AutoTracked counts. Two repos with disparate event counts:
// scoring.NormalizeScores does min-max scaling, so for raw scores 100
// and 25 the higher lands at 100 and the lower at 0. Threshold sweeps
// the boundary.
func TestDiscoverFromGHArchive_AutoTrackThresholdHonored(t *testing.T) {
	src := gharchiveTestSource(t, map[string]int{
		"hot/repo":  100,
		"warm/repo": 25,
	})
	rest := fakeRESTServer(t, map[string]repoMetricsResponse{
		"hot/repo":  repoEntry("hot/repo", 500),
		"warm/repo": repoEntry("warm/repo", 100),
	})
	t.Cleanup(rest.Close)

	cases := []struct {
		threshold       float64
		wantAutoTracked int
	}{
		{0, 2},   // both pass (warm at NS=0, hot at NS=100)
		{50, 1},  // only hot passes
		{120, 0}, // neither passes (max NS=100)
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("threshold_%v", tc.threshold), func(t *testing.T) {
			d := newPipelineDiscoverer(t, rest.URL, Config{
				Sources: SourcesConfig{GHArchive: GHArchiveSourceConfig{
					Enabled: true, TopN: 10, ActivityFloor: 1,
				}},
				AutoTrackThreshold: tc.threshold,
			})
			d.SetGHArchiveSource(src)

			result, err := d.DiscoverFromGHArchive(context.Background())
			if err != nil {
				t.Fatalf("DiscoverFromGHArchive: %v", err)
			}
			if result.AutoTracked != tc.wantAutoTracked {
				t.Errorf("AutoTracked = %d, want %d (threshold=%v)",
					result.AutoTracked, tc.wantAutoTracked, tc.threshold)
			}
		})
	}
}

// TestDiscoverFromGHArchive_ContextCanceled — surface ctx cancellation
// promptly without claiming the result is complete.
func TestDiscoverFromGHArchive_ContextCanceled(t *testing.T) {
	src := gharchiveTestSource(t, map[string]int{
		"a/one":   30,
		"a/two":   20,
		"a/three": 15,
	})

	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// First request triggers a slow path that we'll cancel before
		// it returns. Ensures ctx-cancel is observed mid-flight.
		time.Sleep(50 * time.Millisecond)
		path := strings.TrimPrefix(r.URL.Path, "/repos/")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(repoEntry(path, 100))
	}))
	t.Cleanup(rest.Close)

	d := newPipelineDiscoverer(t, rest.URL, Config{
		Sources: SourcesConfig{GHArchive: GHArchiveSourceConfig{
			Enabled: true, TopN: 10, ActivityFloor: 1,
		}},
		AutoTrackThreshold: 1000,
	})
	d.SetGHArchiveSource(src)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	result, err := d.DiscoverFromGHArchive(ctx)
	if err == nil {
		t.Fatalf("expected ctx-cancel error, got nil (result=%+v)", result)
	}
}

// TestSplitRepoName covers the helper directly.
func TestSplitRepoName(t *testing.T) {
	cases := []struct {
		in    string
		owner string
		name  string
		ok    bool
	}{
		{"foo/bar", "foo", "bar", true},
		{"github/docs", "github", "docs", true},
		{"", "", "", false},
		{"foo", "", "", false},
		{"/bar", "", "", false},
		{"foo/", "", "", false},
		{"foo/bar/baz", "", "", false},
		{"/foo/", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			owner, name, ok := splitRepoName(tc.in)
			if owner != tc.owner || name != tc.name || ok != tc.ok {
				t.Errorf("splitRepoName(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.in, owner, name, ok, tc.owner, tc.name, tc.ok)
			}
		})
	}
}

// TestDiscoverFromGHArchive_GHArchiveStepInBuildSearchPlan verifies the
// integration point: when Sources.GHArchive.Enabled and the collector
// is wired, buildSearchPlan appends a "gharchive" step after the
// search-API steps. The gharchive step must NOT consume the Search API
// quota (consumesSearchAPI=false) so it's exempt from the inter-step
// throttle.
func TestDiscoverFromGHArchive_GHArchiveStepInBuildSearchPlan(t *testing.T) {
	src := gharchiveTestSource(t, map[string]int{"a/x": 10})
	d := newPipelineDiscoverer(t, "http://unused", Config{
		Topics: []string{"trending"},
		Sources: SourcesConfig{
			GHArchive: GHArchiveSourceConfig{Enabled: true, TopN: 10, ActivityFloor: 1},
		},
	})
	d.SetGHArchiveSource(src)

	plan := d.buildSearchPlan()
	var found bool
	for _, step := range plan {
		if step.source == "gharchive" {
			found = true
			if step.consumesSearchAPI {
				t.Errorf("gharchive step has consumesSearchAPI=true; want false (separate quota)")
			}
			if step.label != "gharchive-top-n" {
				t.Errorf("gharchive label = %q, want gharchive-top-n", step.label)
			}
		}
	}
	if !found {
		t.Errorf("no gharchive step in plan; want one when Enabled+collector wired")
	}

	// Confirm the gharchive step is appended LAST so cross-source dedup
	// keeps topic/org/language hits ahead of it.
	if last := plan[len(plan)-1]; last.source != "gharchive" {
		t.Errorf("last step = %q, want gharchive", last.source)
	}
}

// TestDiscoverFromGHArchive_NotInPlanWhenDisabled — confirm the
// gharchive step is omitted when Enabled=false even with a collector
// wired, and when Enabled=true without a collector.
func TestDiscoverFromGHArchive_NotInPlanWhenDisabled(t *testing.T) {
	src := gharchiveTestSource(t, map[string]int{"a/x": 10})

	t.Run("disabled", func(t *testing.T) {
		d := newPipelineDiscoverer(t, "http://unused", Config{
			Topics: []string{"trending"},
			Sources: SourcesConfig{
				GHArchive: GHArchiveSourceConfig{Enabled: false},
			},
		})
		d.SetGHArchiveSource(src)
		for _, step := range d.buildSearchPlan() {
			if step.source == "gharchive" {
				t.Errorf("gharchive step present despite Enabled=false")
			}
		}
	})

	t.Run("no collector", func(t *testing.T) {
		d := newPipelineDiscoverer(t, "http://unused", Config{
			Topics: []string{"trending"},
			Sources: SourcesConfig{
				GHArchive: GHArchiveSourceConfig{Enabled: true},
			},
		})
		// No SetGHArchiveSource call.
		for _, step := range d.buildSearchPlan() {
			if step.source == "gharchive" {
				t.Errorf("gharchive step present despite no collector wired")
			}
		}
	})
}

// TestDiscoverFromGHArchive_MalformedCounters covers both M5 and M6
// from [ISI-965]: a candidate whose name fails splitRepoName
// (e.g. "noslash") and a candidate whose REST hydration returns an
// unidentifiable payload (empty owner/name/fullname) must each
// increment result.Malformed without bubbling an error or polluting
// the per-repo results.
func TestDiscoverFromGHArchive_MalformedCounters(t *testing.T) {
	t.Run("split_fails_M6", func(t *testing.T) {
		// "noslash" survives ProcessArchive (the collector keys by the
		// raw repo name) and reaches the pipeline, where splitRepoName
		// rejects it.
		src := gharchiveTestSource(t, map[string]int{
			"noslash":  20,
			"ok/clean": 20,
		})

		rest := fakeRESTServer(t, map[string]repoMetricsResponse{
			"ok/clean": repoEntry("ok/clean", 100),
		})
		t.Cleanup(rest.Close)

		d := newPipelineDiscoverer(t, rest.URL, Config{
			Sources: SourcesConfig{GHArchive: GHArchiveSourceConfig{
				Enabled: true, TopN: 10, ActivityFloor: 1,
			}},
			AutoTrackThreshold: 1000,
		})
		d.SetGHArchiveSource(src)

		result, err := d.DiscoverFromGHArchive(context.Background())
		if err != nil {
			t.Fatalf("DiscoverFromGHArchive: %v", err)
		}
		if result.Malformed != 1 {
			t.Errorf("Malformed = %d, want 1 (splitRepoName failure)", result.Malformed)
		}
		if result.NewRepos != 1 {
			t.Errorf("NewRepos = %d, want 1 (ok/clean only)", result.NewRepos)
		}
	})

	t.Run("hydration_returns_unidentifiable_M5", func(t *testing.T) {
		src := gharchiveTestSource(t, map[string]int{
			"a/blank":  20,
			"a/clean":  20,
		})

		// a/blank hydrates to an empty-fielded payload — owner login,
		// name, and full_name all blank. M5 must skip + count Malformed.
		rest := fakeRESTServer(t, map[string]repoMetricsResponse{
			"a/blank": {Owner: map[string]string{"login": ""}, Name: "", FullName: "", Stars: 50},
			"a/clean": repoEntry("a/clean", 100),
		})
		t.Cleanup(rest.Close)

		d := newPipelineDiscoverer(t, rest.URL, Config{
			Sources: SourcesConfig{GHArchive: GHArchiveSourceConfig{
				Enabled: true, TopN: 10, ActivityFloor: 1,
			}},
			AutoTrackThreshold: 1000,
		})
		d.SetGHArchiveSource(src)

		result, err := d.DiscoverFromGHArchive(context.Background())
		if err != nil {
			t.Fatalf("DiscoverFromGHArchive: %v", err)
		}
		if result.Malformed != 1 {
			t.Errorf("Malformed = %d, want 1 (unidentifiable hydrated metrics)", result.Malformed)
		}
		if result.NewRepos != 1 {
			t.Errorf("NewRepos = %d, want 1 (a/clean only)", result.NewRepos)
		}
	})
}

// TestTopActiveRepos_NPositiveDefaultsApplied — M7 ([ISI-965]) treats
// n <= 0 as "use Default" rather than "no cap". Seed > DefaultGHArchiveTopN
// repos and confirm the snapshot is capped at the default rather than
// returning every tracked bucket.
func TestTopActiveRepos_NonPositiveDefaultsApplied(t *testing.T) {
	// One repo per bucket — DefaultGHArchiveTopN + a few to overflow.
	repoEvents := make(map[string]int, DefaultGHArchiveTopN+5)
	for i := 0; i < DefaultGHArchiveTopN+5; i++ {
		// Use enough events to clear the default floor.
		repoEvents[fmt.Sprintf("owner/r%05d", i)] = DefaultGHArchiveActivityFloor + 1
	}
	src := gharchiveTestSource(t, repoEvents)

	for _, n := range []int{0, -1, -1000} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			out := src.TopActiveRepos(n, DefaultGHArchiveActivityFloor)
			if len(out) != DefaultGHArchiveTopN {
				t.Errorf("TopActiveRepos(%d, _) returned %d repos; want %d (default cap)",
					n, len(out), DefaultGHArchiveTopN)
			}
		})
	}
}
