// Package discovery provides topic-based repository discovery for github-radar.
package discovery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hrexed/github-radar/internal/github"
	"github.com/hrexed/github-radar/internal/scoring"
	"github.com/hrexed/github-radar/internal/state"
)

// Config contains discovery configuration.
type Config struct {
	// Topics to search for
	Topics []string

	// MinStars filters out repos with fewer stars
	MinStars int

	// MaxAgeDays filters out repos older than this (0 = no limit)
	MaxAgeDays int

	// AutoTrackThreshold is the growth score threshold for auto-tracking
	AutoTrackThreshold float64

	// Exclusions is a list of repo patterns to exclude
	Exclusions []string

	// Sources controls which discovery sources beyond the default topic
	// search are active. Each source is feature-flagged and disabled by
	// default so rollout can be staged via config alone.
	//
	// Source (1) — topic search — is always enabled when Topics is set.
	// Source (3) — org-scoped search — is gated by Sources.Orgs.Enabled.
	// Source (4) — language-pivot search — is gated by Sources.Languages.Enabled.
	Sources SourcesConfig
}

// SourcesConfig contains per-source discovery configuration.
type SourcesConfig struct {
	Orgs      OrgsSourceConfig      `yaml:"orgs"`
	Languages LanguagesSourceConfig `yaml:"languages"`
}

// OrgsSourceConfig configures Source (3): per-org repository search.
//
// When enabled, discovery iterates Names and runs `org:{name} stars:>={MinStars}`
// per org. This catches actively-maintained repos under high-signal orgs
// (kubernetes, hashicorp, opentelemetry, …) that may not have a topic
// label matching our topic list.
type OrgsSourceConfig struct {
	// Enabled gates whether Source (3) runs at all.
	Enabled bool `yaml:"enabled"`
	// Names is the list of GitHub orgs to search.
	Names []string `yaml:"names"`
	// MinStars overrides Discovery.MinStars for org-scoped queries.
	// Zero falls back to Discovery.MinStars.
	MinStars int `yaml:"min_stars"`
}

// LanguagesSourceConfig configures Source (4): language-pivot search.
//
// When enabled, discovery runs `language:{name} stars:>={MinStars}
// pushed:>={cutoff} sort:updated` for each language and each entry in
// PushWindowsDays. Three windows (e.g. 7/30/90) catch both bursts and
// long-running maintenance.
type LanguagesSourceConfig struct {
	// Enabled gates whether Source (4) runs at all.
	Enabled bool `yaml:"enabled"`
	// Names is the list of GitHub language identifiers to search.
	Names []string `yaml:"names"`
	// MinStars overrides Discovery.MinStars for language-pivot queries.
	// Zero falls back to Discovery.MinStars.
	MinStars int `yaml:"min_stars"`
	// PushWindowsDays is the list of `pushed:>=` windows in days.
	// Empty defaults to a single 7-day window.
	PushWindowsDays []int `yaml:"push_windows_days"`
}

// DefaultConfig returns default discovery configuration.
func DefaultConfig() Config {
	return Config{
		MinStars:           100,
		MaxAgeDays:         90,
		AutoTrackThreshold: 50.0,
	}
}

// DiscoveredRepo represents a discovered repository with its metrics.
type DiscoveredRepo struct {
	Owner       string
	Name        string
	FullName    string
	Description string
	Language    string
	Topics      []string
	Stars       int
	Forks       int
	CreatedAt   time.Time
	UpdatedAt   time.Time

	// Calculated scores
	GrowthScore     float64
	NormalizedScore float64
	ShouldAutoTrack bool
	AlreadyTracked  bool
	Excluded        bool
}

// Result contains the results of a discovery run.
type Result struct {
	Topic          string
	StartTime      time.Time
	EndTime        time.Time
	TotalFound     int
	AfterFilters   int
	NewRepos       int
	AutoTracked    int
	AlreadyTracked int
	Excluded       int
	Repos          []DiscoveredRepo
}

