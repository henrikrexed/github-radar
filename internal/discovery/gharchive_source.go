package discovery

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hrexed/github-radar/internal/logging"
)

// gharchive_source.go implements Story 1 of the Path C epic (ISI-950): a
// discovery collector that consumes gharchive.org hourly archives and
// emits per-repo event-volume aggregates over a sliding window.
//
// The collector is intentionally orthogonal to the live-API search-based
// sources in discovery.go. It exposes a plug-in surface (cursor, rollup,
// hooks) so Stories 2 (top-N + dedup), 4 (backpressure), and 5
// (observability) can wire downstream behavior without re-touching the
// archive ingestion path.
//
// NOT to be confused with internal/metrics/gharchive.go which is a
// fallback path that hits gharchive only when the live GitHub API metric
// poll fails for an already-known repo. That collector is per-repo
// targeted; this one is the firehose.

// Default knobs.
const (
	// DefaultGHArchiveBaseURL is the canonical archive host.
	DefaultGHArchiveBaseURL = "https://data.gharchive.org"

	// DefaultGHArchiveWindow is the sliding window over which per-repo
	// event counts are retained. 24h matches Stage C acceptance per
	// [ISI-950](/ISI/issues/ISI-950).
	DefaultGHArchiveWindow = 24 * time.Hour

	// DefaultGHArchiveHTTPTimeout is the per-archive download timeout.
	// Each archive is ~60-90 MB compressed; 60s leaves headroom on
	// slow links.
	DefaultGHArchiveHTTPTimeout = 60 * time.Second

	// DefaultGHArchiveMaxRetries caps transient-error retries per
	// archive. Three attempts with exponential backoff covers the
	// observed gharchive S3 origin blip rate.
	DefaultGHArchiveMaxRetries = 3

	// DefaultGHArchiveInitialBackoff is the first retry delay; each
	// retry doubles up to GHArchiveMaxBackoff.
	DefaultGHArchiveInitialBackoff = 1 * time.Second

	// GHArchiveMaxBackoff is the cap on per-retry backoff.
	GHArchiveMaxBackoff = 30 * time.Second

	// GHArchivePublishLag is the safety margin applied past the end of
	// each hour before its archive is considered ready for fetch.
	// gharchive publishes archives ~5 min after each hour closes; we
	// pad to 30 min to avoid 404s on the leading edge. Combined with
	// the implicit +1h (an archive at hour H covers events during
	// [H, H+1h) so isn't published until at least H+1h), an archive
	// is ready when `now >= archiveHour + 1h + GHArchivePublishLag`.
	GHArchivePublishLag = 30 * time.Minute

	// DefaultGHArchivePoisonFailureThreshold caps how many consecutive
	// Run() cycles a single archive is allowed to fail before Run skips
	// past it (advancing the cursor) so the firehose can keep up. Tuned
	// to 5 in [ISI-960](/ISI/issues/ISI-960): high enough to ride out a
	// gharchive S3 origin outage, low enough to bound the stall to a
	// few hours under default heartbeat cadence.
	DefaultGHArchivePoisonFailureThreshold = 5

	// gharchiveArchiveLayout is the gharchive filename / cursor format.
	gharchiveArchiveLayout = "2006-01-02-15"
)

// DefaultGHArchiveEventTypes is the canonical filter list per
// [ISI-951](/ISI/issues/ISI-951) AC. Override via
// GHArchiveConfig.EventTypes when you need a narrower or wider scope.
var DefaultGHArchiveEventTypes = []string{
	"WatchEvent",
	"ForkEvent",
	"PushEvent",
	"PullRequestEvent",
}

// GHArchiveConfig contains knobs for the gharchive discovery collector.
type GHArchiveConfig struct {
	// BaseURL overrides the gharchive origin. Empty falls back to
	// DefaultGHArchiveBaseURL. Tests use httptest.Server.URL here.
	BaseURL string

	// EventTypes is the set of GitHub event types kept in the
	// aggregate. Empty falls back to DefaultGHArchiveEventTypes.
	EventTypes []string

	// Window is the sliding aggregation window. Zero falls back to
	// DefaultGHArchiveWindow. Bucket granularity is fixed at one
	// hour — windows shorter than 1h are clamped to 1h.
	Window time.Duration

	// HTTPTimeout is the per-archive download timeout. Zero falls
	// back to DefaultGHArchiveHTTPTimeout.
	HTTPTimeout time.Duration

	// MaxRetries is the retry budget per archive across transient
	// errors. Zero falls back to DefaultGHArchiveMaxRetries.
	MaxRetries int

	// InitialBackoff is the first retry delay; each retry doubles
	// up to GHArchiveMaxBackoff. Zero falls back to
	// DefaultGHArchiveInitialBackoff.
	InitialBackoff time.Duration

	// PoisonFailureThreshold caps how many consecutive Run() cycles a
	// single archive may fail before Run advances the cursor past it.
	// Zero falls back to DefaultGHArchivePoisonFailureThreshold.
	// Negative is clamped to 1 (skip after the first failure).
	PoisonFailureThreshold int
}

