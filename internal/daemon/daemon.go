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

	"github.com/hrexed/github-radar/internal/config"
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
	exporter   *metrics.Exporter
	store      *state.Store
	server     *http.Server

	mu              sync.RWMutex
	status          Status
	lastScan        time.Time
	nextScan        time.Time
	reposTracked    int
	rateLimitRemain int
	startTime       time.Time // instance start time for uptime tracking
	ready           bool      // true when daemon is fully initialized

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
		}
		disc = discovery.NewDiscoverer(client, store, discCfg)
		disc.SetLogger(func(level, msg string, args ...interface{}) {
			logWithLevel(level, msg, args...)
		})
	}

	// Create metrics exporter
	var exp *metrics.Exporter
	if !daemonCfg.DryRun {
		exporterCfg := metrics.ExporterConfig{
			Endpoint:       cfg.Otel.Endpoint,
			Headers:        cfg.Otel.Headers,
			ServiceName:    cfg.Otel.ServiceName,
			ServiceVersion: cfg.Otel.ServiceVersion,
		}
		exp, err = metrics.NewExporter(exporterCfg)
		if err != nil {
			return nil, fmt.Errorf("creating metrics exporter: %w", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	d := &Daemon{
		cfg:        cfg,
		daemonCfg:  daemonCfg,
		client:     client,
		scanner:    scanner,
		discoverer: disc,
		exporter:   exp,
		store:      store,
		status:     StatusIdle,
		startTime:  time.Now(),
		ready:      false,
		ctx:        ctx,
		cancel:     cancel,
		reloadChan: make(chan os.Signal, 1),
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

	logging.Info("daemon stopped")
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
	for _, tracked := range repositories {
		// Skip excluded repos
		if isExcluded(tracked.Repo, exclusions) {
			continue
		}
		parts := strings.SplitN(tracked.Repo, "/", 2)
		if len(parts) == 2 {
			repos = append(repos, github.Repo{Owner: parts[0], Name: parts[1]})
		}
	}

	// Run scan
	result, err := d.scanner.Scan(d.ctx, repos)
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

		logging.Info("scan complete",
			"total", result.Total,
			"successful", result.Successful,
			"failed", result.Failed,
			"skipped", result.Skipped,
			"duration", result.EndTime.Sub(result.StartTime))
	}

	// Run discovery if enabled
	if d.discoverer != nil {
		d.runDiscovery()
	}

	// Update status info
	d.mu.Lock()
	d.lastScan = scanStartTime
	d.reposTracked = len(repos)
	d.rateLimitRemain = d.client.RateLimitInfo().Remaining
	d.mu.Unlock()

	// Schedule next scan
	d.scheduleNextScan()
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

// exportMetrics exports all repo metrics via OTel.
func (d *Daemon) exportMetrics() {
	allStates := d.store.AllRepoStates()

	for fullName, repoState := range allStates {
		parts := strings.SplitN(fullName, "/", 2)
		if len(parts) != 2 {
			continue
		}

		// Find categories from config
		var categories []string
		for _, tracked := range d.cfg.Repositories {
			if tracked.Repo == fullName {
				categories = tracked.Categories
				break
			}
		}
		if len(categories) == 0 {
			categories = []string{"default"}
		}

		repoMetrics := metrics.RepoMetrics{
			Owner:             parts[0],
			Name:              parts[1],
			Language:          "", // Would need to store this in state
			Categories:        categories,
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

	// Flush metrics
	flushCtx, cancel := context.WithTimeout(d.ctx, 30*time.Second)
	defer cancel()
	if err := d.exporter.Flush(flushCtx); err != nil {
		logging.Warn("metrics flush error", "error", err)
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
