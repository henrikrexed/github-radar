// Package daemon provides the background scanner daemon for github-radar.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hrexed/github-radar/internal/classification"
	"github.com/hrexed/github-radar/internal/config"
	"github.com/hrexed/github-radar/internal/database"
	"github.com/hrexed/github-radar/internal/discovery"
	"github.com/hrexed/github-radar/internal/github"
	"github.com/hrexed/github-radar/internal/logging"
	"github.com/hrexed/github-radar/internal/metrics"
	"github.com/hrexed/github-radar/internal/scoring"
	"github.com/hrexed/github-radar/internal/state"
)

// Status represents the current daemon status.
type Status string

const (
	StatusIdle     Status = "idle"
	StatusScanning Status = "scanning"
	StatusStarting Status = "starting"
	StatusStopping Status = "stopping"
)

// DaemonConfig contains daemon configuration.
type DaemonConfig struct {
	// Interval between scans (e.g., "24h", "6h")
	Interval time.Duration

	// HTTPAddr is the address for the status endpoint (e.g., ":8080")
	HTTPAddr string

	// ConfigPath is the path to the config file for reload
	ConfigPath string

	// StatePath is the path to the state file
	StatePath string

	// DryRun disables metrics export
	DryRun bool
}

// DefaultDaemonConfig returns default daemon configuration.
func DefaultDaemonConfig() DaemonConfig {
	return DaemonConfig{
		Interval:  24 * time.Hour,
		HTTPAddr:  ":8080",
		StatePath: state.DefaultStatePath,
	}
}

// Daemon manages the background scanner service.
type Daemon struct {
	cfg        *config.Config
	daemonCfg  DaemonConfig
	client     *github.Client
	scanner    *github.Scanner
	discoverer *discovery.Discoverer
	classifier *classification.Pipeline
	exporter   *metrics.Exporter
	store      *state.Store
	db         *database.DB
	server     *http.Server

	mu              sync.RWMutex
	status          Status
	lastScan        time.Time
	nextScan        time.Time
	reposTracked    int
	rateLimitRemain int
	startTime       time.Time // instance start time for uptime tracking
	ready           bool      // true when daemon is fully initialized

	// classificationLastErr captures the most recent ClassifyAll error so the
	// cycle-summary log line can surface it without forcing operators to grep
	// scrollback. Empty when the last run succeeded. Guarded by mu (ISI-775).
	classificationLastErr string

	ctx        context.Context
	cancel     context.CancelFunc
	scanMu     sync.Mutex // prevents overlapping scans
	reloadChan chan os.Signal
}