// DefaultSearchAPIThrottle is the minimum delay between successive
// Search API calls inside DiscoverAll. The GitHub Search API quota is
// 30 req/min (~one call every 2 seconds). At 569 active repos × 177
// configured topics we observed bursts saturating the per-minute window
// mid-cycle (`rate limit exhausted, resets at <next minute>` in steady
// state on 2026-04-25 10:27 CEST). 2s pacing keeps a single discoverer
// at ~30 req/min steady-state while preserving headroom for retries.
//
// Tests construct a Discoverer with SetSearchThrottle(0) to keep the
// suite fast; production code paths leave the default in place.
const DefaultSearchAPIThrottle = 2 * time.Second

// Discoverer handles repository discovery.
type Discoverer struct {
	client     *github.Client
	store      *state.Store
	calculator *scoring.Calculator
	config     Config
	throttle   time.Duration
	onLog      func(level, msg string, args ...interface{})
}

// NewDiscoverer creates a new discoverer.
func NewDiscoverer(client *github.Client, store *state.Store, config Config) *Discoverer {
	return &Discoverer{
		client:     client,
		store:      store,
		calculator: scoring.NewCalculatorWithDefaults(),
		config:     config,
		throttle:   DefaultSearchAPIThrottle,
	}
}

// SetSearchThrottle overrides the inter-call Search API throttle.
// Production code should leave this at DefaultSearchAPIThrottle; tests
// pass 0 to avoid real-time waits.
func (d *Discoverer) SetSearchThrottle(t time.Duration) {
	d.throttle = t
}

// SetLogger sets a logging callback.
func (d *Discoverer) SetLogger(fn func(level, msg string, args ...interface{})) {
	d.onLog = fn
}

func (d *Discoverer) log(level, msg string, args ...interface{}) {
	if d.onLog != nil {
		d.onLog(level, msg, args...)
	}
}

// SetScoringCalculator sets a custom scoring calculator.
func (d *Discoverer) SetScoringCalculator(calc *scoring.Calculator) {
	d.calculator = calc
}

// DiscoverTopic discovers repositories for a single topic.
func (d *Discoverer) DiscoverTopic(ctx context.Context, topic string) (*Result, error) {
	result := &Result{
		Topic:     topic,
		StartTime: time.Now(),
		Repos:     []DiscoveredRepo{},
	}

	d.log("info", "Starting discovery", "topic", topic)

	// Search GitHub for repositories with this topic
	repos, err := d.searchByTopic(ctx, topic)
	if err != nil {
		return nil, fmt.Errorf("searching topic %s: %w", topic, err)
	}

	result.TotalFound = len(repos)
	d.log("debug", "Search completed", "topic", topic, "found", len(repos))

	// Filter and process repos
	for _, repo := range repos {
		discovered := d.processRepo(repo)

		// Check if already tracked
		if d.store.GetRepoState(discovered.FullName) != nil {
			discovered.AlreadyTracked = true
			result.AlreadyTracked++
		}

		// Check exclusions
		if d.isExcluded(discovered.FullName) {
			discovered.Excluded = true
			result.Excluded++
		}

		// Apply filters
		if !d.passesFilters(discovered) {
			continue
		}

		result.AfterFilters++

		// Check if should auto-track
		if !discovered.AlreadyTracked && !discovered.Excluded {
			result.NewRepos++
			if discovered.NormalizedScore >= d.config.AutoTrackThreshold {
				discovered.ShouldAutoTrack = true
				result.AutoTracked++
			}
		}

		result.Repos = append(result.Repos, discovered)
	}

	// Normalize scores across discovered repos
	d.normalizeScores(result)

	result.EndTime = time.Now()
	d.log("info", "Discovery complete",
		"topic", topic,
		"found", result.TotalFound,
		"after_filters", result.AfterFilters,
		"new", result.NewRepos,
		"auto_track", result.AutoTracked)

	return result, nil
}

