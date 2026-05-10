package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// transientStatusHandler returns failStatus on the first N requests,
// then 200 with body. Optional Retry-After header is set on the failure
// responses. This generalizes transientHandler to cover 429/408 and to
// exercise the Retry-After parsing path.
type transientStatusHandler struct {
	failures   int32
	failStatus int
	retryAfter string
	body       []byte
}

func (h *transientStatusHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	if atomic.AddInt32(&h.failures, -1) >= 0 {
		if h.retryAfter != "" {
			w.Header().Set("Retry-After", h.retryAfter)
		}
		http.Error(w, http.StatusText(h.failStatus), h.failStatus)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	_, _ = w.Write(h.body)
}

// TestProcessArchive_RetriesOn429 covers ISI-960 M2: a 429 Too Many
// Requests is treated like a 5xx (retried with backoff), not as a
// terminal 4xx. Eventual success advances the cursor.
func TestProcessArchive_RetriesOn429(t *testing.T) {
	hour := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	archive := hour.Format(gharchiveArchiveLayout)

	body := gzipNDJSON(t, []map[string]any{
		{"type": "WatchEvent", "repo": map[string]any{"name": "x/y"}},
	})
	h := &transientStatusHandler{failures: 2, failStatus: http.StatusTooManyRequests, body: body}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	var errAttempts int32
	hooks := GHArchiveHooks{
		OnArchiveError: func(_ string, _ int, _ error) {
			atomic.AddInt32(&errAttempts, 1)
		},
	}
	cursor := NewMemoryCursorStore()
	src := newTestSource(t, srv.URL, hour.Add(2*time.Hour), cursor, nil, hooks)

	if err := src.ProcessArchive(context.Background(), archive); err != nil {
		t.Fatalf("ProcessArchive: %v (429 must be retried, not surfaced as terminal)", err)
	}
	if got := atomic.LoadInt32(&errAttempts); got != 2 {
		t.Errorf("error attempts = %d, want 2 (retry budget consumed before success)", got)
	}
	c, _ := cursor.GetCursor(context.Background())
	if c.LastProcessedArchive != archive {
		t.Errorf("cursor = %q, want %q (success after 429 retries should advance)",
			c.LastProcessedArchive, archive)
	}
}

// TestProcessArchive_RetriesOn408 covers ISI-960 M2: 408 Request Timeout
// is retried like 5xx.
func TestProcessArchive_RetriesOn408(t *testing.T) {
	hour := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	archive := hour.Format(gharchiveArchiveLayout)

	body := gzipNDJSON(t, []map[string]any{
		{"type": "WatchEvent", "repo": map[string]any{"name": "x/y"}},
	})
	h := &transientStatusHandler{failures: 1, failStatus: http.StatusRequestTimeout, body: body}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	var errAttempts int32
	hooks := GHArchiveHooks{
		OnArchiveError: func(_ string, _ int, _ error) {
			atomic.AddInt32(&errAttempts, 1)
		},
	}
	cursor := NewMemoryCursorStore()
	src := newTestSource(t, srv.URL, hour.Add(2*time.Hour), cursor, nil, hooks)

	if err := src.ProcessArchive(context.Background(), archive); err != nil {
		t.Fatalf("ProcessArchive: %v (408 must be retried, not surfaced as terminal)", err)
	}
	if got := atomic.LoadInt32(&errAttempts); got != 1 {
		t.Errorf("error attempts = %d, want 1", got)
	}
	c, _ := cursor.GetCursor(context.Background())
	if c.LastProcessedArchive != archive {
		t.Errorf("cursor = %q, want %q", c.LastProcessedArchive, archive)
	}
}

// TestProcessArchive_403StaysTerminal verifies that other 4xx codes
// (here 403 Forbidden) remain terminal: no retries, error surfaces, and
// the cursor doesn't advance. This guards against an over-broad carve-
// out of the 4xx terminal branch.
func TestProcessArchive_403StaysTerminal(t *testing.T) {
	mux := http.NewServeMux()
	var calls int32
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "forbidden", http.StatusForbidden)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	var errAttempts int32
	hooks := GHArchiveHooks{
		OnArchiveError: func(_ string, _ int, _ error) {
			atomic.AddInt32(&errAttempts, 1)
		},
	}
	cursor := NewMemoryCursorStore()
	src := newTestSource(t, srv.URL, time.Now().UTC(), cursor, nil, hooks)

	err := src.ProcessArchive(context.Background(), "2026-05-10-12")
	if err == nil {
		t.Fatalf("expected error for 403, got nil")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("HTTP calls = %d, want 1 (403 must not be retried)", got)
	}
	if got := atomic.LoadInt32(&errAttempts); got != 1 {
		t.Errorf("error attempts = %d, want 1", got)
	}
	c, _ := cursor.GetCursor(context.Background())
	if !c.IsZero() {
		t.Errorf("cursor advanced on 403; should not, got %+v", c)
	}
}