// New creates a new daemon instance.
func New(cfg *config.Config, daemonCfg DaemonConfig) (*Daemon, error) {
	// Create GitHub client
	client, err := github.NewClient(cfg.GitHub.Token)
	if err != nil {
		return nil, fmt.Errorf("creating github client: %w", err)
	}

	// Set up rate limit options
	client.SetRateLimitOptions(github.RateLimitOptions{
		Threshold: cfg.GitHub.RateLimit,
		OnWarning: func(remaining int, reset time.Time) {
			logging.Warn("rate limit warning",
				"remaining", remaining,
				"reset", reset.Format(time.RFC3339))
		},
	})

	// Create state store
	store := state.NewStore(daemonCfg.StatePath)
	if err := store.Load(); err != nil {
		logging.Warn("could not load state file, starting fresh", "error", err)
	}

	// Create scanner
	scanner := github.NewScanner(client, store)
	scanner.SetScoringWeights(scoring.Weights{
		StarVelocity:      cfg.Scoring.Weights.StarVelocity,
		StarAcceleration:  cfg.Scoring.Weights.StarAcceleration,
		ForkVelocity:      cfg.Scoring.Weights.ForkVelocity,
		ReleaseCadence:    cfg.Scoring.Weights.ReleaseCadence,
		ContributorGrowth: cfg.Scoring.Weights.ContributorGrowth,
		PRVelocity:        cfg.Scoring.Weights.PRVelocity,
		IssueVelocity:     cfg.Scoring.Weights.IssueVelocity,
	})
	scanner.SetLogger(func(level, msg string, args ...interface{}) {
		logWithLevel(level, msg, args...)
	})

	// Create discoverer if enabled
	var disc *discovery.Discoverer
	if cfg.Discovery.Enabled && len(cfg.Discovery.Topics) > 0 {
		discCfg := discovery.Config{
			Topics:             cfg.Discovery.Topics,
			MinStars:           cfg.Discovery.MinStars,
			MaxAgeDays:         cfg.Discovery.MaxAgeDays,
			AutoTrackThreshold: cfg.Discovery.AutoTrackThreshold,
			Exclusions:         cfg.Exclusions,
			Sources: discovery.SourcesConfig{
				Orgs: discovery.OrgsSourceConfig{
					Enabled:  cfg.Discovery.Sources.Orgs.Enabled,
					Names:    cfg.Discovery.Sources.Orgs.Names,
					MinStars: cfg.Discovery.Sources.Orgs.MinStars,
				},
				Languages: discovery.LanguagesSourceConfig{
					Enabled:         cfg.Discovery.Sources.Languages.Enabled,
					Names:           cfg.Discovery.Sources.Languages.Names,
					MinStars:        cfg.Discovery.Sources.Languages.MinStars,
					PushWindowsDays: cfg.Discovery.Sources.Languages.PushWindowsDays,
				},
			},
		}
		disc = discovery.NewDiscoverer(client, store, discCfg)
		disc.SetLogger(func(level, msg string, args ...interface{}) {
			logWithLevel(level, msg, args...)
		})
	}

	// Create metrics exporter
	var exp *metrics.Exporter
	if !daemonCfg.DryRun {
		var flushTimeout time.Duration
		if cfg.Otel.FlushTimeout > 0 {
			flushTimeout = time.Duration(cfg.Otel.FlushTimeout) * time.Second
		}
		exporterCfg := metrics.ExporterConfig{
			Endpoint:       cfg.Otel.Endpoint,
			Headers:        cfg.Otel.Headers,
			ServiceName:    cfg.Otel.ServiceName,
			ServiceVersion: cfg.Otel.ServiceVersion,
			FlushTimeout:   flushTimeout,
		}
		exp, err = metrics.NewExporter(exporterCfg)
		if err != nil {
			return nil, fmt.Errorf("creating metrics exporter: %w", err)
		}
		endpoint := cfg.Otel.Endpoint
		if endpoint == "" {
			endpoint = "(from OTEL_EXPORTER_OTLP_ENDPOINT env var or default)"
		}
		logging.Info("metrics exporter created", "endpoint", endpoint, "service_name", cfg.Otel.ServiceName)
	}

	// Open the database so classification, metric-export category
	// resolution, and the T5 refresh-tier classifier can read
	// FirstSeenAt. All three features degrade gracefully when the DB
	// cannot be opened.
	var classifyDB *database.DB
	var classifyPipeline *classification.Pipeline
	classifyDB, err = database.Open("")
	if err != nil {
		logging.Warn("could not open database, classification, DB-based categories, and refresh-tier first-seen disabled", "error", err)
		classifyDB = nil
	} else if cfg.Classification.OllamaEndpoint != "" && cfg.Classification.Model != "" {
		clsCfg := cfg.Classification
		ollama := classification.NewOllamaClient(
			clsCfg.OllamaEndpoint,
			clsCfg.Model,
			clsCfg.TimeoutMs,
			clsCfg.Categories,
		)
		classifyPipeline = classification.NewPipeline(classifyDB, client, ollama, clsCfg)
		logging.Info("classification enabled",
			"model", clsCfg.Model,
			"endpoint", clsCfg.OllamaEndpoint)
	}

	ctx, cancel := context.WithCancel(context.Background())

	d := &Daemon{
		cfg:        cfg,
		daemonCfg:  daemonCfg,
		client:     client,
		scanner:    scanner,
		discoverer: disc,
		classifier: classifyPipeline,
		exporter:   exp,
		store:      store,
		db:         classifyDB,
		status:     StatusIdle,
		startTime:  time.Now(),
		ready:      false,
		ctx:        ctx,
		cancel:     cancel,
		reloadChan: make(chan os.Signal, 1),
	}

	// Install API telemetry observer so every HTTP round-trip emits
	// OTel counters and the rate-limit gauge stays fresh (T5 / ISI-716).
	if exp != nil {
		client.SetAPIObserver(newAPIObserver(ctx, exp))
	}

	// Set up HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/health", d.handleHealth)
	mux.HandleFunc("/status", d.handleStatus)

	d.server = &http.Server{
		Addr:              daemonCfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return d, nil
}

// Run starts the daemon and blocks until shutdown.
func (d *Daemon) Run() error {
	d.setStatus(StatusStarting)
	logging.Info("daemon starting",
		"http_addr", d.daemonCfg.HTTPAddr,
		"interval", d.daemonCfg.Interval.String(),
		"repos", len(d.cfg.Repositories),
		"dry_run", d.daemonCfg.DryRun)

	// Start HTTP server
	go func() {
		if err := d.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.Error("http server error", "error", err)
		}
	}()

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	signal.Notify(d.reloadChan, syscall.SIGHUP)

	// Calculate next scan time
	d.scheduleNextScan()

	d.setStatus(StatusIdle)
	d.setReady(true)
	logging.Info("daemon ready", "next_scan", d.nextScan.Format(time.RFC3339))

	// Main loop with immediate first scan check
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Check immediately if first scan should run (don't wait for ticker)
	if time.Now().After(d.nextScan) {
		d.runScan()
	}

	for {
		select {
		case <-d.ctx.Done():
			return d.shutdown()

		case sig := <-sigChan:
			logging.Info("received signal", "signal", sig.String())
			return d.shutdown()

		case <-d.reloadChan:
			logging.Info("received SIGHUP, reloading config")
			d.reloadConfig()

		case <-ticker.C:
			// Check if it's time to scan
			if time.Now().After(d.nextScan) {
				d.runScan()
			}
		}
	}
}

// Stop gracefully stops the daemon.
func (d *Daemon) Stop() {
	d.cancel()
}

// shutdown performs graceful shutdown.
func (d *Daemon) shutdown() error {
	d.setStatus(StatusStopping)
	logging.Info("daemon shutting down")

	// Stop HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := d.server.Shutdown(ctx); err != nil {
		logging.Warn("http server shutdown error", "error", err)
	}

	// Flush metrics
	if d.exporter != nil {
		if err := d.exporter.Flush(ctx); err != nil {
			logging.Warn("metrics flush error", "error", err)
		}
		if err := d.exporter.Shutdown(ctx); err != nil {
			logging.Warn("metrics shutdown error", "error", err)
		}
	}

	// Save state
	if err := d.store.Save(); err != nil {
		logging.Warn("state save error", "error", err)
	}

	// ISI-773 regression guard: surface the needs_reclassify backlog on the
	// daemon's final log line so operators see drift the moment the scanner
	// stops. A non-zero number larger than ~50 for >24h is the early signal
	// that some bulk MarkAllNeedsReclassify-style operation needs a drain
	// pass (`github-radar admin drain-needs-reclassify`).
	needsReclassifyTotal := -1
	if d.db != nil {
		if n, err := d.db.NeedsReclassifyCount(); err == nil {
			needsReclassifyTotal = n
		} else {
			logging.Warn("needs_reclassify count failed", "error", err)
		}
	}

	logging.Info("daemon stopped",
		"repos_needs_reclassify_total", needsReclassifyTotal,
	)

	// ISI-775: emit the cycle-summary line on shutdown so the very last log
	// entry shows the full health snapshot (pending count + last
	// classification error). `journalctl -u github-radar | tail -1` should
	// make a silent failure obvious. Intentionally also called at the end of
	// the last runScan() so a normal shutdown emits two summary lines (the
	// run-end one and this one); the redundancy is cheap and shutdown is
	// rare.
	d.logCycleSummary()

	return nil
}

