package discovery

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// gzipNDJSON returns a gzipped NDJSON payload built from the given
// gharchive event records. Each record is a top-level map; the
// collector's gharchiveEvent only reads `type` and `repo.name` so any
// extra fields are ignored.
func gzipNDJSON(t *testing.T, events []map[string]any) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	enc := json.NewEncoder(gz)
	for _, evt := range events {
		if err := enc.Encode(evt); err != nil {
			t.Fatalf("encoding event: %v", err)
		}
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("closing gzip writer: %v", err)
	}
	return buf.Bytes()
}

// fakeArchiveServer serves /YYYY-MM-DD-HH.json.gz from a map. Missing
// archives respond 404. Useful for retry/iteration tests.
func fakeArchiveServer(t *testing.T, archives map[string][]byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".json.gz")
		body, ok := archives[name]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
	return httptest.NewServer(mux)
}

// freezeClock returns a clock function that always returns t.
func freezeClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// noJitter is a deterministic jitter for retry tests.
func noJitter(_ time.Duration) time.Duration { return 0 }

// newTestSource returns a collector wired to a fake server with a
// frozen clock. All retry waits are kept tiny so suite latency stays
// in the millisecond range.
func newTestSource(t *testing.T, baseURL string, now time.Time, cursor GHArchiveCursorStore, rollup GHArchiveRollupStore, hooks GHArchiveHooks) *GHArchiveSource {
	t.Helper()
	cfg := GHArchiveConfig{
		BaseURL:        baseURL,
		Window:         24 * time.Hour,
		HTTPTimeout:    2 * time.Second,
		MaxRetries:     3,
		InitialBackoff: 10 * time.Millisecond,
	}
	src := NewGHArchiveSource(cfg, cursor, rollup, hooks)
	src.SetClock(freezeClock(now))
	src.SetJitter(noJitter)
	return src
}

func TestGHArchiveCursor_Roundtrip(t *testing.T) {
	mem := NewMemoryCursorStore()
	c1, err := mem.GetCursor(context.Background())
	if err != nil {
		t.Fatalf("GetCursor: %v", err)
	}
	if !c1.IsZero() {
		t.Fatalf("expected zero cursor, got %+v", c1)
	}

	want := GHArchiveCursor{
		LastProcessedArchive: "2026-05-10-12",
		CompletedAt:          time.Date(2026, 5, 10, 13, 0, 0, 0, time.UTC),
	}
	if err := mem.SetCursor(context.Background(), want); err != nil {
		t.Fatalf("SetCursor: %v", err)
	}
	got, err := mem.GetCursor(context.Background())
	if err != nil {
		t.Fatalf("GetCursor: %v", err)
	}
	if got.LastProcessedArchive != want.LastProcessedArchive {
		t.Errorf("LastProcessedArchive = %q, want %q", got.LastProcessedArchive, want.LastProcessedArchive)
	}
	if !got.CompletedAt.Equal(want.CompletedAt) {
		t.Errorf("CompletedAt = %v, want %v", got.CompletedAt, want.CompletedAt)
	}
	if got.IsZero() {
		t.Errorf("IsZero() = true, want false")
	}
	if got.Hour() != time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) {
		t.Errorf("Hour() = %v", got.Hour())
	}
}

// fakeMetadataKV is a tiny in-memory MetadataKVStore.
type fakeMetadataKV struct {
	mu sync.Mutex
	kv map[string]string
}

func (f *fakeMetadataKV) GetMetadata(key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.kv[key], nil
}
func (f *fakeMetadataKV) SetMetadata(key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.kv == nil {
		f.kv = map[string]string{}
	}
	f.kv[key] = value
	return nil
}

func TestMetadataCursorStore_RoundtripJSON(t *testing.T) {
	kv := &fakeMetadataKV{}
	store := NewMetadataCursorStore(kv)

	c1, err := store.GetCursor(context.Background())
	if err != nil {
		t.Fatalf("GetCursor empty: %v", err)
	}
	if !c1.IsZero() {
		t.Fatalf("expected zero cursor, got %+v", c1)
	}

	want := GHArchiveCursor{
		LastProcessedArchive: "2026-05-10-12",
		CompletedAt:          time.Date(2026, 5, 10, 13, 0, 0, 0, time.UTC),
	}
	if err := store.SetCursor(context.Background(), want); err != nil {
		t.Fatalf("SetCursor: %v", err)
	}

	// Verify the stored blob is JSON with the spec'd schema.
	raw := kv.kv[GHArchiveCursorMetadataKey]
	var decoded gharchiveCursorJSON
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("stored value is not valid JSON: %v (raw=%q)", err, raw)
	}
	if decoded.LastProcessedArchive != "2026-05-10-12" {
		t.Errorf("stored last_processed_archive = %q", decoded.LastProcessedArchive)
	}

	got, err := store.GetCursor(context.Background())
	if err != nil {
		t.Fatalf("GetCursor: %v", err)
	}
	if got.LastProcessedArchive != want.LastProcessedArchive ||
		!got.CompletedAt.Equal(want.CompletedAt) {
		t.Errorf("roundtrip = %+v, want %+v", got, want)
	}
}