// DiscoverAll runs every enabled discovery source and returns one
// Result per (source, query) pair. The default topic source always
// runs when Topics is set; org-scoped and language-pivot sources are
// gated by Sources.Orgs.Enabled and Sources.Languages.Enabled.
//
// All sources share the same Search API budget, so DiscoverAll inserts
// d.throttle between every successive Search call regardless of source.
// Repositories surfaced by an earlier source are deduplicated out of
// later results so a repo discovered by both topic and org search is
// only inserted into discovered_known_repos once per cycle.
func (d *Discoverer) DiscoverAll(ctx context.Context) ([]*Result, error) {
	plan := d.buildSearchPlan()
	if len(plan) == 0 {
		d.log("info", "No discovery sources configured, skipping discovery")
		return nil, nil
	}

	var (
		results []*Result
		seen    = map[string]struct{}{}
	)
	for i, step := range plan {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		// Throttle to stay under the 30 req/min Search API quota,
		// shared across topic / orgs / language-pivot sources. Skip
		// the wait before the first call so a one-off run pays no
		// upfront cost.
		if i > 0 && d.throttle > 0 {
			select {
			case <-ctx.Done():
				return results, ctx.Err()
			case <-time.After(d.throttle):
			}
		}

		result, err := step.run(ctx)
		if err != nil {
			d.log("warn", "Discovery failed",
				"source", step.source, "query", step.label, "error", err)
			continue
		}

		// Cross-source dedup: drop any repo already surfaced this
		// cycle by an earlier source. The first source wins
		// (topic > orgs > languages by buildSearchPlan ordering),
		// so org/language searches don't re-promote repos the
		// topic search already counted.
		result.Repos = filterAndMarkSeen(result.Repos, seen,
			&result.AfterFilters, &result.NewRepos, &result.AutoTracked)

		results = append(results, result)
	}

	return results, nil
}

// searchStep is one Search API call within a DiscoverAll cycle.
type searchStep struct {
	source string // "topic" | "org" | "language"
	label  string // human-readable identifier shown in logs/results
	run    func(ctx context.Context) (*Result, error)
}

// buildSearchPlan returns the ordered list of Search API calls to make
// in a DiscoverAll cycle. Topic search runs first, then org-scoped,
// then language-pivot. Order matters: cross-source dedup keeps the
// earliest hit, so the most specific signal wins.
func (d *Discoverer) buildSearchPlan() []searchStep {
	var plan []searchStep

	for _, topic := range d.config.Topics {
		topic := topic
		plan = append(plan, searchStep{
			source: "topic",
			label:  topic,
			run:    func(ctx context.Context) (*Result, error) { return d.DiscoverTopic(ctx, topic) },
		})
	}

	if d.config.Sources.Orgs.Enabled {
		for _, org := range d.config.Sources.Orgs.Names {
			org := org
			plan = append(plan, searchStep{
				source: "org",
				label:  org,
				run:    func(ctx context.Context) (*Result, error) { return d.DiscoverOrg(ctx, org) },
			})
		}
	}

	if d.config.Sources.Languages.Enabled {
		windows := d.config.Sources.Languages.PushWindowsDays
		if len(windows) == 0 {
			windows = []int{7}
		}
		for _, lang := range d.config.Sources.Languages.Names {
			for _, days := range windows {
				lang, days := lang, days
				plan = append(plan, searchStep{
					source: "language",
					label:  fmt.Sprintf("%s/pushed-%dd", lang, days),
					run: func(ctx context.Context) (*Result, error) {
						return d.DiscoverLanguage(ctx, lang, days)
					},
				})
			}
		}
	}

	return plan
}

