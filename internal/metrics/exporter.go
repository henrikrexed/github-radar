// Package metrics provides OpenTelemetry metrics export for github-radar.
package metrics

import (
	"context"
	"fmt"
	"time"

	"github.com/hrexed/github-radar/internal/logging"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// Version is the application version, set at build time.
var Version = "dev"

// DefaultFlushTimeout is the default timeout for flushing metrics on shutdown.
const DefaultFlushTimeout = 10 * time.Second

// ExporterConfig contains configuration for the metrics exporter.
type ExporterConfig struct {
	// Endpoint is the OTLP HTTP endpoint URL
	Endpoint string

	// Headers are additional HTTP headers (e.g., Authorization)
	Headers map[string]string

	// ServiceName is the service.name resource attribute
	ServiceName string

	// ServiceVersion is the service.version resource attribute
	ServiceVersion string

	// FlushTimeout is the timeout for flushing metrics on shutdown
	FlushTimeout time.Duration

	// DryRun disables actual export when true
	DryRun bool
}

// DefaultExporterConfig returns configuration with sensible defaults.
func DefaultExporterConfig() ExporterConfig {
	return ExporterConfig{
		Endpoint:       "http://localhost:4318",
		ServiceName:    "github-radar",
		ServiceVersion: Version,
		FlushTimeout:   DefaultFlushTimeout,
	}
}

// Exporter manages OpenTelemetry metrics export.
type Exporter struct {
	config        ExporterConfig
	meterProvider *sdkmetric.MeterProvider
	meter         metric.Meter
	shutdownFuncs []func(context.Context) error

	// Instruments
	starsGauge             metric.Int64Gauge
	forksGauge             metric.Int64Gauge
	openIssuesGauge        metric.Int64Gauge
	openPRsGauge           metric.Int64Gauge
	contributorsGauge      metric.Int64Gauge
	growthScoreGauge       metric.Float64Gauge
	normalizedScoreGauge   metric.Float64Gauge
	starVelocityGauge      metric.Float64Gauge
	starAccelerationGauge  metric.Float64Gauge
	prVelocityGauge        metric.Float64Gauge
	issueVelocityGauge     metric.Float64Gauge
	contributorGrowthGauge metric.Float64Gauge

	// GitHub API budget instruments (T5 / ISI-716)
	apiRateLimitGauge     metric.Int64Gauge
	apiRateRemainingGauge metric.Int64Gauge
	apiRateUsedRatioGauge metric.Float64Gauge
	apiRateResetSecsGauge metric.Int64Gauge
	apiCallsCounter       metric.Int64Counter
	refreshTierReposGauge metric.Int64Gauge
	scanDurationHist      metric.Float64Histogram

	// Classification health instruments (ISI-775).
	reposPendingGauge      metric.Int64Gauge
	classificationRunCount metric.Int64Counter
}

// NewExporter creates a new metrics exporter.
func NewExporter(config ExporterConfig) (*Exporter, error) {
	if config.ServiceName == "" {
		config.ServiceName = "github-radar"
	}
	if config.ServiceVersion == "" {
		config.ServiceVersion = Version
	}
	if config.FlushTimeout == 0 {
		config.FlushTimeout = DefaultFlushTimeout
	}

	e := &Exporter{
		config: config,
	}

	if err := e.init(); err != nil {
		return nil, err
	}

	return e, nil
}

// NewExporterForTest constructs an exporter wired to a caller-provided
// reader (typically a sdkmetric.ManualReader). This lets in-process
// tests scrape counters synchronously without standing up an OTLP
// receiver. The exporter is otherwise identical to one built by
// NewExporter — same instruments, same recording semantics.
func NewExporterForTest(reader sdkmetric.Reader, serviceName string) (*Exporter, error) {
	if serviceName == "" {
		serviceName = "github-radar-test"
	}
	e := &Exporter{
		config: ExporterConfig{
			ServiceName:    serviceName,
			ServiceVersion: Version,
			FlushTimeout:   DefaultFlushTimeout,
			DryRun:         true,
		},
	}

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion(Version),
	)
	e.meterProvider = sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(reader),
	)
	e.shutdownFuncs = append(e.shutdownFuncs, e.meterProvider.Shutdown)
	e.meter = e.meterProvider.Meter("github-radar",
		metric.WithInstrumentationVersion(Version),
	)
	if err := e.createInstruments(); err != nil {
		return nil, fmt.Errorf("creating instruments: %w", err)
	}
	return e, nil
}