func TestRingBucket_BasicAggregation(t *testing.T) {
	h := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	b := newRingBucket(h, 24)

	b.set(h, 5, map[string]int{"WatchEvent": 5})
	if b.total() != 5 {
		t.Errorf("total = %d, want 5", b.total())
	}

	// Idempotent overwrite for the same hour.
	b.set(h, 7, map[string]int{"WatchEvent": 7})
	if b.total() != 7 {
		t.Errorf("after re-set: total = %d, want 7", b.total())
	}
}

func TestRingBucket_RotateForward(t *testing.T) {
	h0 := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	b := newRingBucket(h0, 24)
	b.set(h0, 1, map[string]int{"WatchEvent": 1})

	// Add 23 more sequential hours.
	for i := 1; i < 24; i++ {
		hi := h0.Add(time.Duration(i) * time.Hour)
		b.set(hi, i+1, map[string]int{"WatchEvent": i + 1})
	}

	// Sum of 1..24
	wantTotal := 24 * 25 / 2
	if b.total() != wantTotal {
		t.Errorf("total = %d, want %d", b.total(), wantTotal)
	}

	// Window is full: the slot for h0 is the oldest (counts[0]=1).
	got := b.hourCountsOldestFirst()
	if got[0] != 1 || got[23] != 24 {
		t.Errorf("oldest=%d newest=%d, want oldest=1 newest=24", got[0], got[23])
	}
}

func TestRingBucket_SlideAgesOutOldData(t *testing.T) {
	h0 := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	b := newRingBucket(h0, 24)
	b.set(h0, 100, nil)

	// Slide 25 hours forward — the original hour falls out of window.
	b.slideTo(h0.Add(25 * time.Hour))
	if b.total() != 0 {
		t.Errorf("after slide >window: total = %d, want 0", b.total())
	}
}

func TestRingBucket_SlideRetainsInsideWindow(t *testing.T) {
	h0 := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	b := newRingBucket(h0, 24)
	b.set(h0, 50, nil)

	// Slide 5 hours forward — original still inside the 24h window.
	b.slideTo(h0.Add(5 * time.Hour))
	if b.total() != 50 {
		t.Errorf("after slide inside window: total = %d, want 50", b.total())
	}
	// Add a new hour count.
	h5 := h0.Add(5 * time.Hour)
	b.set(h5, 7, nil)
	if b.total() != 57 {
		t.Errorf("after new event: total = %d, want 57", b.total())
	}
}

func TestRingBucket_OutOfWindowDrop(t *testing.T) {
	h0 := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	b := newRingBucket(h0, 24)
	b.set(h0, 10, nil)

	// Try to set an hour 30h in the past — should be dropped silently.
	old := h0.Add(-30 * time.Hour)
	b.set(old, 999, nil)
	if b.total() != 10 {
		t.Errorf("total = %d, want 10 (out-of-window drop)", b.total())
	}
}