// withDefaults returns a copy of cfg with empty fields populated.
func (c GHArchiveConfig) withDefaults() GHArchiveConfig {
	if c.BaseURL == "" {
		c.BaseURL = DefaultGHArchiveBaseURL
	}
	c.BaseURL = strings.TrimSuffix(c.BaseURL, "/")
	if len(c.EventTypes) == 0 {
		c.EventTypes = DefaultGHArchiveEventTypes
	}
	if c.Window <= 0 {
		c.Window = DefaultGHArchiveWindow
	}
	if c.Window < time.Hour {
		c.Window = time.Hour
	}
	if c.HTTPTimeout <= 0 {
		c.HTTPTimeout = DefaultGHArchiveHTTPTimeout
	}
	if c.MaxRetries < 0 {
		c.MaxRetries = 0
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = DefaultGHArchiveMaxRetries
	}
	if c.InitialBackoff <= 0 {
		c.InitialBackoff = DefaultGHArchiveInitialBackoff
	}
	if c.PoisonFailureThreshold == 0 {
		c.PoisonFailureThreshold = DefaultGHArchivePoisonFailureThreshold
	}
	if c.PoisonFailureThreshold < 1 {
		c.PoisonFailureThreshold = 1
	}
	return c
}

// GHArchiveCursor records the last-fully-processed archive. Cursor
// advances only after the archive is decoded, aggregated, and (if a
// rollup store is configured) snapshotted — never on download start.
// Mid-archive crash → re-process from scratch. Aggregation is
// idempotent on (repo, hour-bucket) so reprocessing is safe.
type GHArchiveCursor struct {
	// LastProcessedArchive is in YYYY-MM-DD-HH format (UTC). Empty
	// when no archive has ever been processed.
	LastProcessedArchive string

	// CompletedAt is the wall-clock time the archive finished
	// processing.
	CompletedAt time.Time
}

// IsZero reports whether the cursor is empty (no archive processed yet).
func (c GHArchiveCursor) IsZero() bool {
	return c.LastProcessedArchive == "" && c.CompletedAt.IsZero()
}

// Hour returns the parsed UTC hour represented by the cursor, or zero
// time when the cursor is empty / malformed.
func (c GHArchiveCursor) Hour() time.Time {
	if c.LastProcessedArchive == "" {
		return time.Time{}
	}
	t, err := time.ParseInLocation(gharchiveArchiveLayout, c.LastProcessedArchive, time.UTC)
	if err != nil {
		return time.Time{}
	}
	return t
}

// GHArchiveCursorStore persists the discovery cursor across restarts.
// The default binding (NewMetadataCursorStore) maps to a single key in
// the SQLite metadata table; tests pass an in-memory implementation.
type GHArchiveCursorStore interface {
	GetCursor(ctx context.Context) (GHArchiveCursor, error)
	SetCursor(ctx context.Context, c GHArchiveCursor) error
}

// GHArchiveHourAggregate is one (repo, hour-bucket) cell of the rollup
// snapshot. RollupStore implementations persist one record per cell at
// archive-completion time.
type GHArchiveHourAggregate struct {
	RepoName    string
	HourBucket  time.Time // UTC, hour-aligned
	EventCount  int
	PerEventTyp map[string]int
}

// GHArchiveRollupStore optionally persists per-repo per-hour event
// counts for crash recovery + analytics. Story 1 wires the in-memory
// hot path; the rollup writer is plug-in by design — Story 2 binds the
// real `gharchive_repo_window` table per the [ISI-950 plan](/ISI/issues/ISI-950#document-plan).
//
// A nil store is valid — the collector skips rollup writes and the
// cursor advances on in-memory aggregation alone.
type GHArchiveRollupStore interface {
	WriteHourRollup(ctx context.Context, archive string, aggregates []GHArchiveHourAggregate) error
}

