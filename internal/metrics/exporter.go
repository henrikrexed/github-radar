// Package metrics provides OpenTelemetry metrics export for github-radar.
package metrics

import (
	"context"
	"fmt"
	"time"

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

// init initializes the OpenTelemetry SDK.
func (e *Exporter) init() error {
	ctx := context.Background()

	// Build resource with semantic conventions
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(e.config.ServiceName),
		semconv.ServiceVersion(e.config.ServiceVersion),
	)

	// Create OTLP HTTP exporter options
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpointURL(e.config.Endpoint),
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
		metric.WithUnit("{stars}/d²"),
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

	return nil
}

// RepoMetrics contains metrics to record for a repository.
type RepoMetrics struct {
	Owner      string
	Name       string
	Language   string
	Categories []string

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

// RecordRepoMetrics records all metrics for a repository.
func (e *Exporter) RecordRepoMetrics(ctx context.Context, m RepoMetrics) {
	// Build common attributes
	attrs := []attribute.KeyValue{
		attribute.String("repo_owner", m.Owner),
		attribute.String("repo_name", m.Name),
		attribute.String("repo_full_name", m.Owner+"/"+m.Name),
	}

	if m.Language != "" {
		attrs = append(attrs, attribute.String("language", m.Language))
	}

	// Add categories as a comma-separated string
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

	attrSet := metric.WithAttributes(attrs...)

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