func TestProcessArchive_EndToEnd(t *testing.T) {
	hour := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	archive := hour.Format(gharchiveArchiveLayout)

	events := []map[string]any{
		{"type": "WatchEvent", "repo": map[string]any{"name": "alice/repoA"}},
		{"type": "WatchEvent", "repo": map[string]any{"name": "alice/repoA"}},
		{"type": "ForkEvent", "repo": map[string]any{"name": "alice/repoA"}},
		{"type": "PushEvent", "repo": map[string]any{"name": "bob/repoB"}},
		// Filtered out — IssuesEvent isn't in the default filter set.
		{"type": "IssuesEvent", "repo": map[string]any{"name": "carol/skip"}},
		// No repo name — discarded.
		{"type": "WatchEvent", "repo": map[string]any{"name": ""}},
	}
	body := gzipNDJSON(t, events)
	srv := fakeArchiveServer(t, map[string][]byte{archive: body})
	t.Cleanup(srv.Close)

	cursor := NewMemoryCursorStore()
	rollup := newCapturingRollup()

	var keptObs, discardedObs int64
	hooks := GHArchiveHooks{
		OnEventsProcessed: func(_ string, k, d int64) {
			atomic.StoreInt64(&keptObs, k)
			atomic.StoreInt64(&discardedObs, d)
		},
	}

	now := hour.Add(2 * time.Hour) // archive is 2h old
	src := newTestSource(t, srv.URL, now, cursor, rollup, hooks)

	if err := src.ProcessArchive(context.Background(), archive); err != nil {
		t.Fatalf("ProcessArchive: %v", err)
	}

	// Cursor advanced to the processed archive.
	c, _ := cursor.GetCursor(context.Background())
	if c.LastProcessedArchive != archive {
		t.Errorf("cursor = %q, want %q", c.LastProcessedArchive, archive)
	}

	// Filter contract: kept = 4 (3 alice + 1 bob), discarded = 2.
	if got := atomic.LoadInt64(&keptObs); got != 4 {
		t.Errorf("kept = %d, want 4", got)
	}
	if got := atomic.LoadInt64(&discardedObs); got != 2 {
		t.Errorf("discarded = %d, want 2", got)
	}

	// Aggregation: alice/repoA has 3 events, bob/repoB has 1.
	top := src.TopActiveRepos(0, 1)
	if len(top) != 2 {
		t.Fatalf("got %d active repos: %+v", len(top), top)
	}
	if top[0].RepoName != "alice/repoA" || top[0].TotalEvents != 3 {
		t.Errorf("top[0] = %+v, want alice/repoA=3", top[0])
	}
	if top[1].RepoName != "bob/repoB" || top[1].TotalEvents != 1 {
		t.Errorf("top[1] = %+v, want bob/repoB=1", top[1])
	}

	// Rollup got the same data.
	rolls := rollup.snapshot()
	if got := len(rolls[archive]); got != 2 {
		t.Errorf("rollup len = %d, want 2", got)
	}
}

func TestProcessArchive_IdempotentOnReplay(t *testing.T) {
	hour := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	archive := hour.Format(gharchiveArchiveLayout)

	body := gzipNDJSON(t, []map[string]any{
		{"type": "WatchEvent", "repo": map[string]any{"name": "alice/repo"}},
		{"type": "WatchEvent", "repo": map[string]any{"name": "alice/repo"}},
	})
	srv := fakeArchiveServer(t, map[string][]byte{archive: body})
	t.Cleanup(srv.Close)

	src := newTestSource(t, srv.URL, hour.Add(2*time.Hour), NewMemoryCursorStore(), nil, GHArchiveHooks{})

	if err := src.ProcessArchive(context.Background(), archive); err != nil {
		t.Fatalf("first ProcessArchive: %v", err)
	}
	if err := src.ProcessArchive(context.Background(), archive); err != nil {
		t.Fatalf("second ProcessArchive: %v", err)
	}

	top := src.TopActiveRepos(0, 1)
	if len(top) != 1 {
		t.Fatalf("got %d active repos: %+v", len(top), top)
	}
	if top[0].TotalEvents != 2 {
		t.Errorf("total = %d, want 2 (replay should overwrite, not double)", top[0].TotalEvents)
	}
}

func TestProcessArchive_404IsTerminal(t *testing.T) {
	srv := fakeArchiveServer(t, map[string][]byte{}) // empty → all 404
	t.Cleanup(srv.Close)

	var attempts int32
	hooks := GHArchiveHooks{
		OnArchiveError: func(_ string, _ int, _ error) {
			atomic.AddInt32(&attempts, 1)
		},
	}
	cursor := NewMemoryCursorStore()
	src := newTestSource(t, srv.URL, time.Now().UTC(), cursor, nil, hooks)

	err := src.ProcessArchive(context.Background(), "2026-05-10-12")
	if err == nil {
		t.Fatalf("expected error for 404, got nil")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (404 is terminal, not retried)", got)
	}
	c, _ := cursor.GetCursor(context.Background())
	if !c.IsZero() {
		t.Errorf("cursor advanced on 404; should not advance, got %+v", c)
	}
}

// transientThen503 returns 503 on the first N requests, then 200 with body.
type transientHandler struct {
	failures int32
	body     []byte
}

