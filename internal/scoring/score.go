// Package scoring provides growth score calculation for github-radar.
package scoring

import (
	"math"
	"sort"
)

// Weights contains the configurable weights for composite scoring.
type Weights struct {
	StarVelocity      float64
	StarAcceleration  float64
	ContributorGrowth float64
	PRVelocity        float64
	IssueVelocity     float64
}

// DefaultWeights returns the default scoring weights.
// Default weights: star=2.0, accel=3.0, contrib=1.5, pr=1.0, issue=0.5
func DefaultWeights() Weights {
	return Weights{
		StarVelocity:      2.0,
		StarAcceleration:  3.0,
		ContributorGrowth: 1.5,
		PRVelocity:        1.0,
		IssueVelocity:     0.5,
	}
}

// RepoMetrics contains all the input metrics needed for scoring.
type RepoMetrics struct {
	// Current values
	Stars        int
	Forks        int
	Contributors int

	// Previous values (for velocity calculation)
	StarsPrev        int
	ContributorsPrev int

	// Activity metrics (7-day window)
	MergedPRs7d int
	NewIssues7d int

	// Time elapsed since last measurement (in days)
	DaysElapsed float64

	// Previous velocities (for acceleration)
	PrevStarVelocity float64
}

// VelocityMetrics contains calculated velocity values.
type VelocityMetrics struct {
	StarVelocity      float64 // Stars gained per day
	StarAcceleration  float64 // Change in star velocity
	PRVelocity        float64 // PRs merged per day
	IssueVelocity     float64 // Issues opened per day
	ContributorGrowth float64 // Contributors gained per day
}

// ScoredRepo contains a repository with its calculated scores.
type ScoredRepo struct {
	FullName        string
	Velocities      VelocityMetrics
	RawScore        float64 // Weighted composite score
	NormalizedScore float64 // 0-100 normalized score
}

// Calculator calculates growth scores for repositories.
type Calculator struct {
	weights Weights
}

// NewCalculator creates a new score calculator with the given weights.
func NewCalculator(weights Weights) *Calculator {
	return &Calculator{weights: weights}
}

// NewCalculatorWithDefaults creates a calculator with default weights.
func NewCalculatorWithDefaults() *Calculator {
	return NewCalculator(DefaultWeights())
}

// CalculateVelocities calculates all velocity metrics for a repository.
func (c *Calculator) CalculateVelocities(metrics RepoMetrics) VelocityMetrics {
	v := VelocityMetrics{}

	// Star velocity: (current_stars - previous_stars) / days_elapsed
	// First-time repos use 0 as baseline
	v.StarVelocity = CalculateStarVelocity(metrics.Stars, metrics.StarsPrev, metrics.DaysElapsed)

	// Star acceleration: current_velocity - previous_velocity
	// Requires at least 2 data points (else 0)
	v.StarAcceleration = CalculateStarAcceleration(v.StarVelocity, metrics.PrevStarVelocity)

	// PR velocity: merged_prs / 7 (normalized to daily rate)
	v.PRVelocity = CalculatePRVelocity(metrics.MergedPRs7d)

	// Issue velocity: new_issues / 7 (normalized to daily rate)
	v.IssueVelocity = CalculateIssueVelocity(metrics.NewIssues7d)

	// Contributor growth: (current - previous) / days_elapsed
	v.ContributorGrowth = CalculateContributorGrowth(
		metrics.Contributors, metrics.ContributorsPrev, metrics.DaysElapsed)

	return v
}

// CalculateRawScore computes the weighted composite growth score.
func (c *Calculator) CalculateRawScore(v VelocityMetrics) float64 {
	// Formula:
	// growth_score = (star_velocity × weight_star) +
	//                (star_acceleration × weight_accel) +
	//                (contributor_growth × weight_contrib) +
	//                (pr_velocity × weight_pr) +
	//                (issue_velocity × weight_issue)
	return (v.StarVelocity * c.weights.StarVelocity) +
		(v.StarAcceleration * c.weights.StarAcceleration) +
		(v.ContributorGrowth * c.weights.ContributorGrowth) +
		(v.PRVelocity * c.weights.PRVelocity) +
		(v.IssueVelocity * c.weights.IssueVelocity)
}

// Score calculates the complete score for a repository.
func (c *Calculator) Score(fullName string, metrics RepoMetrics) ScoredRepo {
	velocities := c.CalculateVelocities(metrics)
	rawScore := c.CalculateRawScore(velocities)

	return ScoredRepo{
		FullName:   fullName,
		Velocities: velocities,
		RawScore:   rawScore,
		// NormalizedScore is set later via NormalizeScores
	}
}