// filterAndMarkSeen drops any DiscoveredRepo whose FullName is already
// in seen, marking the rest as seen. Counters are decremented for
// dropped entries so totals across sources reflect unique repos.
func filterAndMarkSeen(repos []DiscoveredRepo, seen map[string]struct{}, afterFilters, newRepos, autoTracked *int) []DiscoveredRepo {
	out := repos[:0]
	for _, r := range repos {
		if _, dup := seen[r.FullName]; dup {
			if afterFilters != nil {
				*afterFilters--
			}
			if newRepos != nil && !r.AlreadyTracked && !r.Excluded {
				*newRepos--
			}
			if autoTracked != nil && r.ShouldAutoTrack && !r.AlreadyTracked && !r.Excluded {
				*autoTracked--
			}
			continue
		}
		seen[r.FullName] = struct{}{}
		out = append(out, r)
	}
	return out
}

// DiscoverOrg runs Source (3): org-scoped repository search. Returns
// repos owned by org with at least Sources.Orgs.MinStars (or
// Discovery.MinStars when zero). Honors the same age + exclusion +
// auto-track logic as DiscoverTopic.
func (d *Discoverer) DiscoverOrg(ctx context.Context, org string) (*Result, error) {
	result := &Result{
		Topic:     "org:" + org,
		StartTime: time.Now(),
		Repos:     []DiscoveredRepo{},
	}

	d.log("info", "Starting org discovery", "org", org)

	repos, err := d.searchByOrg(ctx, org)
	if err != nil {
		return nil, fmt.Errorf("searching org %s: %w", org, err)
	}

	result.TotalFound = len(repos)
	d.log("debug", "Org search completed", "org", org, "found", len(repos))

	d.processSearchResults(repos, result)
	d.normalizeScores(result)

	result.EndTime = time.Now()
	d.log("info", "Org discovery complete",
		"org", org,
		"found", result.TotalFound,
		"after_filters", result.AfterFilters,
		"new", result.NewRepos,
		"auto_track", result.AutoTracked)

	return result, nil
}

// DiscoverLanguage runs Source (4): language-pivot repository search
// for one language and one push-window. The query
// `language:{lang} stars:>={MinStars} pushed:>={cutoff} sort:updated`
// catches actively-maintained popular projects across topics.
func (d *Discoverer) DiscoverLanguage(ctx context.Context, lang string, pushDays int) (*Result, error) {
	if pushDays <= 0 {
		pushDays = 7
	}
	result := &Result{
		Topic:     fmt.Sprintf("language:%s/pushed-%dd", lang, pushDays),
		StartTime: time.Now(),
		Repos:     []DiscoveredRepo{},
	}

	d.log("info", "Starting language discovery", "language", lang, "push_days", pushDays)

	repos, err := d.searchByLanguage(ctx, lang, pushDays)
	if err != nil {
		return nil, fmt.Errorf("searching language %s pushed-%dd: %w", lang, pushDays, err)
	}

	result.TotalFound = len(repos)
	d.log("debug", "Language search completed", "language", lang, "push_days", pushDays, "found", len(repos))

	d.processSearchResults(repos, result)
	d.normalizeScores(result)

	result.EndTime = time.Now()
	d.log("info", "Language discovery complete",
		"language", lang,
		"push_days", pushDays,
		"found", result.TotalFound,
		"after_filters", result.AfterFilters,
		"new", result.NewRepos,
		"auto_track", result.AutoTracked)

	return result, nil
}

// processSearchResults runs the same filter/exclusion/auto-track
// pipeline as DiscoverTopic does for its inline loop, against the
// unified Result. Used by org and language sources to keep behavior
// identical across sources.
func (d *Discoverer) processSearchResults(repos []github.SearchResult, result *Result) {
	for _, repo := range repos {
		discovered := d.processRepo(repo)

		if d.store.GetRepoState(discovered.FullName) != nil {
			discovered.AlreadyTracked = true
			result.AlreadyTracked++
		}

		if d.isExcluded(discovered.FullName) {
			discovered.Excluded = true
			result.Excluded++
		}

		if !d.passesFilters(discovered) {
			continue
		}

		result.AfterFilters++

		if !discovered.AlreadyTracked && !discovered.Excluded {
			result.NewRepos++
			if discovered.NormalizedScore >= d.config.AutoTrackThreshold {
				discovered.ShouldAutoTrack = true
				result.AutoTracked++
			}
		}

		result.Repos = append(result.Repos, discovered)
	}
}

