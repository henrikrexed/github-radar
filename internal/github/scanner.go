package github

import (
	"context"
	"fmt"
	"time"

	"github.com/hrexed/github-radar/internal/scoring"
	"github.com/hrexed/github-radar/internal/state"
)

// Scanner orchestrates repository data collection with state persistence.
type Scanner struct {
	client     *Client
	collector  *Collector
	store      *state.Store
	calculator *scoring.Calculator
	onLog      func(level, msg string, args ...interface{})
}

// NewScanner creates a new scanner with the given client and state store.
func NewScanner(client *Client, store *state.Store) *Scanner {
	collector := NewCollector(client)

	return &Scanner{
		client:     client,
		collector:  collector,
		store:      store,
		calculator: scoring.NewCalculatorWithDefaults(),
	}
}

// SetScoringWeights sets custom scoring weights.
func (s *Scanner) SetScoringWeights(weights scoring.Weights) {
	s.calculator = scoring.NewCalculator(weights)
}

// SetLogger sets a logging callback.
func (s *Scanner) SetLogger(fn func(level, msg string, args ...interface{})) {
	s.onLog = fn
}

func (s *Scanner) log(level, msg string, args ...interface{}) {
	if s.onLog != nil {
		s.onLog(level, msg, args...)
	}
}

// ScanResult contains the result of a scan operation.
type ScanResult struct {
	StartTime   time.Time
	EndTime     time.Time
	Total       int
	Successful  int
	Failed      int
	Skipped     int
	Updated     int
	FailedRepos []string
}

// Repo represents a repository to scan.
type Repo struct {
	Owner string
	Name  string
}

// Scan collects data for the given repositories and updates state.
func (s *Scanner) Scan(ctx context.Context, repos []Repo) (*ScanResult, error) {
	result := &ScanResult{
		StartTime: time.Now(),
	}

	s.log("info", "Starting scan", "repo_count", len(repos))

	for _, repo := range repos {
		select {
		case <-ctx.Done():
			s.log("warn", "Scan cancelled", "completed", result.Successful+result.Failed)
			result.EndTime = time.Now()
			return result, ctx.Err()
		default:
		}

		result.Total++

		// Get previous state for conditional requests and velocity calculation
		prevState := s.store.GetRepoState(fmt.Sprintf("%s/%s", repo.Owner, repo.Name))
		var cond *ConditionalInfo
		if prevState != nil {
			cond = &ConditionalInfo{
				ETag:         prevState.ETag,
				LastModified: prevState.LastModified,
			}
		}

		// Collect data
		collResult := s.collector.CollectRepoConditional(ctx, repo.Owner, repo.Name, cond)

		if collResult.Error != nil {
			result.Failed++
			result.FailedRepos = append(result.FailedRepos, collResult.FullName)
			s.log("warn", "Failed to collect repo", "repo", collResult.FullName, "error", collResult.Error)
			continue
		}

		if collResult.Skipped {
			result.Skipped++
			s.log("debug", "Repo unchanged (304)", "repo", collResult.FullName)
			continue
		}

		result.Successful++

		// Update state with new data
		s.updateRepoState(repo.Owner, repo.Name, &collResult, prevState)
		result.Updated++
	}

	// Save state
	s.store.SetLastScan(time.Now())
	if err := s.store.Save(); err != nil {
		s.log("error", "Failed to save state", "error", err)
		// Continue even if save fails - we have the results
	}

	result.EndTime = time.Now()
	s.log("info", "Scan complete",
		"total", result.Total,
		"successful", result.Successful,
		"failed", result.Failed,
		"skipped", result.Skipped,
		"duration", result.EndTime.Sub(result.StartTime))

	return result, nil
}

