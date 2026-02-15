// Package discovery provides topic-based repository discovery for github-radar.
package discovery

import (
	"context"
	"fmt"
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
	GrowthScore       float64
	NormalizedScore   float64
	ShouldAutoTrack   bool
	AlreadyTracked    bool
	Excluded          bool
}

// Result contains the results of a discovery run.
type Result struct {
	Topic           string
	StartTime       time.Time
	EndTime         time.Time
	TotalFound      int
	AfterFilters    int
	NewRepos        int
	AutoTracked     int
	AlreadyTracked  int
	Excluded        int
	Repos           []DiscoveredRepo
}

// Discoverer handles repository discovery.
type Discoverer struct {
	client     *github.Client
	store      *state.Store
	calculator *scoring.Calculator
	config     Config
	onLog      func(level, msg string, args ...interface{})
}

// NewDiscoverer creates a new discoverer.
func NewDiscoverer(client *github.Client, store *state.Store, config Config) *Discoverer {
	return &Discoverer{
		client:     client,
		store:      store,
		calculator: scoring.NewCalculatorWithDefaults(),
		config:     config,
	}
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

// DiscoverAll discovers repositories for all configured topics.
func (d *Discoverer) DiscoverAll(ctx context.Context) ([]*Result, error) {
	if len(d.config.Topics) == 0 {
		d.log("info", "No topics configured, skipping discovery")
		return nil, nil
	}

	var results []*Result
	for _, topic := range d.config.Topics {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		result, err := d.DiscoverTopic(ctx, topic)
		if err != nil {
			d.log("warn", "Discovery failed for topic", "topic", topic, "error", err)
			continue
		}
		results = append(results, result)
	}

	return results, nil
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

	// Max age filter
	if d.config.MaxAgeDays > 0 {
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
// Supports * as wildcard.
func matchesPattern(name, pattern string) bool {
	// Simple exact match for now
	// TODO: Implement glob matching
	return name == pattern
}