// AutoTrack adds discovered repos that meet the threshold to tracking.
// Returns the list of repos that were added.
func (d *Discoverer) AutoTrack(result *Result) []DiscoveredRepo {
	var tracked []DiscoveredRepo

	for _, repo := range result.Repos {
		if repo.ShouldAutoTrack && !repo.AlreadyTracked && !repo.Excluded {
			// Add to state with "discovered" category
			d.store.SetRepoState(repo.FullName, state.RepoState{
				Owner:         repo.Owner,
				Name:          repo.Name,
				Stars:         repo.Stars,
				Forks:         repo.Forks,
				LastCollected: time.Now(),
				GrowthScore:   repo.GrowthScore,
			})
			d.store.MarkKnownRepo(repo.FullName)

			tracked = append(tracked, repo)
			d.log("info", "Auto-tracked repository",
				"repo", repo.FullName,
				"score", repo.NormalizedScore,
				"stars", repo.Stars)
		}
	}

	return tracked
}

// searchByTopic searches GitHub for repositories with the given topic.
func (d *Discoverer) searchByTopic(ctx context.Context, topic string) ([]github.SearchResult, error) {
	// Build search query with topic and minimum stars
	query := fmt.Sprintf("topic:%s stars:>=%d", topic, d.config.MinStars)

	// Add age filter if configured
	if d.config.MaxAgeDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -d.config.MaxAgeDays)
		query += fmt.Sprintf(" created:>=%s", cutoff.Format("2006-01-02"))
	}

	return d.client.SearchRepositories(ctx, query, "stars", "desc", 100)
}

// searchByOrg searches GitHub for repositories under the given org with
// at least the configured min-stars threshold. Source (3) of the
// 4-source discovery funnel (plan rev 5).
func (d *Discoverer) searchByOrg(ctx context.Context, org string) ([]github.SearchResult, error) {
	minStars := d.config.Sources.Orgs.MinStars
	if minStars <= 0 {
		minStars = d.config.MinStars
	}

	query := fmt.Sprintf("org:%s stars:>=%d", org, minStars)

	if d.config.MaxAgeDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -d.config.MaxAgeDays)
		query += fmt.Sprintf(" created:>=%s", cutoff.Format("2006-01-02"))
	}

	return d.client.SearchRepositories(ctx, query, "stars", "desc", 100)
}

// searchByLanguage searches GitHub for repositories written in the
// given language, recently pushed within pushDays, ordered by recent
// update activity. Source (4) of the 4-source discovery funnel
// (plan rev 5). Uses Sources.Languages.MinStars (falls back to
// Discovery.MinStars). Does NOT apply MaxAgeDays — the push-window
// filter already constrains freshness, and applying both would
// over-filter long-running active projects (the exact signal this
// source exists to catch).
func (d *Discoverer) searchByLanguage(ctx context.Context, lang string, pushDays int) ([]github.SearchResult, error) {
	if pushDays <= 0 {
		pushDays = 7
	}

	minStars := d.config.Sources.Languages.MinStars
	if minStars <= 0 {
		minStars = d.config.MinStars
	}

	cutoff := time.Now().AddDate(0, 0, -pushDays).Format("2006-01-02")
	query := fmt.Sprintf("language:%s stars:>=%d pushed:>=%s", lang, minStars, cutoff)

	return d.client.SearchRepositories(ctx, query, "updated", "desc", 100)
}