// runScan executes a collection and discovery scan.
func (d *Daemon) runScan() {
	// Prevent overlapping scans
	if !d.scanMu.TryLock() {
		logging.Debug("scan already in progress, skipping")
		return
	}
	defer d.scanMu.Unlock()

	d.setStatus(StatusScanning)
	defer d.setStatus(StatusIdle)

	scanStartTime := time.Now()
	logging.Info("starting scheduled scan")

	// Build repo list from config (hold lock while reading config)
	d.mu.RLock()
	repositories := make([]config.TrackedRepo, len(d.cfg.Repositories))
	copy(repositories, d.cfg.Repositories)
	exclusions := make([]string, len(d.cfg.Exclusions))
	copy(exclusions, d.cfg.Exclusions)
	d.mu.RUnlock()

	repos := make([]github.Repo, 0, len(repositories))
	seen := make(map[string]bool, len(repositories))
	for _, tracked := range repositories {
		// Skip excluded repos
		if isExcluded(tracked.Repo, exclusions) {
			continue
		}
		parts := strings.SplitN(tracked.Repo, "/", 2)
		if len(parts) == 2 {
			repos = append(repos, github.Repo{Owner: parts[0], Name: parts[1]})
			seen[tracked.Repo] = true
		}
	}

	// Also include auto-discovered repos from state store
	for fullName := range d.store.AllRepoStates() {
		if seen[fullName] {
			continue
		}
		if isExcluded(fullName, exclusions) {
			continue
		}
		parts := strings.SplitN(fullName, "/", 2)
		if len(parts) == 2 {
			repos = append(repos, github.Repo{Owner: parts[0], Name: parts[1]})
			seen[fullName] = true
		}
	}

	// Run scan — three paths:
	//   - canary: bulk_fetch_enabled=true AND bulk_fetch_canary_full_names is
	//     non-empty → listed repos go through the tiered/bulk path, the rest
	//     stay on the legacy REST single-pass path. Used for staged rollout
	//     (Stage A/B of the T5 prod canary, ISI-716/ISI-792).
	//   - bulk: bulk_fetch_enabled=true AND list empty → all repos on the
	//     tiered/bulk path (Stage C / steady state).
	//   - legacy: bulk_fetch_enabled=false → pre-T5 single-pass REST.
	var result *github.ScanResult
	var err error
	switch {
	case d.cfg.GitHub.BulkFetchEnabled && len(d.cfg.GitHub.BulkFetchCanaryFullNames) > 0:
		result, err = d.runCanaryScan(repos)
	case d.cfg.GitHub.BulkFetchEnabled:
		result, err = d.runTieredScan(repos)
	default:
		result, err = d.scanner.Scan(d.ctx, repos)
	}
	if err != nil && err != context.Canceled {
		logging.Error("scan failed", "error", err)
	}

	if result != nil {
		// Normalize scores after scan
		d.scanner.NormalizeAllScores()

		// Export metrics if not dry run
		if d.exporter != nil {
			d.exportMetrics()
		}

		// Record cycle wallclock as a histogram. PM's T5 canary decision
		// tree (ISI-792) gates on scan p95 latency >2× baseline; logging
		// alone is not sufficient.
		scanDur := result.EndTime.Sub(result.StartTime)
		if d.exporter != nil {
			d.exporter.RecordScanDuration(d.ctx, scanDur, scanPathLabel(d.cfg.GitHub))
		}

		logging.Info("scan complete",
			"total", result.Total,
			"successful", result.Successful,
			"failed", result.Failed,
			"skipped", result.Skipped,
			"duration", scanDur)
	}

	// Run discovery if enabled
	if d.discoverer != nil {
		d.runDiscovery()
	}

	// Sync in-memory store to database so classification and metric export can find repos
	d.syncStoreToDatabase()

	// Run classification if enabled
	if d.classifier != nil {
		d.runClassification()
	}

	// Export metrics for all repos (including auto-tracked from discovery)
	if d.exporter != nil {
		d.exportMetrics()
	}

	// Update status info — count all repos in state (includes config + discovered)
	d.mu.Lock()
	d.lastScan = scanStartTime
	d.reposTracked = len(d.store.AllRepoStates())
	d.rateLimitRemain = d.client.RateLimitInfo().Remaining
	d.mu.Unlock()

	// ISI-775: structured cycle-summary log line. The goal is that
	// `journalctl -u github-radar | tail -1` makes a silent classification
	// failure obvious — every counter on the row, last error included.
	d.logCycleSummary()

	// Schedule next scan
	d.scheduleNextScan()
}

