package github

import (
	"context"
	"fmt"
	"time"

	"github.com/hrexed/github-radar/internal/state"
)

// Scanner orchestrates repository data collection with state persistence.
type Scanner struct {
	client    *Client
	collector *Collector
	store     *state.Store
	onLog     func(level, msg string, args ...interface{})
}

// NewScanner creates a new scanner with the given client and state store.
func NewScanner(client *Client, store *state.Store) *Scanner {
	collector := NewCollector(client)

	return &Scanner{
		client:    client,
		collector: collector,
		store:     store,
	}
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
	}

	// Calculate velocities if we have previous data
	if prev != nil && !prev.LastCollected.IsZero() {
		daysSince := result.Collected.Sub(prev.LastCollected).Hours() / 24
		if daysSince > 0 {
			// Star velocity (stars per day)
			newState.StarsPrev = prev.Stars
			starDelta := float64(newState.Stars - prev.Stars)
			newState.StarVelocity = starDelta / daysSince

			// Star acceleration (change in velocity)
			newState.StarAcceleration = newState.StarVelocity - prev.StarVelocity
		}
	}

	s.store.SetRepoState(fullName, newState)
}

// GetClient returns the underlying GitHub client.
func (s *Scanner) GetClient() *Client {
	return s.client
}

// GetStore returns the state store.
func (s *Scanner) GetStore() *state.Store {
	return s.store
}