// init initializes the OpenTelemetry SDK.
func (e *Exporter) init() error {
	ctx := context.Background()

	// Route OTel SDK errors through the app's logging so export
	// failures are visible instead of silently going to stderr.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logging.Error("otel sdk error", "error", err)
	}))

	// Build resource with semantic conventions
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(e.config.ServiceName),
		semconv.ServiceVersion(e.config.ServiceVersion),
	)

	// Create OTLP HTTP exporter options
	var opts []otlpmetrichttp.Option
	if e.config.Endpoint != "" {
		opts = append(opts, otlpmetrichttp.WithEndpointURL(e.config.Endpoint))
	}

	// Add custom headers if provided
	if len(e.config.Headers) > 0 {
		opts = append(opts, otlpmetrichttp.WithHeaders(e.config.Headers))
	}

	// Create exporter (unless dry-run)
	var reader sdkmetric.Reader
	if e.config.DryRun {
		// Use a no-op reader for dry-run mode
		reader = sdkmetric.NewManualReader()
	} else {
		exporter, err := otlpmetrichttp.New(ctx, opts...)
		if err != nil {
			return fmt.Errorf("creating OTLP exporter: %w", err)
		}
		e.shutdownFuncs = append(e.shutdownFuncs, exporter.Shutdown)

		reader = sdkmetric.NewPeriodicReader(exporter,
			sdkmetric.WithInterval(60*time.Second),
		)
	}

	// Create meter provider
	e.meterProvider = sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(reader),
	)
	e.shutdownFuncs = append(e.shutdownFuncs, e.meterProvider.Shutdown)

	// Set global meter provider
	otel.SetMeterProvider(e.meterProvider)

	// Create meter
	e.meter = e.meterProvider.Meter("github-radar",
		metric.WithInstrumentationVersion(e.config.ServiceVersion),
	)

	// Create instruments
	if err := e.createInstruments(); err != nil {
		return fmt.Errorf("creating instruments: %w", err)
	}

	return nil
}

