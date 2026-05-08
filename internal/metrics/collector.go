package metrics

import (
	"context"
	"time"
)

type RepoRef struct {
	Owner string
	Name  string
}

type CollectedMetrics struct {
	Owner             string
	Name              string
	Stars             int
	Forks             int
	OpenIssues        int
	OpenPRs           int
	Contributors      int
	MergedPRs7d       int
	NewIssues7d       int
	LatestReleaseAt   time.Time
	ReleaseDates      []time.Time
	CollectedAt       time.Time
	StarVelocity      float64
	StarAcceleration  float64
	ForkVelocity      float64
	ReleaseCadence    float64
	PRVelocity        float64
	IssueVelocity     float64
	ContributorGrowth float64
	GrowthScore       float64

	// Partial marks this result as containing only delta/derived metrics
	// (e.g. from gharchive.org fallback).  When true, UpdateStoreFromCollected
	// preserves the previous absolute counts (Stars, Forks, Contributors,
	// GrowthScore) and only updates velocity/cadence fields.  This prevents
	// a fallback collector from overwriting correct absolute values with
	// near-zero delta counts.
	Partial bool
}

type MetricsCollector interface {
	Collect(ctx context.Context, repos []RepoRef, window time.Duration) ([]CollectedMetrics, error)
}
