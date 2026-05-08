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
}

type MetricsCollector interface {
	Collect(ctx context.Context, repos []RepoRef, window time.Duration) ([]CollectedMetrics, error)
}