// createInstruments creates all metric instruments.
func (e *Exporter) createInstruments() error {
	var err error

	// Core metrics
	e.starsGauge, err = e.meter.Int64Gauge("github.repo.stars",
		metric.WithDescription("Number of stars for the repository"),
		metric.WithUnit("{stars}"),
	)
	if err != nil {
		return err
	}

	e.forksGauge, err = e.meter.Int64Gauge("github.repo.forks",
		metric.WithDescription("Number of forks for the repository"),
		metric.WithUnit("{forks}"),
	)
	if err != nil {
		return err
	}

	e.openIssuesGauge, err = e.meter.Int64Gauge("github.repo.open_issues",
		metric.WithDescription("Number of open issues for the repository"),
		metric.WithUnit("{issues}"),
	)
	if err != nil {
		return err
	}

	e.openPRsGauge, err = e.meter.Int64Gauge("github.repo.open_prs",
		metric.WithDescription("Number of open pull requests for the repository"),
		metric.WithUnit("{prs}"),
	)
	if err != nil {
		return err
	}

	e.contributorsGauge, err = e.meter.Int64Gauge("github.repo.contributors",
		metric.WithDescription("Number of contributors to the repository"),
		metric.WithUnit("{contributors}"),
	)
	if err != nil {
		return err
	}

	// Scoring metrics
	e.growthScoreGauge, err = e.meter.Float64Gauge("github.repo.growth_score",
		metric.WithDescription("Raw composite growth score"),
		metric.WithUnit("{score}"),
	)
	if err != nil {
		return err
	}

	e.normalizedScoreGauge, err = e.meter.Float64Gauge("github.repo.growth_score_normalized",
		metric.WithDescription("Normalized growth score (0-100)"),
		metric.WithUnit("{score}"),
	)
	if err != nil {
		return err
	}

	// Velocity metrics
	e.starVelocityGauge, err = e.meter.Float64Gauge("github.repo.star_velocity",
		metric.WithDescription("Stars gained per day"),
		metric.WithUnit("{stars}/d"),
	)
	if err != nil {
		return err
	}

	e.starAccelerationGauge, err = e.meter.Float64Gauge("github.repo.star_acceleration",
		metric.WithDescription("Change in star velocity"),
		metric.WithUnit("{stars_per_day_squared}"),
	)
	if err != nil {
		return err
	}

	e.prVelocityGauge, err = e.meter.Float64Gauge("github.repo.pr_velocity",
		metric.WithDescription("PRs merged per day"),
		metric.WithUnit("{prs}/d"),
	)
	if err != nil {
		return err
	}

	e.issueVelocityGauge, err = e.meter.Float64Gauge("github.repo.issue_velocity",
		metric.WithDescription("Issues opened per day"),
		metric.WithUnit("{issues}/d"),
	)
	if err != nil {
		return err
	}

	e.contributorGrowthGauge, err = e.meter.Float64Gauge("github.repo.contributor_growth",
		metric.WithDescription("New contributors per day"),
		metric.WithUnit("{contributors}/d"),
	)
	if err != nil {
		return err
	}

	// GitHub API budget instruments (T5 / ISI-716) -----------------------
	e.apiRateLimitGauge, err = e.meter.Int64Gauge("github.api.rate_limit.limit",
		metric.WithDescription("GitHub API rate limit ceiling from X-RateLimit-Limit"),
		metric.WithUnit("{calls}/h"),
	)
	if err != nil {
		return err
	}

	e.apiRateRemainingGauge, err = e.meter.Int64Gauge("github.api.rate_limit.remaining",
		metric.WithDescription("Remaining API calls in the current window from X-RateLimit-Remaining"),
		metric.WithUnit("{calls}"),
	)
	if err != nil {
		return err
	}

	e.apiRateUsedRatioGauge, err = e.meter.Float64Gauge("github.api.rate_limit.used_ratio",
		metric.WithDescription("Fraction of API budget consumed (0.0 - 1.0). Alert at ≥ 0.9."),
		metric.WithUnit("{ratio}"),
	)
	if err != nil {
		return err
	}

	e.apiRateResetSecsGauge, err = e.meter.Int64Gauge("github.api.rate_limit.reset_seconds",
		metric.WithDescription("Seconds until the rate limit window resets"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}

	e.apiCallsCounter, err = e.meter.Int64Counter("github.api.calls_total",
		metric.WithDescription("GitHub API calls issued, tagged by resource and result"),
		metric.WithUnit("{calls}"),
	)
	if err != nil {
		return err
	}

	e.refreshTierReposGauge, err = e.meter.Int64Gauge("github.api.refresh_tier.repos",
		metric.WithDescription("Repo count currently assigned to each refresh tier"),
		metric.WithUnit("{repos}"),
	)
	if err != nil {
		return err
	}

	// Scan-cycle wallclock distribution. Used by the T5 prod canary
	// decision tree (ISI-792 / ISI-716): "p95 latency >2× baseline" is
	// one of the halt criteria, and that requires a histogram, not just
	// per-cycle slog lines. Attribute "path" is one of {legacy, bulk,
	// canary} so dashboards can split-screen the two paths during the
	// canary stages.
	e.scanDurationHist, err = e.meter.Float64Histogram("github.scan.duration",
		metric.WithDescription("Wallclock duration of a single scan cycle"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}

	// Classification health instruments (ISI-775). Surfaces pending-status
	// repos and classification-run outcomes so a future SQL Scan / pipeline
	// regression shows up within one cycle instead of going silent.
	e.reposPendingGauge, err = e.meter.Int64Gauge("radar.repos.pending",
		metric.WithDescription("Number of repos in status='pending', tagged by excluded and force_category_set"),
		metric.WithUnit("{repos}"),
	)
	if err != nil {
		return err
	}

	e.classificationRunCount, err = e.meter.Int64Counter("radar.classification.run",
		metric.WithDescription("Classification cycles completed, tagged by result (success|failed|partial)"),
		metric.WithUnit("{runs}"),
	)
	if err != nil {
		return err
	}

	return nil
}

// RepoMetrics contains metrics to record for a repository.
type RepoMetrics struct {
	Owner      string
	Name       string
	Language   string
	Categories []string

	// v3 taxonomy attributes (ISI-714 / ISI-786). Subcategory is the v3
	// 2nd-level token. CategoryLegacy is the pre-migration flat slug
	// (e.g. "ai-agents") emitted for the time-boxed 30-day backward-compat
	// window. Both are emitted unconditionally — even when empty — so the
	// dashboard sees a stable attribute shape across rows instead of
	// silently-dropped dimensions.
	Subcategory    string
	CategoryLegacy string

	Stars        int
	Forks        int
	OpenIssues   int
	OpenPRs      int
	Contributors int

	GrowthScore       float64
	NormalizedScore   float64
	StarVelocity      float64
	StarAcceleration  float64
	PRVelocity        float64
	IssueVelocity     float64
	ContributorGrowth float64
}

// attributes builds the OTel attribute set for a RepoMetrics row. Extracted
// from RecordRepoMetrics so the attribute shape (especially the v3 subcategory
// + category_legacy emission added in ISI-786) can be asserted directly in
// unit tests without standing up the full meter provider pipeline.
func (m RepoMetrics) attributes() []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("repo_owner", m.Owner),
		attribute.String("repo_name", m.Name),
		attribute.String("repo_full_name", m.Owner+"/"+m.Name),
	}

	if m.Language != "" {
		attrs = append(attrs, attribute.String("language", m.Language))
	}

	// Add categories as a comma-separated string.
	if len(m.Categories) > 0 {
		categoryStr := ""
		for i, cat := range m.Categories {
			if i > 0 {
				categoryStr += ","
			}
			categoryStr += cat
		}
		attrs = append(attrs, attribute.String("category", categoryStr))
	}

	// ISI-714 / ISI-786: emit v3 subcategory + legacy attributes unconditionally
	// so every github.radar.* series carries a stable attribute shape during the
	// 30-day backward-compat window. Gating these on non-empty would silently
	// drop the dimension on rows that haven't been re-classified yet — the exact
	// regression Observability Agent surfaced on the dev tenant.
	attrs = append(attrs,
		attribute.String("subcategory", m.Subcategory),
		attribute.String("category_legacy", m.CategoryLegacy),
	)

	return attrs
}

