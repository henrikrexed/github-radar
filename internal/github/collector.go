package github

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// CollectionResult contains the result of collecting data for a repository.
type CollectionResult struct {
	Owner         string
	Name          string
	FullName      string
	Metrics       *RepoMetrics
	Activity      *ActivityMetrics
	Collected     time.Time
	Error         error
	Skipped       bool             // True if skipped due to conditional request (304)
	ConditionInfo *ConditionalInfo // ETag/Last-Modified for future conditional requests
}

// CollectionSummary summarizes a collection run.
type CollectionSummary struct {
	StartTime   time.Time
	EndTime     time.Time
	Total       int
	Successful  int
	Failed      int
	Skipped     int
	Results     []CollectionResult
	FailedRepos []string
}

// ErrorHandler is called when a repository collection fails.
type ErrorHandler func(owner, name string, err error)

// Collector handles collecting data for multiple repositories.
type Collector struct {
	client          *Client
	onError         ErrorHandler
	collectPRs      bool
	collectActivity bool
}

// NewCollector creates a new repository data collector.
func NewCollector(client *Client) *Collector {
	return &Collector{
		client:          client,
		collectPRs:      true,
		collectActivity: true,
	}
}

// SetErrorHandler sets a callback for repository errors.
func (c *Collector) SetErrorHandler(handler ErrorHandler) {
	c.onError = handler
}

// SetCollectPRs enables or disables PR count collection.
func (c *Collector) SetCollectPRs(enabled bool) {
	c.collectPRs = enabled
}

// SetCollectActivity enables or disables activity metrics collection.
func (c *Collector) SetCollectActivity(enabled bool) {
	c.collectActivity = enabled
}

// CollectRepo collects all data for a single repository.
func (c *Collector) CollectRepo(ctx context.Context, owner, name string) CollectionResult {
	result := CollectionResult{
		Owner:     owner,
		Name:      name,
		FullName:  fmt.Sprintf("%s/%s", owner, name),
		Collected: time.Now(),
	}

	// Collect core metrics
	metrics, err := c.client.GetRepository(ctx, owner, name)
	if err != nil {
		result.Error = c.classifyError(owner, name, err)
		c.handleError(owner, name, result.Error)
		return result
	}
	result.Metrics = metrics

	// Collect open PR count
	if c.collectPRs {
		prCount, err := c.client.GetOpenPRCount(ctx, owner, name)
		if err != nil {
			// Log warning but continue
			c.handleError(owner, name, fmt.Errorf("fetching PR count: %w", err))
		} else {
			result.Metrics.OpenPRs = prCount
		}
	}

	// Collect activity metrics
	if c.collectActivity {
		activity, err := c.client.GetActivityMetrics(ctx, owner, name)
		if err != nil {
			// Log warning but continue
			c.handleError(owner, name, fmt.Errorf("fetching activity: %w", err))
		} else {
			result.Activity = activity
		}
	}

	return result
}

// CollectRepoConditional collects data with conditional request support.
func (c *Collector) CollectRepoConditional(ctx context.Context, owner, name string, cond *ConditionalInfo) CollectionResult {
	result := CollectionResult{
		Owner:     owner,
		Name:      name,
		FullName:  fmt.Sprintf("%s/%s", owner, name),
		Collected: time.Now(),
	}

	// Try conditional request first
	metrics, notModified, newInfo, err := c.client.GetRepositoryConditional(ctx, owner, name, cond)
	if err != nil {
		result.Error = c.classifyError(owner, name, err)
		c.handleError(owner, name, result.Error)
		return result
	}

	// Store the new conditional info for future requests
	result.ConditionInfo = newInfo

	if notModified {
		result.Skipped = true
		return result
	}

	result.Metrics = metrics

	// Collect additional data if core metrics succeeded
	if c.collectPRs && result.Metrics != nil {
		prCount, err := c.client.GetOpenPRCount(ctx, owner, name)
		if err == nil {
			result.Metrics.OpenPRs = prCount
		}
	}

	if c.collectActivity {
		activity, _ := c.client.GetActivityMetrics(ctx, owner, name)
		result.Activity = activity
	}

	return result
}