// updateRepoState updates the state store with collection results.
func (s *Scanner) updateRepoState(owner, name string, result *CollectionResult, prev *state.RepoState) {
	fullName := fmt.Sprintf("%s/%s", owner, name)

	newState := state.RepoState{
		Owner:         owner,
		Name:          name,
		LastCollected: result.Collected,
	}

	// Store conditional request info for future requests
	if result.ConditionInfo != nil {
		newState.ETag = result.ConditionInfo.ETag
		newState.LastModified = result.ConditionInfo.LastModified
	}

	if result.Metrics != nil {
		newState.Stars = result.Metrics.Stars
		newState.Forks = result.Metrics.Forks
	}

	if result.Activity != nil {
		newState.Contributors = result.Activity.Contributors
		newState.MergedPRs7d = result.Activity.MergedPRs7d
		newState.NewIssues7d = result.Activity.NewIssues7d
	}

	// Build scoring metrics from current and previous data
	metrics := scoring.RepoMetrics{
		Stars:        newState.Stars,
		Forks:        newState.Forks,
		Contributors: newState.Contributors,
		MergedPRs7d:  newState.MergedPRs7d,
		NewIssues7d:  newState.NewIssues7d,
	}

	// Include previous state for velocity calculations
	if prev != nil && !prev.LastCollected.IsZero() {
		metrics.StarsPrev = prev.Stars
		metrics.ContributorsPrev = prev.Contributors
		metrics.DaysElapsed = result.Collected.Sub(prev.LastCollected).Hours() / 24
		metrics.PrevStarVelocity = prev.StarVelocity

		// Store previous values for reference
		newState.StarsPrev = prev.Stars
		newState.ContributorsPrev = prev.Contributors
	}

	// Calculate all velocities using the scoring calculator
	velocities := s.calculator.CalculateVelocities(metrics)
	newState.StarVelocity = velocities.StarVelocity
	newState.StarAcceleration = velocities.StarAcceleration
	newState.PRVelocity = velocities.PRVelocity
	newState.IssueVelocity = velocities.IssueVelocity
	newState.ContributorGrowth = velocities.ContributorGrowth

	// Calculate raw growth score
	newState.GrowthScore = s.calculator.CalculateRawScore(velocities)

	s.store.SetRepoState(fullName, newState)
}

// NormalizeAllScores normalizes growth scores across all tracked repos.
// This should be called after a scan to update normalized scores.
func (s *Scanner) NormalizeAllScores() {
	allStates := s.store.AllRepoStates()
	if len(allStates) == 0 {
		return
	}

	// Convert to scored repos for normalization
	repos := make([]scoring.ScoredRepo, 0, len(allStates))
	for fullName, repoState := range allStates {
		repos = append(repos, scoring.ScoredRepo{
			FullName: fullName,
			RawScore: repoState.GrowthScore,
		})
	}

	// Normalize scores
	normalized := scoring.NormalizeScores(repos)

	// Update state with normalized scores
	for _, scored := range normalized {
		if repoState := s.store.GetRepoState(scored.FullName); repoState != nil {
			repoState.NormalizedGrowthScore = scored.NormalizedScore
			s.store.SetRepoState(scored.FullName, *repoState)
		}
	}
}

// GetTopRepos returns the top N repos by normalized growth score.
func (s *Scanner) GetTopRepos(n int) []scoring.ScoredRepo {
	allStates := s.store.AllRepoStates()
	if len(allStates) == 0 {
		return nil
	}

	repos := make([]scoring.ScoredRepo, 0, len(allStates))
	for fullName, repoState := range allStates {
		repos = append(repos, scoring.ScoredRepo{
			FullName: fullName,
			Velocities: scoring.VelocityMetrics{
				StarVelocity:      repoState.StarVelocity,
				StarAcceleration:  repoState.StarAcceleration,
				PRVelocity:        repoState.PRVelocity,
				IssueVelocity:     repoState.IssueVelocity,
				ContributorGrowth: repoState.ContributorGrowth,
			},
			RawScore:        repoState.GrowthScore,
			NormalizedScore: repoState.NormalizedGrowthScore,
		})
	}

	return scoring.TopN(repos, n)
}

// GetClient returns the underlying GitHub client.
func (s *Scanner) GetClient() *Client {
	return s.client
}

// GetStore returns the state store.
func (s *Scanner) GetStore() *state.Store {
	return s.store
}