// logCycleSummary emits a single structured INFO line at the end of every
// scan cycle (and on shutdown via the same fields) covering all repo-status
// counters plus the most recent classification error. See ISI-775.
//
// The intent: a silent failure (e.g. SQL Scan column-count drift like the
// 26-hour incident from ISI-714) should surface within one cycle from this
// line alone, without forcing the operator to grep scrollback. We deliberately
// emit -1 sentinels when the DB is unavailable so a missing value isn't
// confused with a true zero.
func (d *Daemon) logCycleSummary() {
	reposTotal := -1
	reposActive := -1
	reposPending := -1
	reposNeedsReclassify := -1
	reposNeedsReview := -1
	lastClassifiedAt := ""

	if d.db != nil {
		if statuses, err := d.db.CountByStatus(); err == nil {
			total := 0
			for _, n := range statuses {
				total += n
			}
			reposTotal = total
			reposActive = statuses["active"]
			reposPending = statuses["pending"]
			reposNeedsReclassify = statuses["needs_reclassify"]
			reposNeedsReview = statuses["needs_review"]
		} else {
			logging.Warn("cycle summary status count failed", "error", err)
		}
		if ts, err := d.db.LastClassifiedAt(); err == nil {
			lastClassifiedAt = ts
		} else {
			logging.Warn("cycle summary last_classified_at failed", "error", err)
		}
	}

	d.mu.RLock()
	classificationLastErr := d.classificationLastErr
	d.mu.RUnlock()

	logging.Info("cycle summary",
		"repos_total", reposTotal,
		"repos_active", reposActive,
		"repos_pending", reposPending,
		"repos_needs_reclassify", reposNeedsReclassify,
		"repos_needs_review", reposNeedsReview,
		"last_classified_at", lastClassifiedAt,
		"classification_last_error", classificationLastErr,
	)
}

// runDiscovery runs topic-based discovery.
func (d *Daemon) runDiscovery() {
	logging.Info("starting discovery scan")

	results, err := d.discoverer.DiscoverAll(d.ctx)
	if err != nil && err != context.Canceled {
		logging.Error("discovery failed", "error", err)
		return
	}

	totalNew := 0
	totalTracked := 0
	for _, result := range results {
		totalNew += result.NewRepos
		tracked := d.discoverer.AutoTrack(result)
		totalTracked += len(tracked)
	}

	if totalTracked > 0 {
		logging.Info("discovery auto-tracked repos",
			"new_found", totalNew,
			"auto_tracked", totalTracked)
	}
}