// CollectAll collects data for multiple repositories.
// Returns results for all repos, including failed ones.
func (c *Collector) CollectAll(ctx context.Context, repos []struct{ Owner, Name string }) CollectionSummary {
	summary := CollectionSummary{
		StartTime: time.Now(),
		Total:     len(repos),
		Results:   make([]CollectionResult, 0, len(repos)),
	}

	for i, repo := range repos {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			// Add remaining repos (including current one) as failed
			for j := i; j < len(repos); j++ {
				summary.Results = append(summary.Results, CollectionResult{
					Owner:    repos[j].Owner,
					Name:     repos[j].Name,
					FullName: fmt.Sprintf("%s/%s", repos[j].Owner, repos[j].Name),
					Error:    ctx.Err(),
				})
				summary.Failed++
				summary.FailedRepos = append(summary.FailedRepos,
					fmt.Sprintf("%s/%s", repos[j].Owner, repos[j].Name))
			}
			summary.EndTime = time.Now()
			return summary
		default:
		}

		result := c.CollectRepo(ctx, repo.Owner, repo.Name)
		summary.Results = append(summary.Results, result)

		if result.Error != nil {
			summary.Failed++
			summary.FailedRepos = append(summary.FailedRepos, result.FullName)
		} else if result.Skipped {
			summary.Skipped++
		} else {
			summary.Successful++
		}
	}

	summary.EndTime = time.Now()
	return summary
}

// classifyError wraps errors with context about the failure type.
func (c *Collector) classifyError(owner, name string, err error) error {
	fullName := fmt.Sprintf("%s/%s", owner, name)

	// Check for API errors (404, etc)
	if IsAPINotFound(err) {
		return &RepoError{
			Repo:    fullName,
			Type:    RepoErrorNotFound,
			Message: "repository not found (may be deleted or renamed)",
			Err:     err,
		}
	}

	// Check for HTTPError from retry logic
	if IsNotFoundError(err) {
		return &RepoError{
			Repo:    fullName,
			Type:    RepoErrorNotFound,
			Message: "repository not found (may be deleted or renamed)",
			Err:     err,
		}
	}

	if IsRateLimitError(err) {
		return &RepoError{
			Repo:    fullName,
			Type:    RepoErrorRateLimit,
			Message: "rate limit exceeded",
			Err:     err,
		}
	}

	if IsPermanentError(err) {
		return &RepoError{
			Repo:    fullName,
			Type:    RepoErrorPermanent,
			Message: "permanent error (will not retry)",
			Err:     err,
		}
	}

	return &RepoError{
		Repo:    fullName,
		Type:    RepoErrorTransient,
		Message: "transient error",
		Err:     err,
	}
}

// handleError calls the error handler if set.
func (c *Collector) handleError(owner, name string, err error) {
	if c.onError != nil {
		c.onError(owner, name, err)
	}
}

// RepoErrorType classifies repository errors.
type RepoErrorType int

const (
	RepoErrorTransient RepoErrorType = iota
	RepoErrorPermanent
	RepoErrorNotFound
	RepoErrorRateLimit
)

// RepoError wraps an error with repository context.
type RepoError struct {
	Repo    string
	Type    RepoErrorType
	Message string
	Err     error
}

func (e *RepoError) Error() string {
	return fmt.Sprintf("%s: %s: %v", e.Repo, e.Message, e.Err)
}

func (e *RepoError) Unwrap() error {
	return e.Err
}

// IsRepoNotFoundError checks if an error indicates a repository was not found.
func IsRepoNotFoundError(err error) bool {
	var repoErr *RepoError
	if errors.As(err, &repoErr) {
		return repoErr.Type == RepoErrorNotFound
	}
	return false
}