// ScoreAll calculates scores for multiple repositories and normalizes them.
func (c *Calculator) ScoreAll(repos map[string]RepoMetrics) []ScoredRepo {
	scored := make([]ScoredRepo, 0, len(repos))

	for fullName, metrics := range repos {
		scored = append(scored, c.Score(fullName, metrics))
	}

	// Normalize all scores to 0-100 range
	return NormalizeScores(scored)
}

// CalculateStarVelocity calculates stars gained per day.
// First-time repos (no previous data) use 0 as baseline.
// Negative velocity (star loss) is captured accurately.
func CalculateStarVelocity(currentStars, previousStars int, daysElapsed float64) float64 {
	if daysElapsed <= 0 {
		return 0
	}
	return float64(currentStars-previousStars) / daysElapsed
}

// CalculateStarAcceleration calculates the change in velocity.
// Requires at least 2 data points (else returns 0).
// Positive acceleration indicates speeding up.
func CalculateStarAcceleration(currentVelocity, previousVelocity float64) float64 {
	return currentVelocity - previousVelocity
}

// CalculatePRVelocity calculates PRs merged per day.
// Takes PRs merged in last 7 days and normalizes to daily rate.
func CalculatePRVelocity(mergedPRs7d int) float64 {
	return float64(mergedPRs7d) / 7.0
}

// CalculateIssueVelocity calculates issues opened per day.
// Takes issues opened in last 7 days and normalizes to daily rate.
// Note: High issue velocity can indicate popularity OR problems.
func CalculateIssueVelocity(newIssues7d int) float64 {
	return float64(newIssues7d) / 7.0
}

// CalculateContributorGrowth calculates contributors gained per day.
// New repos use current count as baseline (returns 0).
func CalculateContributorGrowth(currentContribs, previousContribs int, daysElapsed float64) float64 {
	if daysElapsed <= 0 {
		return 0
	}
	return float64(currentContribs-previousContribs) / daysElapsed
}

// NormalizeScores normalizes raw scores to a 0-100 scale.
// Uses min-max scaling where:
// - Score of 100 represents highest growth in the set
// - Negative raw scores map to low end (0-20 range)
func NormalizeScores(repos []ScoredRepo) []ScoredRepo {
	if len(repos) == 0 {
		return repos
	}

	if len(repos) == 1 {
		// Single repo gets score of 50 (middle)
		repos[0].NormalizedScore = 50.0
		return repos
	}

	// Find min and max raw scores
	minScore := repos[0].RawScore
	maxScore := repos[0].RawScore
	for _, r := range repos[1:] {
		if r.RawScore < minScore {
			minScore = r.RawScore
		}
		if r.RawScore > maxScore {
			maxScore = r.RawScore
		}
	}

	// Handle case where all scores are the same
	scoreRange := maxScore - minScore
	if scoreRange == 0 {
		for i := range repos {
			repos[i].NormalizedScore = 50.0
		}
		return repos
	}

	// Normalize to 0-100 using min-max scaling
	for i := range repos {
		normalized := ((repos[i].RawScore - minScore) / scoreRange) * 100.0

		// Ensure bounds
		if normalized < 0 {
			normalized = 0
		}
		if normalized > 100 {
			normalized = 100
		}

		repos[i].NormalizedScore = math.Round(normalized*100) / 100 // Round to 2 decimal places
	}

	return repos
}

// NormalizeScoresPercentile normalizes using percentile ranking.
// This is an alternative to min-max that's more robust to outliers.
func NormalizeScoresPercentile(repos []ScoredRepo) []ScoredRepo {
	if len(repos) == 0 {
		return repos
	}

	if len(repos) == 1 {
		repos[0].NormalizedScore = 50.0
		return repos
	}

	// Create sorted copy of indices by raw score
	indices := make([]int, len(repos))
	for i := range indices {
		indices[i] = i
	}
	sort.Slice(indices, func(i, j int) bool {
		return repos[indices[i]].RawScore < repos[indices[j]].RawScore
	})

	// Assign percentile ranks
	n := float64(len(repos))
	for rank, idx := range indices {
		// Percentile = (rank / (n-1)) * 100
		percentile := (float64(rank) / (n - 1)) * 100.0
		repos[idx].NormalizedScore = math.Round(percentile*100) / 100
	}

	return repos
}

// RankByScore sorts scored repos by normalized score (highest first).
func RankByScore(repos []ScoredRepo) []ScoredRepo {
	sorted := make([]ScoredRepo, len(repos))
	copy(sorted, repos)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].NormalizedScore > sorted[j].NormalizedScore
	})
	return sorted
}

// TopN returns the top N repos by normalized score.
func TopN(repos []ScoredRepo, n int) []ScoredRepo {
	ranked := RankByScore(repos)
	if n >= len(ranked) {
		return ranked
	}
	return ranked[:n]
}