// runClassification runs LLM-based classification on pending repos.
func (d *Daemon) runClassification() {
	logging.Info("starting classification scan")

	summary, err := d.classifier.ClassifyAll(d.ctx)

	// Record classification run outcome (ISI-775). A top-level error → "failed";
	// per-repo failures only → "partial"; otherwise "success". context.Canceled
	// is shutdown-driven, not a regression — don't taint the failed counter.
	result := metrics.ClassificationRunSuccess
	switch {
	case err != nil && err == context.Canceled:
		// Skip recording — daemon is shutting down.
	case err != nil:
		result = metrics.ClassificationRunFailed
	case summary != nil && summary.Failed > 0:
		result = metrics.ClassificationRunPartial
	}

	d.mu.Lock()
	if err != nil && err != context.Canceled {
		d.classificationLastErr = err.Error()
	} else {
		d.classificationLastErr = ""
	}
	d.mu.Unlock()

	if d.exporter != nil && !(err != nil && err == context.Canceled) {
		d.exporter.RecordClassificationRun(d.ctx, result)
	}

	if err != nil && err != context.Canceled {
		logging.Error("classification failed", "error", err)
		return
	}

	if summary != nil {
		logging.Info("classification complete",
			"total", summary.Total,
			"classified", summary.Classified,
			"needs_review", summary.NeedsReview,
			"skipped", summary.Skipped,
			"failed", summary.Failed,
			"duration", summary.Duration,
			"result", string(result))
	}
}

// exportMetrics exports all repo metrics via OTel.
func (d *Daemon) exportMetrics() {
	allStates := d.store.AllRepoStates()

	if len(allStates) == 0 {
		logging.Warn("no repo states to export metrics for")
		return
	}

	for fullName, repoState := range allStates {
		parts := strings.SplitN(fullName, "/", 2)
		if len(parts) != 2 {
			continue
		}

		// Resolve taxonomy: DB classification > config > "default" / "pending".
		// (ISI-786) — pull the (category, subcategory, legacy) triple via
		// ResolveTaxonomy so force_* overrides and pre-v3 flat slugs are
		// collapsed to the v3 (cat, sub) shape with a stable legacy fallback.
		// (ISI-775) — capture the raw repo status so the no-category fallback
		// below can surface "pending" separately from "default" instead of
		// conflating the two and hiding SQL Scan drift like the v3 incident.
		var categories []string
		var subcategory, categoryLegacy string
		repoStatus := ""
		if d.db != nil {
			if repo, err := d.db.GetRepo(fullName); err == nil && repo != nil {
				repoStatus = repo.Status
				cat, sub, legacy := repo.ResolveTaxonomy()
				if cat != "" {
					categories = []string{cat}
				}
				subcategory = sub
				categoryLegacy = legacy
			}
		}
		if len(categories) == 0 {
			for _, tracked := range d.cfg.Repositories {
				if tracked.Repo == fullName {
					categories = tracked.Categories
					break
				}
			}
		}
		if len(categories) == 0 {
			if repoStatus == "pending" {
				categories = []string{"pending"}
			} else {
				categories = []string{"default"}
			}
		}

		repoMetrics := metrics.RepoMetrics{
			Owner:             parts[0],
			Name:              parts[1],
			Language:          "", // Would need to store this in state
			Categories:        categories,
			Subcategory:       subcategory,
			CategoryLegacy:    categoryLegacy,
			Stars:             repoState.Stars,
			Forks:             repoState.Forks,
			OpenIssues:        0, // Would need to store this
			OpenPRs:           0, // Would need to store this
			Contributors:      repoState.Contributors,
			GrowthScore:       repoState.GrowthScore,
			NormalizedScore:   repoState.NormalizedGrowthScore,
			StarVelocity:      repoState.StarVelocity,
			StarAcceleration:  repoState.StarAcceleration,
			PRVelocity:        repoState.PRVelocity,
			IssueVelocity:     repoState.IssueVelocity,
			ContributorGrowth: repoState.ContributorGrowth,
		}

		d.exporter.RecordRepoMetrics(d.ctx, repoMetrics)
	}

	// ISI-773: include needs_reclassify backlog gauge so each flush carries
	// the same drift signal the shutdown line surfaces.
	needsReclassifyTotal := -1
	if d.db != nil {
		if n, err := d.db.NeedsReclassifyCount(); err == nil {
			needsReclassifyTotal = n
		} else {
			logging.Warn("needs_reclassify count failed", "error", err)
		}
	}

	// ISI-775: emit pending-status gauge buckets so the next SQL Scan / pipeline
	// regression surfaces within one cycle. Tagged by (excluded,
	// force_category_set) so dashboards can split transient new-discovery state
	// from genuinely stuck rows. Logged inline as well so a `journalctl |
	// tail` shows the same number the gauge does.
	pendingTotalActive := -1
	if d.db != nil {
		if buckets, err := d.db.PendingCountsByDimension(); err != nil {
			logging.Warn("pending count failed", "error", err)
		} else {
			metricBuckets := make([]metrics.PendingBucket, 0, len(buckets))
			for _, b := range buckets {
				metricBuckets = append(metricBuckets, metrics.PendingBucket{
					Excluded:         b.Excluded,
					ForceCategorySet: b.ForceCategorySet,
					Count:            b.Count,
				})
				if !b.Excluded && !b.ForceCategorySet {
					pendingTotalActive = b.Count
				}
			}
			d.exporter.RecordPendingBuckets(d.ctx, metricBuckets)
		}
	}

	logging.Info("recorded metrics",
		"repos", len(allStates),
		"repos_needs_reclassify_total", needsReclassifyTotal,
		"repos_pending_active", pendingTotalActive,
	)

	// Flush metrics
	flushCtx, cancel := context.WithTimeout(d.ctx, 30*time.Second)
	defer cancel()
	if err := d.exporter.Flush(flushCtx); err != nil {
		logging.Error("metrics flush failed", "error", err)
	} else {
		logging.Info("metrics flushed successfully")
	}
}