// processRepo converts a search result to a discovered repo with scores.
func (d *Discoverer) processRepo(repo github.SearchResult) DiscoveredRepo {
	discovered := DiscoveredRepo{
		Owner:       repo.Owner,
		Name:        repo.Name,
		FullName:    repo.FullName,
		Description: repo.Description,
		Language:    repo.Language,
		Topics:      repo.Topics,
		Stars:       repo.Stars,
		Forks:       repo.Forks,
		CreatedAt:   repo.CreatedAt,
		UpdatedAt:   repo.UpdatedAt,
	}

	// Calculate a simple growth score based on available data
	// For discovered repos, we don't have velocity data, so use heuristics
	metrics := scoring.RepoMetrics{
		Stars:       repo.Stars,
		Forks:       repo.Forks,
		DaysElapsed: 7, // Assume 7-day window
	}

	// Estimate star velocity from repo age and current stars
	repoAgeDays := time.Since(repo.CreatedAt).Hours() / 24
	if repoAgeDays > 0 {
		// Average stars per day over lifetime
		avgStarsPerDay := float64(repo.Stars) / repoAgeDays
		// Weight recent activity higher (assume 2x average if recently updated)
		daysSinceUpdate := time.Since(repo.UpdatedAt).Hours() / 24
		if daysSinceUpdate < 7 {
			metrics.StarsPrev = int(float64(repo.Stars) - avgStarsPerDay*7)
			if metrics.StarsPrev < 0 {
				metrics.StarsPrev = 0
			}
		}
	}

	velocities := d.calculator.CalculateVelocities(metrics)
	discovered.GrowthScore = d.calculator.CalculateRawScore(velocities)

	return discovered
}

// normalizeScores normalizes growth scores across all discovered repos.
func (d *Discoverer) normalizeScores(result *Result) {
	if len(result.Repos) == 0 {
		return
	}

	// Build scored repos for normalization
	scored := make([]scoring.ScoredRepo, len(result.Repos))
	for i, repo := range result.Repos {
		scored[i] = scoring.ScoredRepo{
			FullName: repo.FullName,
			RawScore: repo.GrowthScore,
		}
	}

	// Normalize
	normalized := scoring.NormalizeScores(scored)

	// Update repos with normalized scores
	for i := range result.Repos {
		result.Repos[i].NormalizedScore = normalized[i].NormalizedScore
		// Update auto-track decision based on normalized score
		if !result.Repos[i].AlreadyTracked && !result.Repos[i].Excluded {
			result.Repos[i].ShouldAutoTrack = result.Repos[i].NormalizedScore >= d.config.AutoTrackThreshold
		}
	}

	// Recount auto-tracked
	result.AutoTracked = 0
	for _, repo := range result.Repos {
		if repo.ShouldAutoTrack && !repo.AlreadyTracked && !repo.Excluded {
			result.AutoTracked++
		}
	}
}

// passesFilters checks if a repo passes all configured filters.
func (d *Discoverer) passesFilters(repo DiscoveredRepo) bool {
	// Min stars filter
	if repo.Stars < d.config.MinStars {
		return false
	}

	// Max age filter (skip if CreatedAt is zero/unknown)
	if d.config.MaxAgeDays > 0 && !repo.CreatedAt.IsZero() {
		maxAge := time.Duration(d.config.MaxAgeDays) * 24 * time.Hour
		if time.Since(repo.CreatedAt) > maxAge {
			return false
		}
	}

	return true
}

// isExcluded checks if a repo matches any exclusion pattern.
func (d *Discoverer) isExcluded(fullName string) bool {
	for _, pattern := range d.config.Exclusions {
		if matchesPattern(fullName, pattern) {
			return true
		}
	}
	return false
}

// matchesPattern checks if a repo name matches a glob-like pattern.
// Supports:
//   - Exact match: "owner/repo"
//   - Wildcard suffix: "owner/*" matches all repos from owner
//   - Wildcard prefix: "*/repo" matches repo from any owner
//   - Full wildcard: "*/*" matches everything
//
// Names must be valid "owner/repo" format (exactly one slash).
func matchesPattern(name, pattern string) bool {
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