// GHArchiveHooks is the metric-callback surface consumed by Story 5
// observability ([ISI-955](/ISI/issues/ISI-955)). All hooks are
// optional; a nil callback is a no-op. The hooks are the *only* path
// for the collector to emit telemetry — keeping the metric SDK out of
// this package keeps the unit tests free of OTel setup.
//
// Spec:
//   - OnEventsProcessed fires once per archive after decode completes.
//     `keptByType` maps gharchive event type to count of events kept by
//     the type filter for tracked repos (here: all repos seen in the
//     firehose). The map is non-nil but may be empty when no events
//     survived the filter; total kept = sum of values. `discarded`
//     counts events that didn't match the type filter or had no usable
//     repo name. The map is owned by the caller after the hook returns
//     (no further mutation by the collector).
//   - OnLagSeconds fires once per archive on entry; lag is wall-clock
//     minus archive-hour, useful for dashboards to spot cursor stall.
//   - OnArchiveStart / OnArchiveComplete bracket per-archive work for
//     timing histograms.
//   - OnArchiveError fires per failed attempt (transient + final). The
//     `attempt` counter is 1-based.
type GHArchiveHooks struct {
	OnEventsProcessed func(archive string, keptByType map[string]int64, discarded int64)
	OnLagSeconds      func(seconds float64)
	OnArchiveStart    func(archive string)
	OnArchiveComplete func(archive string, dur time.Duration)
	OnArchiveError    func(archive string, attempt int, err error)
}

// GHArchiveSource is the gharchive discovery collector. It is safe for
// concurrent reads against the in-memory aggregate via a mutex; the
// archive-processing loop assumes a single writer goroutine.
type GHArchiveSource struct {
	cfg    GHArchiveConfig
	client *http.Client
	cursor GHArchiveCursorStore
	rollup GHArchiveRollupStore
	hooks  GHArchiveHooks

	// nowFn is a clock indirection so tests can pin "current" time
	// without time.Now drift.
	nowFn func() time.Time

	// jitterFn produces a random duration in [0, max). Indirected so
	// tests can pin retry timing.
	jitterFn func(max time.Duration) time.Duration

	// eventTypes is the indexed filter set built from cfg.EventTypes.
	eventTypes map[string]bool

	mu      sync.RWMutex
	buckets map[string]*ringBucket

	// poisonMu guards consecutiveFailures. Held only briefly during Run
	// — Run is the single writer goroutine for ProcessArchive but the
	// hooks fire under callers' goroutines, so a dedicated mutex keeps
	// the failure tracker thread-safe under -race.
	poisonMu            sync.Mutex
	consecutiveFailures map[string]int
}

// NewGHArchiveSource constructs a collector. cursorStore must be
// non-nil; rollupStore is optional (nil → no rollup writes); hooks
// fields are individually optional.
func NewGHArchiveSource(
	cfg GHArchiveConfig,
	cursorStore GHArchiveCursorStore,
	rollupStore GHArchiveRollupStore,
	hooks GHArchiveHooks,
) *GHArchiveSource {
	if cursorStore == nil {
		// A real bug at the call site; surface immediately.
		panic("discovery: NewGHArchiveSource requires a non-nil cursor store")
	}
	cfg = cfg.withDefaults()

	idx := make(map[string]bool, len(cfg.EventTypes))
	for _, t := range cfg.EventTypes {
		idx[t] = true
	}

	return &GHArchiveSource{
		cfg:                 cfg,
		client:              &http.Client{Timeout: cfg.HTTPTimeout},
		cursor:              cursorStore,
		rollup:              rollupStore,
		hooks:               hooks,
		nowFn:               func() time.Time { return time.Now().UTC() },
		jitterFn:            func(max time.Duration) time.Duration { return time.Duration(rand.Int63n(int64(max + 1))) }, //nolint:gosec
		eventTypes:          idx,
		buckets:             make(map[string]*ringBucket),
		consecutiveFailures: make(map[string]int),
	}
}

// SetClock overrides the wall-clock used to compute archive freshness.
// Tests pin this to a deterministic value; production callers don't
// need it.
func (s *GHArchiveSource) SetClock(now func() time.Time) {
	if now != nil {
		s.nowFn = now
	}
}

// SetHTTPClient overrides the HTTP client. Tests pass a client wired
// to httptest.Server; production code can pre-build a client with
// custom transport (e.g. with proxy settings) and inject it here.
func (s *GHArchiveSource) SetHTTPClient(c *http.Client) {
	if c != nil {
		s.client = c
	}
}

// SetJitter overrides the retry-jitter function. Tests use a constant
// 0 to make backoff deterministic.
func (s *GHArchiveSource) SetJitter(jit func(max time.Duration) time.Duration) {
	if jit != nil {
		s.jitterFn = jit
	}
}