// TestProcessArchive_HonorsRetryAfterSeconds covers ISI-960 M2 bonus:
// when the origin sends Retry-After (delta-seconds form), the next
// retry sleeps for that duration and skips the jitter path.
//
// The check is via jitter accounting: noJitter increments a counter on
// each call. With Retry-After honored, the retry path doesn't reach
// jitterFn for that attempt, so the counter stays at zero across the
// entire request when there is exactly one retry.
func TestProcessArchive_HonorsRetryAfterSeconds(t *testing.T) {
	hour := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	archive := hour.Format(gharchiveArchiveLayout)

	body := gzipNDJSON(t, []map[string]any{
		{"type": "WatchEvent", "repo": map[string]any{"name": "x/y"}},
	})
	h := &transientStatusHandler{
		failures:   1,
		failStatus: http.StatusTooManyRequests,
		retryAfter: "0", // 0 means "no override" — verify parser returns 0
		body:       body,
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	var jitterCalls int32
	cursor := NewMemoryCursorStore()
	src := newTestSource(t, srv.URL, hour.Add(2*time.Hour), cursor, nil, GHArchiveHooks{})
	src.SetJitter(func(max time.Duration) time.Duration {
		atomic.AddInt32(&jitterCalls, 1)
		return 0
	})

	if err := src.ProcessArchive(context.Background(), archive); err != nil {
		t.Fatalf("ProcessArchive: %v", err)
	}
	// Retry-After "0" parses to 0 → fallback to backoff+jitter, so
	// jitter must be called exactly once for the single retry.
	if got := atomic.LoadInt32(&jitterCalls); got != 1 {
		t.Errorf("jitter calls = %d, want 1 (Retry-After=0 must NOT override)", got)
	}

	// Now retry with a positive Retry-After. The trick: send a large
	// Retry-After (60s) and bound the test with a short context. If the
	// override is honored, the retry-sleep blocks until ctx is done,
	// surfacing context.DeadlineExceeded. The fallback path would sleep
	// only ~1ms (InitialBackoff) and call jitter, so a passing override
	// is unambiguously distinguished by:
	//   - jitter call count = 0 (no fallback sleep computed)
	//   - the returned error wraps context.DeadlineExceeded
	h2 := &transientStatusHandler{
		failures:   5, // keep failing for the duration of the test
		failStatus: http.StatusTooManyRequests,
		retryAfter: "60",
		body:       body,
	}
	srv2 := httptest.NewServer(h2)
	t.Cleanup(srv2.Close)

	atomic.StoreInt32(&jitterCalls, 0)
	src2 := newTestSource(t, srv2.URL, hour.Add(2*time.Hour), NewMemoryCursorStore(), nil, GHArchiveHooks{})
	src2.SetJitter(func(_ time.Duration) time.Duration {
		atomic.AddInt32(&jitterCalls, 1)
		return 0
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := src2.ProcessArchive(ctx, archive)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected error from ctx cancellation while Retry-After sleep ran, got nil")
	}
	if got := atomic.LoadInt32(&jitterCalls); got != 0 {
		t.Errorf("jitter calls = %d, want 0 (positive Retry-After must override the jitter fallback path)", got)
	}
	// Sanity: the test should resolve at ~ctx timeout, NOT the full
	// Retry-After delay. If the override were ignored, the fallback
	// 1ms sleep would let attempts 2 and 3 burn through near-instantly.
	if elapsed > 5*time.Second {
		t.Errorf("elapsed = %v, want around the 200ms ctx timeout", elapsed)
	}
}

// TestParseRetryAfter exercises the header parser directly.
func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{"empty", "", 0},
		{"whitespace", "   ", 0},
		{"zero seconds", "0", 0},
		{"negative seconds", "-5", 0},
		{"positive seconds", "30", 30 * time.Second},
		{"padded seconds", "  120  ", 120 * time.Second},
		{"future http-date", now.Add(45 * time.Second).UTC().Format(http.TimeFormat), 45 * time.Second},
		{"past http-date", now.Add(-1 * time.Hour).UTC().Format(http.TimeFormat), 0},
		{"garbage", "definitely not a number or date", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseRetryAfter(tc.header, now)
			// HTTP date parsing has 1-second granularity; allow 1s slack
			// in either direction for the future-date case.
			if tc.name == "future http-date" {
				if got < tc.want-time.Second || got > tc.want+time.Second {
					t.Errorf("parseRetryAfter(%q) = %v, want ~%v", tc.header, got, tc.want)
				}
				return
			}
			if got != tc.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tc.header, got, tc.want)
			}
		})
	}
}