func (h *transientHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	if atomic.AddInt32(&h.failures, -1) >= 0 {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	_, _ = w.Write(h.body)
}

func TestProcessArchive_RetriesOn5xx(t *testing.T) {
	hour := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	archive := hour.Format(gharchiveArchiveLayout)

	body := gzipNDJSON(t, []map[string]any{
		{"type": "WatchEvent", "repo": map[string]any{"name": "x/y"}},
	})
	h := &transientHandler{failures: 2, body: body} // 2 failures, then success
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
		t.Fatalf("ProcessArchive: %v", err)
	}
	if got := atomic.LoadInt32(&errAttempts); got != 2 {
		t.Errorf("error attempts = %d, want 2 (retry budget consumed before success)", got)
	}
	c, _ := cursor.GetCursor(context.Background())
	if c.LastProcessedArchive != archive {
		t.Errorf("cursor = %q, want %q", c.LastProcessedArchive, archive)
	}
}

func TestProcessArchive_RetryBudgetExhausted(t *testing.T) {
	h := &transientHandler{failures: 99} // never recovers
	srv := httptest.NewServer(h)
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
		t.Fatalf("expected error, got nil")
	}
	if got := atomic.LoadInt32(&errAttempts); got != int32(DefaultGHArchiveMaxRetries) {
		t.Errorf("attempts = %d, want %d", got, DefaultGHArchiveMaxRetries)
	}
	c, _ := cursor.GetCursor(context.Background())
	if !c.IsZero() {
		t.Errorf("cursor should not advance on exhaustion, got %+v", c)
	}
}

func TestProcessArchive_RollupErrorBlocksCursorAdvance(t *testing.T) {
	hour := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	archive := hour.Format(gharchiveArchiveLayout)

	body := gzipNDJSON(t, []map[string]any{
		{"type": "WatchEvent", "repo": map[string]any{"name": "x/y"}},
	})
	srv := fakeArchiveServer(t, map[string][]byte{archive: body})
	t.Cleanup(srv.Close)

	cursor := NewMemoryCursorStore()
	rollup := &erroringRollup{err: errors.New("disk full")}
	src := newTestSource(t, srv.URL, hour.Add(2*time.Hour), cursor, rollup, GHArchiveHooks{})

	err := src.ProcessArchive(context.Background(), archive)
	if err == nil {
		t.Fatalf("expected error from rollup write, got nil")
	}
	c, _ := cursor.GetCursor(context.Background())
	if !c.IsZero() {
		t.Errorf("cursor advanced despite rollup failure: %+v", c)
	}
}

func TestRun_IteratesArchivesInOrderAndStopsAtPublishLag(t *testing.T) {
	// Build archives at h0, h0+1h, h0+2h. Frozen "now" at h0+3h means
	// the publish-lag cutoff (now - 30min) sits inside h0+2h, so only
	// h0 and h0+1h are processable. h0+2h is "too fresh" → not fetched.
	h0 := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	body := func(repo string) []byte {
		return gzipNDJSON(t, []map[string]any{
			{"type": "WatchEvent", "repo": map[string]any{"name": repo}},
		})
	}
	archives := map[string][]byte{
		h0.Format(gharchiveArchiveLayout):                    body("h0/x"),
		h0.Add(1 * time.Hour).Format(gharchiveArchiveLayout): body("h1/x"),
		h0.Add(2 * time.Hour).Format(gharchiveArchiveLayout): body("h2/x"),
	}
	var fetched int32
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fetched, 1)
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".json.gz")
		body, ok := archives[name]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cursor := NewMemoryCursorStore()
	// Seed cursor to h0 - 1h so Run should process h0 and h0+1h only.
	if err := cursor.SetCursor(context.Background(), GHArchiveCursor{
		LastProcessedArchive: h0.Add(-1 * time.Hour).Format(gharchiveArchiveLayout),
		CompletedAt:          h0,
	}); err != nil {
		t.Fatalf("seed cursor: %v", err)
	}

	now := h0.Add(3 * time.Hour) // publish lag = now-30m = h0+2h30m
	src := newTestSource(t, srv.URL, now, cursor, nil, GHArchiveHooks{})

	if err := src.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	c, _ := cursor.GetCursor(context.Background())
	want := h0.Add(1 * time.Hour).Format(gharchiveArchiveLayout)
	if c.LastProcessedArchive != want {
		t.Errorf("cursor = %q, want %q", c.LastProcessedArchive, want)
	}
	if got := atomic.LoadInt32(&fetched); got != 2 {
		t.Errorf("fetched = %d archives, want 2 (h0+2h is too fresh)", got)
	}

	// Both repos should be in the active set.
	top := src.TopActiveRepos(0, 1)
	if len(top) != 2 {
		t.Errorf("got %d top repos, want 2: %+v", len(top), top)
	}
}