// RecordRepoMetrics records all metrics for a repository.
func (e *Exporter) RecordRepoMetrics(ctx context.Context, m RepoMetrics) {
	attrSet := metric.WithAttributes(m.attributes()...)

	// Record core metrics
	e.starsGauge.Record(ctx, int64(m.Stars), attrSet)
	e.forksGauge.Record(ctx, int64(m.Forks), attrSet)
	e.openIssuesGauge.Record(ctx, int64(m.OpenIssues), attrSet)
	e.openPRsGauge.Record(ctx, int64(m.OpenPRs), attrSet)
	e.contributorsGauge.Record(ctx, int64(m.Contributors), attrSet)

	// Record scoring metrics
	e.growthScoreGauge.Record(ctx, m.GrowthScore, attrSet)
	e.normalizedScoreGauge.Record(ctx, m.NormalizedScore, attrSet)

	// Record velocity metrics
	e.starVelocityGauge.Record(ctx, m.StarVelocity, attrSet)
	e.starAccelerationGauge.Record(ctx, m.StarAcceleration, attrSet)
	e.prVelocityGauge.Record(ctx, m.PRVelocity, attrSet)
	e.issueVelocityGauge.Record(ctx, m.IssueVelocity, attrSet)
	e.contributorGrowthGauge.Record(ctx, m.ContributorGrowth, attrSet)
}

// RateLimitSnapshot carries the inputs needed to populate the GitHub API
// budget gauges. Remaining/Limit come from `X-RateLimit-*` headers;
// ResetAt is the absolute reset timestamp.
type RateLimitSnapshot struct {
	Limit     int
	Remaining int
	ResetAt   time.Time
}

// RecordRateLimit emits all four API-budget gauges. Safe to call from
// any goroutine; a zero Limit is treated as "no data yet" and we skip
// the used_ratio gauge to avoid a misleading 1.0 reading.
func (e *Exporter) RecordRateLimit(ctx context.Context, snap RateLimitSnapshot) {
	e.apiRateLimitGauge.Record(ctx, int64(snap.Limit))
	e.apiRateRemainingGauge.Record(ctx, int64(snap.Remaining))

	if snap.Limit > 0 {
		used := 1.0 - float64(snap.Remaining)/float64(snap.Limit)
		if used < 0 {
			used = 0
		}
		e.apiRateUsedRatioGauge.Record(ctx, used)
	}

	if !snap.ResetAt.IsZero() {
		secs := int64(time.Until(snap.ResetAt).Seconds())
		if secs < 0 {
			secs = 0
		}
		e.apiRateResetSecsGauge.Record(ctx, secs)
	}
}

// RecordAPICall increments the API call counter. `resource` should be
// one of "repo", "graphql", "search", "activity", "readme"; `result` is
// "ok" (2xx non-304), "not_modified" (304), "error", or "rate_limited".
func (e *Exporter) RecordAPICall(ctx context.Context, resource, result string) {
	e.apiCallsCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("resource", resource),
		attribute.String("result", result),
	))
}