// TestRun_SkipsPoisonArchiveAfterThreshold covers ISI-960 M3: when an
// archive permanently fails (here: server always returns 503), Run
// tracks consecutive Run-cycle failures and after PoisonFailureThreshold
// cycles advances the cursor past it so the firehose can resume making
// progress on subsequent hours.
func TestRun_SkipsPoisonArchiveAfterThreshold(t *testing.T) {
	// Archive at h0 always 503s. h0+1h is "too fresh" relative to
	// publish-lag at our frozen now, so Run() processes only h0 each
	// cycle. After threshold failures, the cursor should jump past h0.
	h0 := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	archive := h0.Format(gharchiveArchiveLayout)

	var hits int32
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	const threshold = 3
	cfg := GHArchiveConfig{
		BaseURL:                srv.URL,
		Window:                 24 * time.Hour,
		HTTPTimeout:            2 * time.Second,
		MaxRetries:             1, // cut per-cycle work; threshold is the focus
		InitialBackoff:         1 * time.Millisecond,
		PoisonFailureThreshold: threshold,
	}
	cursor := NewMemoryCursorStore()
	// Seed cursor to h0 - 1h so Run starts at h0.
	if err := cursor.SetCursor(context.Background(), GHArchiveCursor{
		LastProcessedArchive: h0.Add(-1 * time.Hour).Format(gharchiveArchiveLayout),
		CompletedAt:          h0,
	}); err != nil {
		t.Fatalf("seed cursor: %v", err)
	}

	var skipEvents int32
	var lastSkipErr atomic.Value // string
	hooks := GHArchiveHooks{
		OnArchiveError: func(_ string, attempt int, err error) {
			// attempt==0 is the dedicated poison-skip channel.
			if attempt == 0 {
				atomic.AddInt32(&skipEvents, 1)
				lastSkipErr.Store(err.Error())
			}
		},
	}
	src := NewGHArchiveSource(cfg, cursor, nil, hooks)
	src.SetClock(freezeClock(h0.Add(2 * time.Hour))) // h0+1h is too fresh
	src.SetJitter(noJitter)

	for i := 1; i <= threshold; i++ {
		if err := src.Run(context.Background()); err != nil {
			t.Fatalf("Run cycle %d: %v", i, err)
		}
	}

	c, _ := cursor.GetCursor(context.Background())
	if c.LastProcessedArchive != archive {
		t.Errorf("cursor after %d failed cycles = %q, want %q (poison skip should advance)",
			threshold, c.LastProcessedArchive, archive)
	}
	if got := atomic.LoadInt32(&skipEvents); got != 1 {
		t.Errorf("poison-skip events = %d, want 1", got)
	}
	if v := lastSkipErr.Load(); v == nil || !strings.Contains(v.(string), "poison archive") {
		t.Errorf("poison-skip error message = %v, want substring 'poison archive'", v)
	}

	// One more Run cycle: cursor is now at h0, startHour = h0+1h, which
	// is too fresh, so Run is a no-op and HTTP isn't hit again.
	hitsBefore := atomic.LoadInt32(&hits)
	if err := src.Run(context.Background()); err != nil {
		t.Fatalf("post-skip Run: %v", err)
	}
	if got := atomic.LoadInt32(&hits) - hitsBefore; got != 0 {
		t.Errorf("post-skip Run hit server %d times, want 0", got)
	}
}