// Run advances the cursor through every archive that is at least
// GHArchivePublishLag old, in chronological order. Returns when ctx is
// cancelled or no further archives are available. Errors are logged
// per-archive; one bad archive does not fail the whole run.
//
// The starting point is:
//   - cursor's LastProcessedArchive + 1h, when the cursor is set
//   - now - cfg.Window, when the cursor is empty (first start)
//
// On every successful archive: aggregation lands in-memory, the
// optional rollup store gets the hour snapshot, and only then does
// the cursor advance — so a crash mid-archive replays that archive.
func (s *GHArchiveSource) Run(ctx context.Context) error {
	cursor, err := s.cursor.GetCursor(ctx)
	if err != nil {
		return fmt.Errorf("loading gharchive cursor: %w", err)
	}

	startHour := s.nextStartHour(cursor)
	// An archive at hour H is "ready" iff now >= H + 1h + lag, so the
	// latest processable archive is at hour floor((now - 1h - lag)/1h).
	endHour := s.nowFn().UTC().Add(-time.Hour - GHArchivePublishLag).Truncate(time.Hour)

	if startHour.After(endHour) {
		// Cursor already at the leading edge; nothing to do.
		return nil
	}

	for h := startHour; !h.After(endHour); h = h.Add(time.Hour) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		archive := h.Format(gharchiveArchiveLayout)
		if err := s.ProcessArchive(ctx, archive); err != nil {
			// ProcessArchive logs; continue with the next archive
			// rather than stalling the whole run on one bad hour.
			// Cursor advance for the failed archive depends on
			// the poison-skip path below.
			logging.Warn("gharchive_source: archive failed; continuing",
				"archive", archive, "error", err)
			s.handleArchiveFailure(ctx, archive, err)
			continue
		}
		// Success: clear any prior failure tally for this archive so
		// transient blips never accumulate toward the poison threshold.
		s.poisonMu.Lock()
		delete(s.consecutiveFailures, archive)
		s.poisonMu.Unlock()
	}

	return nil
}

// handleArchiveFailure tracks consecutive Run-cycle failures per archive
// and skips past a poison archive once the threshold is reached. Without
// the skip, a permanently undecodable archive (truncated upstream,
// persistent gzip CRC error, persistent 5xx) would stall the cursor at
// h-1 forever and the firehose would never catch up to today.
//
// Skip semantics: advance the cursor as if the archive succeeded
// (LastProcessedArchive = archive) and emit OnArchiveError with a
// distinguishing "poison archive" message + attempt=0 so dashboards can
// count skips separately from in-fetch errors.
func (s *GHArchiveSource) handleArchiveFailure(ctx context.Context, archive string, cause error) {
	s.poisonMu.Lock()
	s.consecutiveFailures[archive]++
	failures := s.consecutiveFailures[archive]
	s.poisonMu.Unlock()

	if failures < s.cfg.PoisonFailureThreshold {
		return
	}

	skipErr := fmt.Errorf("poison archive: skipping after %d consecutive failures: %w", failures, cause)
	logging.Warn("gharchive_source: skipping poison archive",
		"archive", archive,
		"consecutive_failures", failures,
		"threshold", s.cfg.PoisonFailureThreshold,
		"cause", cause)

	if s.hooks.OnArchiveError != nil {
		s.hooks.OnArchiveError(archive, 0, skipErr)
	}

	if err := s.cursor.SetCursor(ctx, GHArchiveCursor{
		LastProcessedArchive: archive,
		CompletedAt:          s.nowFn().UTC(),
	}); err != nil {
		// Cursor write failed: leave the failure count in place so
		// the next cycle retries the skip. Do not delete the entry.
		logging.Warn("gharchive_source: failed to advance cursor past poison archive",
			"archive", archive, "error", err)
		return
	}

	s.poisonMu.Lock()
	delete(s.consecutiveFailures, archive)
	s.poisonMu.Unlock()
}

// nextStartHour returns the first hour Run should attempt to process.
func (s *GHArchiveSource) nextStartHour(cursor GHArchiveCursor) time.Time {
	if !cursor.IsZero() {
		return cursor.Hour().Add(time.Hour)
	}
	return s.nowFn().UTC().Add(-s.cfg.Window).Truncate(time.Hour)
}