// syncStoreToDatabase syncs repos from the in-memory store to the SQLite database.
// This ensures the classification pipeline and metric export can find repo records.
// Only scan-related fields are updated; classification fields are preserved.
func (d *Daemon) syncStoreToDatabase() {
	if d.db == nil {
		return
	}

	allStates := d.store.AllRepoStates()
	synced := 0
	for fullName, rs := range allStates {
		parts := strings.SplitN(fullName, "/", 2)
		if len(parts) != 2 {
			continue
		}

		record := &database.RepoRecord{
			FullName:              fullName,
			Owner:                 parts[0],
			Name:                  parts[1],
			Stars:                 rs.Stars,
			StarsPrev:             rs.StarsPrev,
			Forks:                 rs.Forks,
			Contributors:          rs.Contributors,
			ContributorsPrev:      rs.ContributorsPrev,
			GrowthScore:           rs.GrowthScore,
			NormalizedGrowthScore: rs.NormalizedGrowthScore,
			StarVelocity:          rs.StarVelocity,
			StarAcceleration:      rs.StarAcceleration,
			PRVelocity:            rs.PRVelocity,
			IssueVelocity:         rs.IssueVelocity,
			ContributorGrowth:     rs.ContributorGrowth,
			MergedPRs7d:           rs.MergedPRs7d,
			NewIssues7d:           rs.NewIssues7d,
			LastCollectedAt:       rs.LastCollected.Format(time.RFC3339),
			ETag:                  rs.ETag,
			LastModified:          rs.LastModified,
			Status:                "pending",
			FirstSeenAt:           time.Now().Format(time.RFC3339),
		}

		if err := d.db.SyncScanData(record); err != nil {
			logging.Error("failed to sync repo to database", "repo", fullName, "error", err)
			continue
		}
		synced++
	}

	if synced > 0 {
		logging.Info("synced repos from store to database", "count", synced)
	}
}

// scheduleNextScan calculates and sets the next scan time.
func (d *Daemon) scheduleNextScan() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.lastScan.IsZero() {
		// First scan - run immediately
		d.nextScan = time.Now()
	} else {
		d.nextScan = d.lastScan.Add(d.daemonCfg.Interval)
	}

	logging.Debug("next scan scheduled", "time", d.nextScan.Format(time.RFC3339))
}

// reloadConfig reloads configuration from file.
func (d *Daemon) reloadConfig() {
	if d.daemonCfg.ConfigPath == "" {
		logging.Warn("no config path set, cannot reload")
		return
	}

	newCfg, err := config.LoadFromPath(d.daemonCfg.ConfigPath)
	if err != nil {
		logging.Error("config reload failed, keeping old config", "error", err)
		return
	}

	d.mu.Lock()
	d.cfg = newCfg
	d.mu.Unlock()

	// Update scanner weights
	d.scanner.SetScoringWeights(scoring.Weights{
		StarVelocity:      newCfg.Scoring.Weights.StarVelocity,
		StarAcceleration:  newCfg.Scoring.Weights.StarAcceleration,
		ForkVelocity:      newCfg.Scoring.Weights.ForkVelocity,
		ReleaseCadence:    newCfg.Scoring.Weights.ReleaseCadence,
		ContributorGrowth: newCfg.Scoring.Weights.ContributorGrowth,
		PRVelocity:        newCfg.Scoring.Weights.PRVelocity,
		IssueVelocity:     newCfg.Scoring.Weights.IssueVelocity,
	})

	logging.Info("config reloaded",
		"repos", len(newCfg.Repositories),
		"topics", len(newCfg.Discovery.Topics))
}

// isExcluded checks if a repo matches any exclusion pattern.
func isExcluded(fullName string, exclusions []string) bool {
	for _, pattern := range exclusions {
		if MatchesPattern(fullName, pattern) {
			return true
		}
	}
	return false
}

