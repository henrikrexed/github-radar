// Package ghstub provides an httptest-backed GitHub API stub for the
// T5 / ISI-716 load-test harness.
//
// The stub is deterministic: it serves stable repo metadata for every
// requested alias/path, decrements a per-instance X-RateLimit-* budget on
// every successful request, and supports per-scenario fault injection
// (rate-limit responses, GraphQL transient 5xx, GraphQL partial NOT_FOUND,
// rotated ETag). Time-keyed injectors fire from a clock the test owns,
// so the simulation can compress 24h into ~2s without losing ordering.
//
// Endpoints served:
//
//	POST /graphql                                     -> bulk repo metadata (one alias per repo)
//	GET  /repos/{owner}/{name}                        -> single repo metadata; supports If-None-Match
//	GET  /repos/{owner}/{name}/pulls                  -> activity (open/closed PRs)
//	GET  /repos/{owner}/{name}/issues                 -> activity (recent issues)
//	GET  /repos/{owner}/{name}/contributors           -> activity (contributors)
//	GET  /repos/{owner}/{name}/releases/latest        -> activity (latest release)
//
// All responses carry X-RateLimit-Limit / X-RateLimit-Remaining /
// X-RateLimit-Reset headers; Remaining decrements per request and resets
// at the modeled hourly window boundary.
package ghstub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config is the per-stub configuration. Zero value is a sane default
// (no faults, 5,000/hr rate limit, fixed Now()).
type Config struct {
	// RateLimit is the per-window ceiling reported in X-RateLimit-Limit
	// and the value Remaining resets to at the window boundary.
	// Defaults to 5000.
	RateLimit int

	// Window is how often the rate-limit counter resets. Defaults to 1h.
	Window time.Duration

	// Now returns the simulated wall clock. The harness drives this to
	// compress simulated time. Defaults to time.Now.
	Now func() time.Time

	// GraphQLNullAliases is the number of aliases per batch that should
	// resolve to `null` (deleted/renamed repos). Used by L6.
	// 0 means every alias resolves successfully.
	GraphQLNullAliases int

	// GraphQLTransient502BatchIndex, when non-negative, returns HTTP 502
	// for batch indices matching this value (modulo BatchCount tracking).
	// Used by L8.
	// -1 means no transient failure.
	GraphQLTransient502BatchIndex int

	// GraphQLTransient502MaxFires bounds how many times the 502 injector
	// fires across the run. The G3 (replayable POST body) fix flips the
	// next attempt back to 200, so a value of 1 verifies the per-batch
	// retry recovers without burning the outer-loop budget. 0 = unlimited.
	GraphQLTransient502MaxFires int

	// L3ETagRotation, when true, makes the REST repo handler return a
	// fresh ETag and 200 on every request even if the client sends
	// If-None-Match. This forces the conditional-GET write-back path to
	// be exercised so cycle 2 can assert the persisted ETag matches the
	// new server-side value.
	L3ETagRotation bool

	// PrimaryRateLimitInjectAt fires a single 403 + X-RateLimit-Remaining: 0
	// at the first request whose simulated time is >= this offset from
	// the run start. Zero = never inject. Used by L5a.
	PrimaryRateLimitInjectAt time.Duration

	// SecondaryRateLimitInjectAt fires a single 429 + Retry-After at the
	// first request whose simulated time is >= this offset from the run
	// start. Zero = never inject. Used by L5b.
	SecondaryRateLimitInjectAt time.Duration

	// SecondaryRateLimitRetryAfter is the value sent in the Retry-After
	// header when the secondary-rate-limit injector fires. Defaults to
	// 60 (seconds).
	SecondaryRateLimitRetryAfter int
}