// ProcessArchive downloads one archive, aggregates filtered events,
// snapshots the rollup, and advances the cursor. Errors are returned
// after the retry budget is exhausted. Idempotent on (repo, hour) so
// safe to call repeatedly for the same archive.
func (s *GHArchiveSource) ProcessArchive(ctx context.Context, archive string) error {
	hour, err := time.ParseInLocation(gharchiveArchiveLayout, archive, time.UTC)
	if err != nil {
		return fmt.Errorf("parsing archive name %q: %w", archive, err)
	}

	if s.hooks.OnArchiveStart != nil {
		s.hooks.OnArchiveStart(archive)
	}
	if s.hooks.OnLagSeconds != nil {
		lag := s.nowFn().UTC().Sub(hour).Seconds()
		s.hooks.OnLagSeconds(lag)
	}

	start := time.Now()
	body, err := s.fetchArchiveWithRetry(ctx, archive)
	if err != nil {
		return err
	}
	defer body.Close()

	keptByType, discarded, hourAggregates, err := s.consume(ctx, hour, body)
	if err != nil {
		return fmt.Errorf("processing archive %s: %w", archive, err)
	}

	if s.hooks.OnEventsProcessed != nil {
		s.hooks.OnEventsProcessed(archive, keptByType, discarded)
	}

	if s.rollup != nil && len(hourAggregates) > 0 {
		if err := s.rollup.WriteHourRollup(ctx, archive, hourAggregates); err != nil {
			// Rollup write failure is fatal for cursor advance:
			// the architect's gate is "advance only after full
			// archive aggregation + rollup write succeeds".
			return fmt.Errorf("writing rollup for %s: %w", archive, err)
		}
	}

	if err := s.cursor.SetCursor(ctx, GHArchiveCursor{
		LastProcessedArchive: archive,
		CompletedAt:          s.nowFn().UTC(),
	}); err != nil {
		return fmt.Errorf("advancing cursor to %s: %w", archive, err)
	}

	if s.hooks.OnArchiveComplete != nil {
		s.hooks.OnArchiveComplete(archive, time.Since(start))
	}

	var kept int64
	for _, c := range keptByType {
		kept += c
	}
	logging.Debug("gharchive_source: archive complete",
		"archive", archive, "kept", kept, "discarded", discarded,
		"unique_repos", len(hourAggregates))
	return nil
}

// fetchArchiveWithRetry hits the gharchive origin with bounded
// exponential backoff for transient failures (network errors, 5xx, plus
// the retry-friendly 4xx status codes 408 and 429). Other 4xx responses
// (notably 404 for not-yet-published archives, 403 for permission
// issues) are treated as terminal — there is no benefit to retrying.
//
// When the origin sends `Retry-After` on a retryable response, that
// value overrides the computed exponential-backoff sleep for the next
// attempt (capped at GHArchiveMaxBackoff). This honors RFC 9110/9111 so
// we don't hammer a struggling S3 origin that already told us when to
// come back.
func (s *GHArchiveSource) fetchArchiveWithRetry(ctx context.Context, archive string) (io.ReadCloser, error) {
	url := fmt.Sprintf("%s/%s.json.gz", s.cfg.BaseURL, archive)

	var lastErr error
	backoff := s.cfg.InitialBackoff

	for attempt := 1; attempt <= s.cfg.MaxRetries; attempt++ {
		// Reset per-attempt retry-after override; only set when the
		// current response carries a parseable Retry-After header.
		var retryAfter time.Duration

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("building request for %s: %w", url, err)
		}

		resp, err := s.client.Do(req)
		switch {
		case err != nil:
			lastErr = err
			if s.hooks.OnArchiveError != nil {
				s.hooks.OnArchiveError(archive, attempt, err)
			}
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return resp.Body, nil
		case isRetryableHTTPStatus(resp.StatusCode):
			// 408 / 429 / 5xx: transient under burst load on the
			// gharchive S3 origin. Honor Retry-After when present.
			lastErr = fmt.Errorf("gharchive %s: %s", url, resp.Status)
			retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"), s.nowFn())
			resp.Body.Close()
			if s.hooks.OnArchiveError != nil {
				s.hooks.OnArchiveError(archive, attempt, lastErr)
			}
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			// Other 4xx is terminal: 404 for not-yet-published
			// archives and 403 for permission issues won't fix on
			// retry, so fail fast and let Run decide whether to
			// skip-past via the poison threshold.
			lastErr = fmt.Errorf("gharchive %s: %s", url, resp.Status)
			resp.Body.Close()
			if s.hooks.OnArchiveError != nil {
				s.hooks.OnArchiveError(archive, attempt, lastErr)
			}
			return nil, lastErr
		default:
			// Non-2xx, non-4xx (i.e. 1xx/3xx unexpected here, or
			// any future >=600 oddity): treat as transient.
			lastErr = fmt.Errorf("gharchive %s: %s", url, resp.Status)
			resp.Body.Close()
			if s.hooks.OnArchiveError != nil {
				s.hooks.OnArchiveError(archive, attempt, lastErr)
			}
		}

		if attempt == s.cfg.MaxRetries {
			break
		}

		// Exponential backoff with jitter — capped at GHArchiveMaxBackoff.
		// Retry-After (when present and positive) overrides the computed
		// sleep so the origin's hint wins.
		var sleep time.Duration
		if retryAfter > 0 {
			sleep = retryAfter
		} else {
			sleep = backoff + s.jitterFn(backoff/2)
		}
		if sleep > GHArchiveMaxBackoff {
			sleep = GHArchiveMaxBackoff
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(sleep):
		}
		backoff *= 2
		if backoff > GHArchiveMaxBackoff {
			backoff = GHArchiveMaxBackoff
		}
	}

	if lastErr == nil {
		lastErr = errors.New("retries exhausted")
	}
	return nil, fmt.Errorf("gharchive fetch %s: %w", url, lastErr)
}