// MatchesPattern checks if a name matches a glob-like pattern.
// Supports:
//   - Exact match: "owner/repo"
//   - Wildcard suffix: "owner/*" matches all repos from owner
//   - Wildcard prefix: "*/repo" matches repo from any owner
//   - Full wildcard: "*/*" matches everything
//
// Names must be valid "owner/repo" format (exactly one slash).
func MatchesPattern(name, pattern string) bool {
	// Validate name format - must have exactly one slash
	nameParts := strings.Split(name, "/")
	if len(nameParts) != 2 {
		return false
	}

	// Handle exact match
	if name == pattern {
		return true
	}

	// Handle wildcard patterns
	if strings.Contains(pattern, "*") {
		patternParts := strings.Split(pattern, "/")
		if len(patternParts) != 2 {
			return false
		}

		ownerMatch := patternParts[0] == "*" || patternParts[0] == nameParts[0]
		repoMatch := patternParts[1] == "*" || patternParts[1] == nameParts[1]

		return ownerMatch && repoMatch
	}

	return false
}

// setStatus updates the daemon status.
func (d *Daemon) setStatus(s Status) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status = s
}

// setReady updates the daemon ready state.
func (d *Daemon) setReady(ready bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ready = ready
}

// handleHealth handles the /health endpoint.
func (d *Daemon) handleHealth(w http.ResponseWriter, r *http.Request) {
	d.mu.RLock()
	status := d.status
	ready := d.ready
	d.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	// Not healthy if stopping or not yet ready
	if status == StatusStopping || !ready {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]bool{"healthy": false})
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"healthy": true})
}

// StatusResponse is the response for /status endpoint.
type StatusResponse struct {
	Status             string `json:"status"`
	LastScan           string `json:"last_scan,omitempty"`
	NextScan           string `json:"next_scan,omitempty"`
	ReposTracked       int    `json:"repos_tracked"`
	RateLimitRemaining int    `json:"rate_limit_remaining"`
	Uptime             string `json:"uptime"`
}