func TestRun_NoOpWhenCursorAtLeadingEdge(t *testing.T) {
	srv := fakeArchiveServer(t, map[string][]byte{})
	t.Cleanup(srv.Close)

	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	cursor := NewMemoryCursorStore()
	// Cursor already past the publish-lag cutoff.
	if err := cursor.SetCursor(context.Background(), GHArchiveCursor{
		LastProcessedArchive: now.Add(-10 * time.Minute).Format(gharchiveArchiveLayout),
		CompletedAt:          now,
	}); err != nil {
		t.Fatalf("seed cursor: %v", err)
	}
	src := newTestSource(t, srv.URL, now, cursor, nil, GHArchiveHooks{})

	if err := src.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestSlidingWindowAgesOutColdRepos(t *testing.T) {
	// Process h0 with repo X, then 25 sequential archives with repo
	// Y. After the slide, X should have aged out completely.
	h0 := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	archives := map[string][]byte{}
	archives[h0.Format(gharchiveArchiveLayout)] = gzipNDJSON(t, []map[string]any{
		{"type": "WatchEvent", "repo": map[string]any{"name": "cold/X"}},
	})
	for i := 1; i <= 25; i++ {
		archives[h0.Add(time.Duration(i)*time.Hour).Format(gharchiveArchiveLayout)] = gzipNDJSON(t, []map[string]any{
			{"type": "WatchEvent", "repo": map[string]any{"name": "hot/Y"}},
		})
	}
	srv := fakeArchiveServer(t, archives)
	t.Cleanup(srv.Close)

	now := h0.Add(48 * time.Hour)
	src := newTestSource(t, srv.URL, now, NewMemoryCursorStore(), nil, GHArchiveHooks{})
	for i := 0; i <= 25; i++ {
		archive := h0.Add(time.Duration(i) * time.Hour).Format(gharchiveArchiveLayout)
		if err := src.ProcessArchive(context.Background(), archive); err != nil {
			t.Fatalf("ProcessArchive(%s): %v", archive, err)
		}
	}

	top := src.TopActiveRepos(0, 1)
	for _, r := range top {
		if r.RepoName == "cold/X" {
			t.Errorf("cold/X still active after 25h slide: total=%d", r.TotalEvents)
		}
	}
}

func TestHooks_LagAndStartCompleteFire(t *testing.T) {
	hour := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	archive := hour.Format(gharchiveArchiveLayout)
	body := gzipNDJSON(t, []map[string]any{
		{"type": "WatchEvent", "repo": map[string]any{"name": "x/y"}},
	})
	srv := fakeArchiveServer(t, map[string][]byte{archive: body})
	t.Cleanup(srv.Close)

	var (
		startCalls    int32
		completeCalls int32
		lagSeconds    float64
		lagMu         sync.Mutex
	)
	hooks := GHArchiveHooks{
		OnArchiveStart:    func(_ string) { atomic.AddInt32(&startCalls, 1) },
		OnArchiveComplete: func(_ string, _ time.Duration) { atomic.AddInt32(&completeCalls, 1) },
		OnLagSeconds: func(s float64) {
			lagMu.Lock()
			lagSeconds = s
			lagMu.Unlock()
		},
	}

	now := hour.Add(2 * time.Hour) // expect lag = 2*3600 = 7200s
	src := newTestSource(t, srv.URL, now, NewMemoryCursorStore(), nil, hooks)
	if err := src.ProcessArchive(context.Background(), archive); err != nil {
		t.Fatalf("ProcessArchive: %v", err)
	}
	if atomic.LoadInt32(&startCalls) != 1 {
		t.Errorf("OnArchiveStart calls = %d, want 1", startCalls)
	}
	if atomic.LoadInt32(&completeCalls) != 1 {
		t.Errorf("OnArchiveComplete calls = %d, want 1", completeCalls)
	}
	lagMu.Lock()
	defer lagMu.Unlock()
	if lagSeconds != 7200 {
		t.Errorf("lagSeconds = %v, want 7200", lagSeconds)
	}
}

func TestProcessArchive_BadArchiveName(t *testing.T) {
	src := newTestSource(t, "", time.Now().UTC(), NewMemoryCursorStore(), nil, GHArchiveHooks{})
	err := src.ProcessArchive(context.Background(), "not-a-date")
	if err == nil {
		t.Fatalf("expected error for malformed archive name")
	}
	if !strings.Contains(err.Error(), "parsing archive name") {
		t.Errorf("error %v doesn't mention parsing", err)
	}
}

// capturingRollup records every WriteHourRollup call.
type capturingRollup struct {
	mu    sync.Mutex
	calls map[string][]GHArchiveHourAggregate
}

func newCapturingRollup() *capturingRollup {
	return &capturingRollup{calls: map[string][]GHArchiveHourAggregate{}}
}

func (c *capturingRollup) WriteHourRollup(_ context.Context, archive string, agg []GHArchiveHourAggregate) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls[archive] = append(c.calls[archive], agg...)
	return nil
}