// isRetryableHTTPStatus returns true for HTTP responses that are worth
// retrying with backoff: all 5xx plus the retry-friendly 4xx codes 408
// (Request Timeout) and 429 (Too Many Requests). Both 408 and 429 are
// retry-friendly per RFC 9110/9111 and are observed from gharchive's S3
// origin under burst load.
func isRetryableHTTPStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	}
	return code >= 500 && code < 600
}

// parseRetryAfter interprets the Retry-After header per RFC 9110 §10.2.3.
// Returns 0 (caller should fall back to exponential backoff) when the
// header is empty, malformed, or names a past instant.
//
// Two forms are accepted:
//   - delta-seconds: integer seconds to wait (e.g. "120")
//   - HTTP-date: RFC 1123 / RFC 850 / asctime instant (e.g.
//     "Fri, 31 Dec 1999 23:59:59 GMT")
func parseRetryAfter(header string, now time.Time) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if secs, err := strconv.Atoi(header); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(header); err == nil {
		d := t.Sub(now)
		if d <= 0 {
			return 0
		}
		return d
	}
	return 0
}

// gharchiveEvent is the minimal envelope we need from each archive line.
// gharchive uses NDJSON (newline-delimited JSON) inside the gzip; we
// stream-decode rather than buffer the whole archive (~80MB compressed,
// ~600MB uncompressed) into memory.
type gharchiveEvent struct {
	Type string `json:"type"`
	Repo struct {
		Name string `json:"name"`
	} `json:"repo"`
}