// Stub is a running httptest-backed GitHub API stub.
type Stub struct {
	cfg    Config
	server *httptest.Server
	start  time.Time

	mu             sync.Mutex
	rateRemaining  int
	rateResetAt    time.Time
	repoETag       map[string]string // owner/name -> current ETag
	transient502N  int
	primaryFired   bool
	secondaryFired bool
	batchSeen      int

	// CallCounts is incremented per served request. Mostly useful for
	// debugging when a scenario fails. Atomic so the harness can read
	// it without locking.
	GraphQLCalls    atomic.Int64
	RepoCalls       atomic.Int64
	NotModifiedHits atomic.Int64
	ActivityCalls   atomic.Int64 // pulls + issues + contributors + releases combined
	RateLimitedHits atomic.Int64 // 403/429 responses served
}

// New creates and starts a new stub. Caller must call Close() to release
// the listener.
func New(cfg Config) *Stub {
	if cfg.RateLimit == 0 {
		cfg.RateLimit = 5000
	}
	if cfg.Window == 0 {
		cfg.Window = time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.GraphQLTransient502BatchIndex == 0 {
		// Treat zero as the explicit "no injector" sentinel; tests that
		// need batch index 0 set it explicitly via a flag in scenario.
		cfg.GraphQLTransient502BatchIndex = -1
	}
	if cfg.SecondaryRateLimitRetryAfter == 0 {
		cfg.SecondaryRateLimitRetryAfter = 60
	}

	s := &Stub{
		cfg:           cfg,
		start:         cfg.Now(),
		rateRemaining: cfg.RateLimit,
		rateResetAt:   cfg.Now().Add(cfg.Window),
		repoETag:      map[string]string{},
	}
	s.server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// URL returns the base URL of the running stub. Pass to Client.SetBaseURL.
func (s *Stub) URL() string { return s.server.URL }

// Close releases the listener.
func (s *Stub) Close() { s.server.Close() }

// Reset zeros all counters and the rate-limit budget. Useful between
// cycles in the same scenario.
func (s *Stub) Reset() {
	s.mu.Lock()
	s.rateRemaining = s.cfg.RateLimit
	s.rateResetAt = s.cfg.Now().Add(s.cfg.Window)
	s.transient502N = 0
	s.primaryFired = false
	s.secondaryFired = false
	s.batchSeen = 0
	s.mu.Unlock()
	s.GraphQLCalls.Store(0)
	s.RepoCalls.Store(0)
	s.NotModifiedHits.Store(0)
	s.ActivityCalls.Store(0)
	s.RateLimitedHits.Store(0)
}

// Snapshot is a read-only view of the stub's instrumentation counters.
type Snapshot struct {
	GraphQLCalls    int64
	RepoCalls       int64
	NotModifiedHits int64
	ActivityCalls   int64
	RateLimitedHits int64
	RateRemaining   int
}

// Snapshot returns a copy of the counters and rate-limit state.
func (s *Stub) Snapshot() Snapshot {
	s.mu.Lock()
	rem := s.rateRemaining
	s.mu.Unlock()
	return Snapshot{
		GraphQLCalls:    s.GraphQLCalls.Load(),
		RepoCalls:       s.RepoCalls.Load(),
		NotModifiedHits: s.NotModifiedHits.Load(),
		ActivityCalls:   s.ActivityCalls.Load(),
		RateLimitedHits: s.RateLimitedHits.Load(),
		RateRemaining:   rem,
	}
}

// CurrentETag returns the ETag currently advertised for the given
// owner/name. Used by tests to assert the conditional-GET write-back
// path.
func (s *Stub) CurrentETag(fullName string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.repoETag[fullName]
}

// serve is the single httptest handler. It dispatches by path and
// method, applies rate-limit accounting, and finally writes the body.
func (s *Stub) serve(w http.ResponseWriter, r *http.Request) {
	// Decide whether to inject a fault before doing real work. The
	// injectors are ordered: primary (403) wins over secondary (429);
	// both consume one request from the budget.
	if s.tryInjectPrimary(w) {
		return
	}
	if s.tryInjectSecondary(w) {
		return
	}

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/graphql":
		s.handleGraphQL(w, r)
	case r.Method == http.MethodGet:
		s.handleGet(w, r)
	default:
		http.Error(w, "ghstub: unsupported method/path "+r.Method+" "+r.URL.Path, http.StatusBadRequest)
	}
}

// applyRateLimit decrements the remaining budget and writes the
// X-RateLimit-* headers. Must be called before WriteHeader.
func (s *Stub) applyRateLimit(w http.ResponseWriter) {
	s.mu.Lock()
	now := s.cfg.Now()
	if !now.Before(s.rateResetAt) {
		s.rateRemaining = s.cfg.RateLimit
		s.rateResetAt = now.Add(s.cfg.Window)
	}
	if s.rateRemaining > 0 {
		s.rateRemaining--
	}
	rem := s.rateRemaining
	reset := s.rateResetAt.Unix()
	s.mu.Unlock()

	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(s.cfg.RateLimit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(rem))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
}

func (s *Stub) tryInjectPrimary(w http.ResponseWriter) bool {
	if s.cfg.PrimaryRateLimitInjectAt == 0 {
		return false
	}
	s.mu.Lock()
	if s.primaryFired || s.cfg.Now().Sub(s.start) < s.cfg.PrimaryRateLimitInjectAt {
		s.mu.Unlock()
		return false
	}
	s.primaryFired = true
	s.mu.Unlock()

	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(s.cfg.RateLimit))
	w.Header().Set("X-RateLimit-Remaining", "0")
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(s.cfg.Now().Add(s.cfg.Window).Unix(), 10))
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	s.RateLimitedHits.Add(1)
	return true
}