// handleStatus handles the /status endpoint.
func (d *Daemon) handleStatus(w http.ResponseWriter, r *http.Request) {
	d.mu.RLock()
	resp := StatusResponse{
		Status:             string(d.status),
		ReposTracked:       d.reposTracked,
		RateLimitRemaining: d.rateLimitRemain,
		Uptime:             time.Since(d.startTime).Round(time.Second).String(),
	}
	if !d.lastScan.IsZero() {
		resp.LastScan = d.lastScan.Format(time.RFC3339)
	}
	if !d.nextScan.IsZero() {
		resp.NextScan = d.nextScan.Format(time.RFC3339)
	}
	d.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// runTieredScan classifies the candidate repos into refresh tiers and
// dispatches each bucket to the appropriate scanner path:
//   - TierHot / TierWarm / TierNew: GraphQL bulk fetch (≤1 call per 50
//     repos for metadata), plus per-repo REST activity collection.
//   - TierCold: existing REST path with conditional GET (preserves the
//     304 cache-hit savings on long-tail repos).
//
// Per the ISI-709 plan §5 this keeps steady-state calls ≤ ~1,000/hr
// at 3,000 repos and well under the 5000/hr budget with headroom for
// discovery bursts.
func (d *Daemon) runTieredScan(requested []github.Repo) (*github.ScanResult, error) {
	tierCfg := tierConfigFromYAML(d.cfg.GitHub.Tiering)
	candidates := d.buildTierCandidates()
	assignments := github.ClassifyAll(candidates, time.Now(), tierCfg)
	histogram := github.Count(assignments)
	d.publishTierHistogram(histogram)

	logging.Info("tiered scan planning",
		"total", len(assignments),
		"hot", histogram.Hot,
		"warm", histogram.Warm,
		"cold", histogram.Cold,
		"new", histogram.New)

	// Index requested repos so we only act on repos the operator asked for.
	requestedSet := make(map[string]github.Repo, len(requested))
	for _, r := range requested {
		requestedSet[r.Owner+"/"+r.Name] = r
	}

	// Split due repos by tier.
	var bulkBatch []github.Repo
	var coldBatch []github.Repo
	for _, a := range github.DueRepos(assignments) {
		r, ok := requestedSet[a.FullName]
		if !ok {
			continue
		}
		if a.Tier == github.TierCold {
			coldBatch = append(coldBatch, r)
		} else {
			bulkBatch = append(bulkBatch, r)
		}
	}

	combined := &github.ScanResult{StartTime: time.Now()}

	// Cold tier via REST + conditional GET.
	if len(coldBatch) > 0 {
		cr, err := d.scanner.Scan(d.ctx, coldBatch)
		if err != nil && err != context.Canceled {
			logging.Warn("cold-tier scan returned error", "error", err)
		}
		if cr != nil {
			combined.Total += cr.Total
			combined.Successful += cr.Successful
			combined.Failed += cr.Failed
			combined.Skipped += cr.Skipped
			combined.Updated += cr.Updated
			combined.FailedRepos = append(combined.FailedRepos, cr.FailedRepos...)
		}
	}

	// Hot/warm/new tiers via GraphQL bulk fetch.
	if len(bulkBatch) > 0 {
		br, err := d.scanner.ScanBulk(d.ctx, bulkBatch)
		if err != nil && err != context.Canceled {
			logging.Warn("bulk-tier scan returned error", "error", err)
		}
		if br != nil {
			combined.Total += br.Total
			combined.Successful += br.Successful
			combined.Failed += br.Failed
			combined.Skipped += br.Skipped
			combined.Updated += br.Updated
			combined.FailedRepos = append(combined.FailedRepos, br.FailedRepos...)
		}
	}

	// Re-publish rate limit snapshot after the sweep so the dashboard
	// value is current even if no API calls raced a ticker reading.
	d.publishRateLimit()

	combined.EndTime = time.Now()
	return combined, nil
}

// runCanaryScan partitions the requested repos into a canary set
// (matching d.cfg.GitHub.BulkFetchCanaryFullNames, case-insensitive) and
// a legacy set, then dispatches each to the appropriate scan path:
//   - canary set → runTieredScan (T5 GraphQL bulk + tiered cadence)
//   - legacy set → scanner.Scan (pre-T5 REST single-pass)
//
// Used for staged prod canary rollout (ISI-792 / ISI-716): Stage A 10%,
// Stage B 50%, Stage C 100% (Stage C uses an empty list + bulk_fetch
// flag on, which routes through runTieredScan directly without this
// partition step).
func (d *Daemon) runCanaryScan(repos []github.Repo) (*github.ScanResult, error) {
	canaryRepos, legacyRepos := partitionByCanary(repos, d.cfg.GitHub.BulkFetchCanaryFullNames)

	logging.Info("canary scan planning",
		"canary_repos", len(canaryRepos),
		"legacy_repos", len(legacyRepos),
		"canary_list_size", len(d.cfg.GitHub.BulkFetchCanaryFullNames))

	combined := &github.ScanResult{StartTime: time.Now()}

	// Canary set → tiered/bulk path.
	if len(canaryRepos) > 0 {
		cr, err := d.runTieredScan(canaryRepos)
		if err != nil && err != context.Canceled {
			logging.Warn("canary tiered scan returned error", "error", err)
		}
		if cr != nil {
			combined.Total += cr.Total
			combined.Successful += cr.Successful
			combined.Failed += cr.Failed
			combined.Skipped += cr.Skipped
			combined.Updated += cr.Updated
			combined.FailedRepos = append(combined.FailedRepos, cr.FailedRepos...)
		}
	}

	// Legacy set → pre-T5 REST + conditional path.
	if len(legacyRepos) > 0 {
		lr, err := d.scanner.Scan(d.ctx, legacyRepos)
		if err != nil && err != context.Canceled {
			logging.Warn("canary legacy scan returned error", "error", err)
		}
		if lr != nil {
			combined.Total += lr.Total
			combined.Successful += lr.Successful
			combined.Failed += lr.Failed
			combined.Skipped += lr.Skipped
			combined.Updated += lr.Updated
			combined.FailedRepos = append(combined.FailedRepos, lr.FailedRepos...)
		}
	}

	combined.EndTime = time.Now()
	return combined, nil
}

// scanPathLabel returns the "path" attribute value attached to the
// github.scan.duration histogram for the cycle just completed. The
// value mirrors the switch in runScan.
func scanPathLabel(g config.GithubConfig) string {
	switch {
	case g.BulkFetchEnabled && len(g.BulkFetchCanaryFullNames) > 0:
		return "canary"
	case g.BulkFetchEnabled:
		return "bulk"
	default:
		return "legacy"
	}
}

// partitionByCanary splits repos into (canary, legacy) per the canary
// full-name list. Matching is case-insensitive and tolerates surrounding
// whitespace. Pulled out as a pure function so the partition contract
// is unit-testable without standing up the full Daemon scaffolding.
func partitionByCanary(repos []github.Repo, canary []string) (canarySet, legacySet []github.Repo) {
	set := make(map[string]struct{}, len(canary))
	for _, fn := range canary {
		key := strings.ToLower(strings.TrimSpace(fn))
		if key == "" {
			continue
		}
		set[key] = struct{}{}
	}
	for _, r := range repos {
		key := strings.ToLower(r.Owner + "/" + r.Name)
		if _, ok := set[key]; ok {
			canarySet = append(canarySet, r)
		} else {
			legacySet = append(legacySet, r)
		}
	}
	return canarySet, legacySet
}

// logWithLevel logs a message at the specified level.
func logWithLevel(level, msg string, args ...interface{}) {
	switch level {
	case "debug":
		logging.Debug(msg, args...)
	case "info":
		logging.Info(msg, args...)
	case "warn":
		logging.Warn(msg, args...)
	case "error":
		logging.Error(msg, args...)
	default:
		logging.Info(msg, args...)
	}
}