// RecordRefreshTierHistogram emits one gauge reading per tier bucket.
func (e *Exporter) RecordRefreshTierHistogram(ctx context.Context, counts map[string]int) {
	for tier, n := range counts {
		e.refreshTierReposGauge.Record(ctx, int64(n), metric.WithAttributes(
			attribute.String("tier", tier),
		))
	}
}

// RecordScanDuration records one observation of the scan-cycle wallclock
// histogram. `path` is one of "legacy" (pre-T5), "bulk" (T5 full rollout),
// or "canary" (T5 partial rollout for ISI-792 staged prod canary). It is
// safe to call with a nil exporter receiver — the daemon checks for that
// before calling, but defensively we no-op if instruments aren't built.
func (e *Exporter) RecordScanDuration(ctx context.Context, d time.Duration, path string) {
	if e == nil || e.scanDurationHist == nil {
		return
	}
	e.scanDurationHist.Record(ctx, d.Seconds(), metric.WithAttributes(
		attribute.String("path", path),
	))
}

// PendingBucket is one (excluded, force_category_set, count) tuple for the
// `radar.repos.pending` gauge. Mirrors database.PendingBreakdown but lives in
// the metrics package so the daemon doesn't have to leak DB types into its
// metric-recording path.
type PendingBucket struct {
	Excluded         bool
	ForceCategorySet bool
	Count            int
}

// RecordPendingBuckets records the `radar.repos.pending` gauge for each
// (excluded, force_category_set) bucket. Always emit all four buckets — even
// the zero ones — so dashboards see a stable shape and missing dimensions are
// obvious instead of being silently dropped from queries.
func (e *Exporter) RecordPendingBuckets(ctx context.Context, buckets []PendingBucket) {
	for _, b := range buckets {
		e.reposPendingGauge.Record(ctx, int64(b.Count),
			metric.WithAttributes(
				attribute.Bool("excluded", b.Excluded),
				attribute.Bool("force_category_set", b.ForceCategorySet),
			),
		)
	}
}

// ClassificationRunResult enumerates the outcome attribute on
// `radar.classification.run`. Kept in the metrics package so callers don't
// pass arbitrary strings.
type ClassificationRunResult string

const (
	// ClassificationRunSuccess: ClassifyAll returned no error and Summary.Failed==0.
	ClassificationRunSuccess ClassificationRunResult = "success"
	// ClassificationRunFailed: ClassifyAll itself returned an error (e.g. SQL
	// Scan column-count drift, DB connection lost) — no rows were classified.
	ClassificationRunFailed ClassificationRunResult = "failed"
	// ClassificationRunPartial: ClassifyAll returned no top-level error but
	// Summary.Failed>0 (per-repo failures during the batch).
	ClassificationRunPartial ClassificationRunResult = "partial"
)

// RecordClassificationRun increments the classification-run counter with the
// given outcome attribute. Called once per `runClassification` cycle.
func (e *Exporter) RecordClassificationRun(ctx context.Context, result ClassificationRunResult) {
	e.classificationRunCount.Add(ctx, 1,
		metric.WithAttributes(attribute.String("result", string(result))),
	)
}

// Flush forces an immediate export of all pending metrics.
func (e *Exporter) Flush(ctx context.Context) error {
	if e.config.DryRun {
		return nil
	}
	return e.meterProvider.ForceFlush(ctx)
}

// Shutdown gracefully shuts down the exporter, flushing all pending metrics.
func (e *Exporter) Shutdown(ctx context.Context) error {
	var errs []error

	// Shutdown in reverse order
	for i := len(e.shutdownFuncs) - 1; i >= 0; i-- {
		if err := e.shutdownFuncs[i](ctx); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}
	return nil
}

// ShutdownWithTimeout shuts down with the configured timeout.
func (e *Exporter) ShutdownWithTimeout() error {
	ctx, cancel := context.WithTimeout(context.Background(), e.config.FlushTimeout)
	defer cancel()
	return e.Shutdown(ctx)
}

// IsDryRun returns true if the exporter is in dry-run mode.
func (e *Exporter) IsDryRun() bool {
	return e.config.DryRun
}

// Meter returns the underlying OTel meter for custom instrumentation.
func (e *Exporter) Meter() metric.Meter {
	return e.meter
}