// consume reads the gzipped archive, filters events to the configured
// type set, aggregates per-repo counts into the in-memory ring under
// the matching hour-bucket, and returns the per-repo snapshot for the
// rollup store. Aggregation is idempotent on (repo, hour-bucket): the
// ring overwrites the matching slot when called twice for the same
// archive, so reprocessing yields the same final state.
//
// Decode runs without the source lock held — ~80 MB compressed
// archives take seconds to decode, and we don't want to block readers
// like TopActiveRepos that whole time. The bucket update + slide
// happens under the write lock at the end.
func (s *GHArchiveSource) consume(ctx context.Context, hour time.Time, body io.Reader) (map[string]int64, int64, []GHArchiveHourAggregate, error) {
	gz, err := gzip.NewReader(body)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	// Wrap the gzip stream in a 1 MiB buffered reader: ~600 MB
	// uncompressed per archive, so larger reads cut syscall overhead.
	br := bufio.NewReaderSize(gz, 1<<20)
	// Scan one NDJSON line at a time. A per-line bufio.Scanner is
	// safer than json.Decoder here: json.Decoder keeps internal state,
	// so a single malformed token mid-archive can silently drop the
	// rest of the stream while only one event is counted as discarded.
	// Scanner + json.Unmarshal makes each line independent.
	scanner := bufio.NewScanner(br)
	// Default 64 KiB token cap is too small for some PullRequestEvent
	// payloads (commit lists routinely run a few hundred KiB). Allow
	// growth up to 8 MiB per line; anything larger is treated as a
	// poison record (see scanner.Err handling below).
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	hourBucket := hour.Truncate(time.Hour).UTC()

	thisArchive := make(map[string]int) // repo -> events kept this archive
	thisArchiveTypes := make(map[string]map[string]int)
	// keptByType is the flat per-event-type tally across all repos in
	// this archive, surfaced through OnEventsProcessed so Story 5
	// observability ([ISI-955](/ISI/issues/ISI-955)) can attribute the
	// counter without re-scanning the firehose. Total kept = sum of
	// keptByType values.
	keptByType := make(map[string]int64, len(s.eventTypes))

	var discarded int64
	for {
		select {
		case <-ctx.Done():
			return keptByType, discarded, nil, ctx.Err()
		default:
		}

		if !scanner.Scan() {
			break
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var evt gharchiveEvent
		if err := json.Unmarshal(line, &evt); err != nil {
			// A single malformed line is independent of the rest:
			// count it as discarded and move on.
			discarded++
			continue
		}

		if evt.Repo.Name == "" || !s.eventTypes[evt.Type] {
			discarded++
			continue
		}

		thisArchive[evt.Repo.Name]++
		typeMap := thisArchiveTypes[evt.Repo.Name]
		if typeMap == nil {
			typeMap = make(map[string]int, len(s.eventTypes))
			thisArchiveTypes[evt.Repo.Name] = typeMap
		}
		typeMap[evt.Type]++
		keptByType[evt.Type]++
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			logging.Warn("gharchive_source: dropping archive tail past oversized line",
				"hour", hour, "discarded", discarded+1)
			discarded++
		} else {
			return keptByType, discarded, nil, fmt.Errorf("scan archive: %w", err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	aggregates := make([]GHArchiveHourAggregate, 0, len(thisArchive))
	for repo, count := range thisArchive {
		bucket, ok := s.buckets[repo]
		if !ok {
			bucket = newRingBucket(hourBucket, int(s.cfg.Window/time.Hour))
			s.buckets[repo] = bucket
		}
		bucket.set(hourBucket, count, thisArchiveTypes[repo])

		aggregates = append(aggregates, GHArchiveHourAggregate{
			RepoName:    repo,
			HourBucket:  hourBucket,
			EventCount:  count,
			PerEventTyp: cloneTypeMap(thisArchiveTypes[repo]),
		})
	}

	// Slide every bucket forward to the new wall-clock edge, even
	// repos with no events this archive. Without this, a repo that
	// went cold would keep reporting its last-seen counts forever
	// because its private hourEnd would never advance.
	newRightEdge := hourBucket.Add(time.Hour)
	for _, bucket := range s.buckets {
		bucket.slideTo(newRightEdge)
	}

	// Drop bucket entries that have no events anywhere in the window
	// after rotation, so memory stays bounded as repos go cold.
	s.gcEmptyBuckets()

	return keptByType, discarded, aggregates, nil
}

// cloneTypeMap returns a defensive copy so callers can mutate the
// returned aggregate without poisoning the in-memory ring.
func cloneTypeMap(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// gcEmptyBuckets removes ring buckets whose total events in the
// current window have decayed to zero. Bounded-memory invariant: under
// the soak-default knobs from the [ISI-950 plan](/ISI/issues/ISI-950#document-plan)
// (~0.5–2M unique active repos), buckets that drop out of the 24h
// window must be discarded promptly so the resident set stays in the
// 50–200 MB range.
//
// Safe to call only with s.mu held.
func (s *GHArchiveSource) gcEmptyBuckets() {
	for repo, b := range s.buckets {
		if b.total() == 0 {
			delete(s.buckets, repo)
		}
	}
}

// GHArchiveRepoActivity is one repo's activity snapshot over the
// current sliding window.
type GHArchiveRepoActivity struct {
	RepoName       string
	TotalEvents    int
	HourCounts     []int          // length = window in hours, oldest first
	PerEventType   map[string]int // aggregated across the window
	PeakHourEvents int
}

// TopActiveRepos returns at most n repos sorted by total events over
// the sliding window, descending. Only repos with at least
// minEventsTotal across the window are included. Used by Story 2 to
// pick top-N candidates for promotion to the discovery known set.
//
// Safe under concurrent ProcessArchive: holds the read lock for the
// duration of the snapshot copy.
func (s *GHArchiveSource) TopActiveRepos(n int, minEventsTotal int) []GHArchiveRepoActivity {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Snapshot all repos meeting the floor. We sort the snapshot
	// rather than maintaining a heap online — N is bounded (Story 2
	// uses N=500) and snapshot frequency is at most once per hour.
	out := make([]GHArchiveRepoActivity, 0, len(s.buckets))
	for repo, bucket := range s.buckets {
		total := bucket.total()
		if total < minEventsTotal {
			continue
		}
		out = append(out, GHArchiveRepoActivity{
			RepoName:       repo,
			TotalEvents:    total,
			HourCounts:     bucket.hourCountsOldestFirst(),
			PerEventType:   bucket.perTypeAggregate(),
			PeakHourEvents: bucket.peak(),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalEvents != out[j].TotalEvents {
			return out[i].TotalEvents > out[j].TotalEvents
		}
		return out[i].RepoName < out[j].RepoName
	})

	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// WindowSize returns the number of hour buckets in the sliding window.
// Exposed for telemetry / Story 2 cap tuning.
func (s *GHArchiveSource) WindowSize() int {
	return int(s.cfg.Window / time.Hour)
}

// TrackedRepoCount returns the number of repos currently held in the
// in-memory ring. Useful as a backpressure signal for Story 4
// ([ISI-954](/ISI/issues/ISI-954)).
func (s *GHArchiveSource) TrackedRepoCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.buckets)
}

// ringBucket holds windowSize hourly counters for a single repo. The
// ring is right-open: counts[len-1] is the most recent hour, counts[0]
// is the oldest. We rotate on every set() to keep the indexing trivial
// at read time (the hot path is TopActiveRepos / total()).
type ringBucket struct {
	// hourEnd is the (exclusive) right edge of the window — i.e. the
	// hour AFTER the most recent counted hour. Lets us compute
	// "how many hours ago was X" without negative indexing.
	hourEnd time.Time

	counts  []int
	perType []map[string]int
}

// newRingBucket constructs a ring sized to windowHours with the given
// initial hour as the most-recent slot.
func newRingBucket(initialHour time.Time, windowHours int) *ringBucket {
	if windowHours < 1 {
		windowHours = 1
	}
	b := &ringBucket{
		hourEnd: initialHour.Add(time.Hour),
		counts:  make([]int, windowHours),
		perType: make([]map[string]int, windowHours),
	}
	return b
}

// set records the per-event-type counts for one (repo, hour-bucket)
// cell. Rotates the ring forward as needed if hourBucket is at or past
// the bucket's current right edge. Idempotent for the same hourBucket:
// re-calls overwrite the slot rather than accumulate.
func (b *ringBucket) set(hourBucket time.Time, count int, perType map[string]int) {
	hourBucket = hourBucket.Truncate(time.Hour).UTC()
	rightEdge := b.hourEnd

	if !hourBucket.Before(rightEdge) {
		// hourBucket is at or after the current right edge — rotate
		// forward by (hourBucket - mostRecent) hours so the rightmost
		// slot represents hourBucket. mostRecent = rightEdge - 1h.
		rotateBy := int(hourBucket.Sub(rightEdge.Add(-time.Hour)) / time.Hour)
		if rotateBy < 1 {
			rotateBy = 1
		}
		b.rotate(rotateBy)
		b.hourEnd = hourBucket.Add(time.Hour)
		idx := len(b.counts) - 1
		b.counts[idx] = count
		b.perType[idx] = cloneTypeMap(perType)
		return
	}

	// hourBucket is somewhere inside the existing window or older.
	// hoursAgo is the offset from mostRecent (slot len-1) backwards.
	hoursAgo := int(rightEdge.Sub(hourBucket.Add(time.Hour)) / time.Hour)
	if hoursAgo < 0 || hoursAgo >= len(b.counts) {
		// Outside the window — silently drop. The cursor would
		// only feed us this hour if Run() asked for it, but a
		// caller passing ProcessArchive() out-of-order would
		// land here.
		return
	}
	idx := len(b.counts) - 1 - hoursAgo
	b.counts[idx] = count
	b.perType[idx] = cloneTypeMap(perType)
}

// slideTo advances the bucket's right edge to newRightEdge by rotating
// in zero-fill slots. No-op when the bucket is already at or past
// newRightEdge. Used to age out cold repos that didn't appear in the
// current archive but whose window should still slide.
func (b *ringBucket) slideTo(newRightEdge time.Time) {
	newRightEdge = newRightEdge.Truncate(time.Hour).UTC()
	if !newRightEdge.After(b.hourEnd) {
		return
	}
	steps := int(newRightEdge.Sub(b.hourEnd) / time.Hour)
	b.rotate(steps)
	b.hourEnd = newRightEdge
}

// rotate advances the ring by `steps` hours, dropping the oldest
// `steps` entries and zero-filling new slots on the right. After
// rotate, the right-most slot is the new "most recent" hour.
func (b *ringBucket) rotate(steps int) {
	if steps <= 0 {
		return
	}
	n := len(b.counts)
	if steps >= n {
		// Window slides entirely past existing data.
		for i := range b.counts {
			b.counts[i] = 0
			b.perType[i] = nil
		}
		return
	}
	// Shift left by `steps`; tail becomes zero.
	copy(b.counts, b.counts[steps:])
	copy(b.perType, b.perType[steps:])
	for i := n - steps; i < n; i++ {
		b.counts[i] = 0
		b.perType[i] = nil
	}
}

// total returns the sum across the window.
func (b *ringBucket) total() int {
	sum := 0
	for _, c := range b.counts {
		sum += c
	}
	return sum
}

// peak returns the largest single-hour count in the window.
func (b *ringBucket) peak() int {
	max := 0
	for _, c := range b.counts {
		if c > max {
			max = c
		}
	}
	return max
}

// hourCountsOldestFirst returns a defensive copy of the count series.
func (b *ringBucket) hourCountsOldestFirst() []int {
	out := make([]int, len(b.counts))
	copy(out, b.counts)
	return out
}

// perTypeAggregate sums per-type counts across the entire window.
// Returns nil when there are no events.
func (b *ringBucket) perTypeAggregate() map[string]int {
	var out map[string]int
	for _, m := range b.perType {
		if m == nil {
			continue
		}
		if out == nil {
			out = make(map[string]int, 4)
		}
		for k, v := range m {
			out[k] += v
		}
	}
	return out
}