func (s *Stub) tryInjectSecondary(w http.ResponseWriter) bool {
	if s.cfg.SecondaryRateLimitInjectAt == 0 {
		return false
	}
	s.mu.Lock()
	if s.secondaryFired || s.cfg.Now().Sub(s.start) < s.cfg.SecondaryRateLimitInjectAt {
		s.mu.Unlock()
		return false
	}
	s.secondaryFired = true
	s.mu.Unlock()

	w.Header().Set("Retry-After", strconv.Itoa(s.cfg.SecondaryRateLimitRetryAfter))
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(s.cfg.RateLimit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(s.cfg.RateLimit/2))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(s.cfg.Now().Add(s.cfg.Window).Unix(), 10))
	w.WriteHeader(http.StatusTooManyRequests)
	w.Write([]byte(`{"message":"You have exceeded a secondary rate limit"}`))
	s.RateLimitedHits.Add(1)
	return true
}

// handleGet dispatches REST GETs.
func (s *Stub) handleGet(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	// Expected shapes:
	//   /repos/{owner}/{name}
	//   /repos/{owner}/{name}/pulls
	//   /repos/{owner}/{name}/issues
	//   /repos/{owner}/{name}/contributors
	//   /repos/{owner}/{name}/releases/latest
	//   /rate_limit
	if r.URL.Path == "/rate_limit" {
		s.applyRateLimit(w)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"resources":{"core":{"limit":` + strconv.Itoa(s.cfg.RateLimit) + `,"remaining":` + strconv.Itoa(s.cfg.RateLimit) + `}}}`))
		return
	}
	if len(parts) < 3 || parts[0] != "repos" {
		http.NotFound(w, r)
		return
	}
	owner, name := parts[1], parts[2]
	fullName := owner + "/" + name

	if len(parts) == 3 {
		s.handleRepo(w, r, owner, name, fullName)
		return
	}
	switch parts[3] {
	case "pulls", "issues", "contributors":
		s.handleActivityList(w, r, parts[3])
	case "releases":
		if len(parts) >= 5 && parts[4] == "latest" {
			s.handleLatestRelease(w, fullName)
			return
		}
		http.NotFound(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Stub) handleRepo(w http.ResponseWriter, r *http.Request, owner, name, fullName string) {
	// L3 mode: rotate the server-side ETag every request so the client
	// must persist the new value via the conditional-GET write-back.
	s.mu.Lock()
	currentETag, ok := s.repoETag[fullName]
	if !ok || s.cfg.L3ETagRotation {
		currentETag = fmt.Sprintf(`"etag-%s-%d"`, fullName, s.cfg.Now().UnixNano())
		s.repoETag[fullName] = currentETag
	}
	s.mu.Unlock()

	s.applyRateLimit(w)
	w.Header().Set("ETag", currentETag)
	w.Header().Set("Last-Modified", s.cfg.Now().UTC().Format(http.TimeFormat))

	// Conditional GET handling (NOT applicable in L3 mode — rotated ETag
	// will never match what the client sends).
	if !s.cfg.L3ETagRotation {
		if ifNone := r.Header.Get("If-None-Match"); ifNone != "" && ifNone == currentETag {
			s.NotModifiedHits.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	resp := repoResponse(owner, name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
	s.RepoCalls.Add(1)
}

func (s *Stub) handleActivityList(w http.ResponseWriter, _ *http.Request, kind string) {
	s.applyRateLimit(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	switch kind {
	case "pulls":
		// One closed PR; merged_at recent enough to count once. The
		// activity counter increments via GetActivityMetrics — the
		// inner sub-calls are not tagged.
		w.Write([]byte(`[]`))
	case "issues":
		w.Write([]byte(`[]`))
	case "contributors":
		w.Write([]byte(`[{"login":"alice"}]`))
	}
	s.ActivityCalls.Add(1)
}

func (s *Stub) handleLatestRelease(w http.ResponseWriter, _ string) {
	s.applyRateLimit(w)
	// 404 keeps the activity collector simple — no follow-up calls and
	// the GetLatestRelease branch returns (nil, nil).
	w.WriteHeader(http.StatusNotFound)
	s.ActivityCalls.Add(1)
}

func (s *Stub) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	// Track per-stub batch index for the L8 injector.
	s.mu.Lock()
	batchIdx := s.batchSeen
	s.batchSeen++
	shouldFail502 := s.cfg.GraphQLTransient502BatchIndex >= 0 &&
		batchIdx == s.cfg.GraphQLTransient502BatchIndex &&
		(s.cfg.GraphQLTransient502MaxFires == 0 || s.transient502N < s.cfg.GraphQLTransient502MaxFires)
	if shouldFail502 {
		s.transient502N++
	}
	s.mu.Unlock()

	if shouldFail502 {
		s.applyRateLimit(w)
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"message":"transient upstream error"}`))
		s.GraphQLCalls.Add(1)
		return
	}

	body, err := readBody(r)
	if err != nil {
		http.Error(w, "ghstub: read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	aliases := parseAliases(body)

	s.applyRateLimit(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Build aliased response. The first GraphQLNullAliases aliases
	// resolve to null; the rest to a populated repo node.
	data := make(map[string]interface{}, len(aliases))
	for i, a := range aliases {
		if i < s.cfg.GraphQLNullAliases {
			data[a.Alias] = nil
			continue
		}
		data[a.Alias] = map[string]interface{}{
			"nameWithOwner":   a.Owner + "/" + a.Name,
			"stargazerCount":  100 + i,
			"forkCount":       10 + i,
			"issues":          map[string]int{"totalCount": 5},
			"pullRequests":    map[string]int{"totalCount": 2},
			"primaryLanguage": map[string]string{"name": "Go"},
			"repositoryTopics": map[string]interface{}{
				"nodes": []map[string]interface{}{},
			},
			"description": "stub",
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
	s.GraphQLCalls.Add(1)
}

// repoResponse mints a stable repository JSON for the REST handler.
func repoResponse(owner, name string) map[string]interface{} {
	return map[string]interface{}{
		"owner":             map[string]string{"login": owner},
		"name":              name,
		"full_name":         owner + "/" + name,
		"stargazers_count":  100,
		"forks_count":       10,
		"open_issues_count": 5,
		"language":          "Go",
		"topics":            []string{},
		"description":       "stub",
	}
}