func (c *capturingRollup) snapshot() map[string][]GHArchiveHourAggregate {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string][]GHArchiveHourAggregate, len(c.calls))
	for k, v := range c.calls {
		out[k] = append([]GHArchiveHourAggregate(nil), v...)
	}
	return out
}

// erroringRollup always returns the configured error.
type erroringRollup struct {
	err error
}

func (e *erroringRollup) WriteHourRollup(_ context.Context, _ string, _ []GHArchiveHourAggregate) error {
	return e.err
}

// Sanity: TestSlidingWindowAgesOutColdRepos hits a corner where Run
// would also work. Keep this as an explicit sanity check so a future
// refactor of TopActiveRepos doesn't break the contract that
// minEventsTotal=1 hides empty-bucket noise.
func TestTopActiveRepos_RespectsMinEventsTotal(t *testing.T) {
	hour := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	archive := hour.Format(gharchiveArchiveLayout)
	body := gzipNDJSON(t, []map[string]any{
		{"type": "WatchEvent", "repo": map[string]any{"name": "high/repo"}},
		{"type": "WatchEvent", "repo": map[string]any{"name": "high/repo"}},
		{"type": "WatchEvent", "repo": map[string]any{"name": "high/repo"}},
		{"type": "WatchEvent", "repo": map[string]any{"name": "low/repo"}},
	})
	srv := fakeArchiveServer(t, map[string][]byte{archive: body})
	t.Cleanup(srv.Close)

	src := newTestSource(t, srv.URL, hour.Add(2*time.Hour), NewMemoryCursorStore(), nil, GHArchiveHooks{})
	if err := src.ProcessArchive(context.Background(), archive); err != nil {
		t.Fatalf("ProcessArchive: %v", err)
	}
	out := src.TopActiveRepos(0, 2)
	if len(out) != 1 {
		t.Fatalf("got %d, want 1 (min=2): %+v", len(out), out)
	}
	if out[0].RepoName != "high/repo" {
		t.Errorf("got %s, want high/repo", out[0].RepoName)
	}
}

// Cover: Hour() returns zero on malformed cursor.
func TestGHArchiveCursor_HourMalformed(t *testing.T) {
	c := GHArchiveCursor{LastProcessedArchive: "bogus"}
	if !c.Hour().IsZero() {
		t.Errorf("Hour() on malformed = %v, want zero", c.Hour())
	}
}

// Confirms config defaults stay aligned with the AC text in
// [ISI-951](/ISI/issues/ISI-951): WatchEvent, ForkEvent, PushEvent,
// PullRequestEvent.
func TestDefaultEventTypes_MatchesACText(t *testing.T) {
	want := []string{"WatchEvent", "ForkEvent", "PushEvent", "PullRequestEvent"}
	if len(DefaultGHArchiveEventTypes) != len(want) {
		t.Fatalf("got %d defaults, want %d", len(DefaultGHArchiveEventTypes), len(want))
	}
	for i, w := range want {
		if DefaultGHArchiveEventTypes[i] != w {
			t.Errorf("DefaultGHArchiveEventTypes[%d] = %q, want %q", i, DefaultGHArchiveEventTypes[i], w)
		}
	}
}

// Sanity: ensure construction panics on nil cursor — surfacing the
// programming error early beats a nil-pointer crash deep in Run.
func TestNewGHArchiveSource_PanicsOnNilCursor(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on nil cursor store")
		}
	}()
	_ = NewGHArchiveSource(GHArchiveConfig{}, nil, nil, GHArchiveHooks{})
}

// Suppress unused-import warning when test goroutines are stripped.
var _ = fmt.Sprintf