// TestRun_PoisonCounterResetsOnSuccess verifies that a transient failure
// streak that doesn't reach the threshold is wiped clean by a single
// successful processing of the same archive — so a flaky archive that
// eventually succeeds doesn't carry phantom failure debt forward.
func TestRun_PoisonCounterResetsOnSuccess(t *testing.T) {
	h0 := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	archive := h0.Format(gharchiveArchiveLayout)
	body := gzipNDJSON(t, []map[string]any{
		{"type": "WatchEvent", "repo": map[string]any{"name": "x/y"}},
	})

	// Server flips between failing and succeeding in a controlled way:
	// 503 on calls 1+2, then 200 on call 3, then 503 on call 4+5+6.
	// Only h0 is processable; we'll Run() three times.
	var call int32
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		c := atomic.AddInt32(&call, 1)
		if c == 3 {
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(body)
			return
		}
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := GHArchiveConfig{
		BaseURL:                srv.URL,
		Window:                 24 * time.Hour,
		HTTPTimeout:            2 * time.Second,
		MaxRetries:             1,
		InitialBackoff:         1 * time.Millisecond,
		PoisonFailureThreshold: 3,
	}
	cursor := NewMemoryCursorStore()
	if err := cursor.SetCursor(context.Background(), GHArchiveCursor{
		LastProcessedArchive: h0.Add(-1 * time.Hour).Format(gharchiveArchiveLayout),
		CompletedAt:          h0,
	}); err != nil {
		t.Fatalf("seed cursor: %v", err)
	}

	var skipEvents int32
	hooks := GHArchiveHooks{
		OnArchiveError: func(_ string, attempt int, _ error) {
			if attempt == 0 {
				atomic.AddInt32(&skipEvents, 1)
			}
		},
	}
	src := NewGHArchiveSource(cfg, cursor, nil, hooks)
	src.SetClock(freezeClock(h0.Add(2 * time.Hour)))
	src.SetJitter(noJitter)

	// Cycle 1: 503 (failure 1)
	// Cycle 2: 503 (failure 2)
	// Cycle 3: 200 (success → counter cleared, cursor advances to h0)
	for i := 1; i <= 3; i++ {
		if err := src.Run(context.Background()); err != nil {
			t.Fatalf("Run cycle %d: %v", i, err)
		}
	}

	c, _ := cursor.GetCursor(context.Background())
	if c.LastProcessedArchive != archive {
		t.Errorf("cursor after recovery = %q, want %q (success should advance via the normal path)",
			c.LastProcessedArchive, archive)
	}
	if got := atomic.LoadInt32(&skipEvents); got != 0 {
		t.Errorf("poison-skip events = %d, want 0 (success in cycle 3 should clear failure debt)", got)
	}
}
