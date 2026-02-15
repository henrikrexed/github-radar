package scoring

import (
	"math"
	"testing"
)

func TestDefaultWeights(t *testing.T) {
	w := DefaultWeights()

	if w.StarVelocity != 2.0 {
		t.Errorf("StarVelocity = %v, want 2.0", w.StarVelocity)
	}
	if w.StarAcceleration != 3.0 {
		t.Errorf("StarAcceleration = %v, want 3.0", w.StarAcceleration)
	}
	if w.ContributorGrowth != 1.5 {
		t.Errorf("ContributorGrowth = %v, want 1.5", w.ContributorGrowth)
	}
	if w.PRVelocity != 1.0 {
		t.Errorf("PRVelocity = %v, want 1.0", w.PRVelocity)
	}
	if w.IssueVelocity != 0.5 {
		t.Errorf("IssueVelocity = %v, want 0.5", w.IssueVelocity)
	}
}

func TestCalculateStarVelocity(t *testing.T) {
	tests := []struct {
		name         string
		current      int
		previous     int
		daysElapsed  float64
		wantVelocity float64
	}{
		{
			name:         "positive growth",
			current:      200,
			previous:     100,
			daysElapsed:  7.0,
			wantVelocity: 100.0 / 7.0,
		},
		{
			name:         "negative growth (star loss)",
			current:      90,
			previous:     100,
			daysElapsed:  10.0,
			wantVelocity: -10.0 / 10.0,
		},
		{
			name:         "no change",
			current:      100,
			previous:     100,
			daysElapsed:  7.0,
			wantVelocity: 0,
		},
		{
			name:         "first-time repo (zero previous)",
			current:      100,
			previous:     0,
			daysElapsed:  7.0,
			wantVelocity: 100.0 / 7.0,
		},
		{
			name:         "zero days elapsed",
			current:      200,
			previous:     100,
			daysElapsed:  0,
			wantVelocity: 0,
		},
		{
			name:         "negative days elapsed",
			current:      200,
			previous:     100,
			daysElapsed:  -1,
			wantVelocity: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateStarVelocity(tt.current, tt.previous, tt.daysElapsed)
			if math.Abs(got-tt.wantVelocity) > 0.0001 {
				t.Errorf("CalculateStarVelocity() = %v, want %v", got, tt.wantVelocity)
			}
		})
	}
}

func TestCalculateStarAcceleration(t *testing.T) {
	tests := []struct {
		name         string
		current      float64
		previous     float64
		wantAccel    float64
	}{
		{
			name:      "speeding up",
			current:   15.0,
			previous:  10.0,
			wantAccel: 5.0,
		},
		{
			name:      "slowing down",
			current:   5.0,
			previous:  10.0,
			wantAccel: -5.0,
		},
		{
			name:      "no change",
			current:   10.0,
			previous:  10.0,
			wantAccel: 0,
		},
		{
			name:      "first measurement (no previous)",
			current:   10.0,
			previous:  0,
			wantAccel: 10.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateStarAcceleration(tt.current, tt.previous)
			if math.Abs(got-tt.wantAccel) > 0.0001 {
				t.Errorf("CalculateStarAcceleration() = %v, want %v", got, tt.wantAccel)
			}
		})
	}
}

func TestCalculatePRVelocity(t *testing.T) {
	tests := []struct {
		name         string
		mergedPRs7d  int
		wantVelocity float64
	}{
		{
			name:         "normal activity",
			mergedPRs7d:  14,
			wantVelocity: 2.0,
		},
		{
			name:         "no PRs",
			mergedPRs7d:  0,
			wantVelocity: 0,
		},
		{
			name:         "high activity",
			mergedPRs7d:  70,
			wantVelocity: 10.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculatePRVelocity(tt.mergedPRs7d)
			if math.Abs(got-tt.wantVelocity) > 0.0001 {
				t.Errorf("CalculatePRVelocity() = %v, want %v", got, tt.wantVelocity)
			}
		})
	}
}

func TestCalculateIssueVelocity(t *testing.T) {
	tests := []struct {
		name         string
		newIssues7d  int
		wantVelocity float64
	}{
		{
			name:         "normal activity",
			newIssues7d:  21,
			wantVelocity: 3.0,
		},
		{
			name:         "no issues",
			newIssues7d:  0,
			wantVelocity: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateIssueVelocity(tt.newIssues7d)
			if math.Abs(got-tt.wantVelocity) > 0.0001 {
				t.Errorf("CalculateIssueVelocity() = %v, want %v", got, tt.wantVelocity)
			}
		})
	}
}

func TestCalculateContributorGrowth(t *testing.T) {
	tests := []struct {
		name        string
		current     int
		previous    int
		daysElapsed float64
		wantGrowth  float64
	}{
		{
			name:        "new contributors",
			current:     20,
			previous:    15,
			daysElapsed: 7.0,
			wantGrowth:  5.0 / 7.0,
		},
		{
			name:        "no change",
			current:     20,
			previous:    20,
			daysElapsed: 7.0,
			wantGrowth:  0,
		},
		{
			name:        "new repo (no previous)",
			current:     10,
			previous:    0,
			daysElapsed: 7.0,
			wantGrowth:  10.0 / 7.0,
		},
		{
			name:        "zero days elapsed",
			current:     20,
			previous:    10,
			daysElapsed: 0,
			wantGrowth:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateContributorGrowth(tt.current, tt.previous, tt.daysElapsed)
			if math.Abs(got-tt.wantGrowth) > 0.0001 {
				t.Errorf("CalculateContributorGrowth() = %v, want %v", got, tt.wantGrowth)
			}
		})
	}
}

func TestCalculator_CalculateVelocities(t *testing.T) {
	calc := NewCalculatorWithDefaults()

	metrics := RepoMetrics{
		Stars:            200,
		StarsPrev:        100,
		Contributors:     20,
		ContributorsPrev: 15,
		MergedPRs7d:      14,
		NewIssues7d:      21,
		DaysElapsed:      7.0,
		PrevStarVelocity: 10.0,
	}

	v := calc.CalculateVelocities(metrics)

	// Star velocity: 100/7 ≈ 14.29
	expectedStarVel := 100.0 / 7.0
	if math.Abs(v.StarVelocity-expectedStarVel) > 0.01 {
		t.Errorf("StarVelocity = %v, want %v", v.StarVelocity, expectedStarVel)
	}

	// Star acceleration: 14.29 - 10 = 4.29
	expectedAccel := expectedStarVel - 10.0
	if math.Abs(v.StarAcceleration-expectedAccel) > 0.01 {
		t.Errorf("StarAcceleration = %v, want %v", v.StarAcceleration, expectedAccel)
	}

	// PR velocity: 14/7 = 2
	if math.Abs(v.PRVelocity-2.0) > 0.01 {
		t.Errorf("PRVelocity = %v, want 2.0", v.PRVelocity)
	}

	// Issue velocity: 21/7 = 3
	if math.Abs(v.IssueVelocity-3.0) > 0.01 {
		t.Errorf("IssueVelocity = %v, want 3.0", v.IssueVelocity)
	}

	// Contributor growth: 5/7 ≈ 0.71
	expectedContrib := 5.0 / 7.0
	if math.Abs(v.ContributorGrowth-expectedContrib) > 0.01 {
		t.Errorf("ContributorGrowth = %v, want %v", v.ContributorGrowth, expectedContrib)
	}
}

func TestCalculator_CalculateRawScore(t *testing.T) {
	calc := NewCalculatorWithDefaults()

	v := VelocityMetrics{
		StarVelocity:      10.0,
		StarAcceleration:  5.0,
		PRVelocity:        2.0,
		IssueVelocity:     3.0,
		ContributorGrowth: 1.0,
	}

	// Expected: (10*2.0) + (5*3.0) + (1*1.5) + (2*1.0) + (3*0.5)
	// = 20 + 15 + 1.5 + 2 + 1.5 = 40
	expected := 40.0
	got := calc.CalculateRawScore(v)

	if math.Abs(got-expected) > 0.01 {
		t.Errorf("CalculateRawScore() = %v, want %v", got, expected)
	}
}

func TestCalculator_Score(t *testing.T) {
	calc := NewCalculatorWithDefaults()

	metrics := RepoMetrics{
		Stars:            200,
		StarsPrev:        100,
		Contributors:     20,
		ContributorsPrev: 15,
		MergedPRs7d:      14,
		NewIssues7d:      7,
		DaysElapsed:      7.0,
		PrevStarVelocity: 5.0,
	}

	scored := calc.Score("test/repo", metrics)

	if scored.FullName != "test/repo" {
		t.Errorf("FullName = %v, want test/repo", scored.FullName)
	}

	if scored.RawScore == 0 {
		t.Error("RawScore should not be 0 for this input")
	}

	// NormalizedScore should be 0 since normalization happens in ScoreAll
	if scored.NormalizedScore != 0 {
		t.Errorf("NormalizedScore = %v, want 0 (before normalization)", scored.NormalizedScore)
	}
}

func TestNormalizeScores(t *testing.T) {
	tests := []struct {
		name       string
		repos      []ScoredRepo
		wantScores []float64
	}{
		{
			name:       "empty list",
			repos:      []ScoredRepo{},
			wantScores: []float64{},
		},
		{
			name: "single repo gets 50",
			repos: []ScoredRepo{
				{FullName: "a", RawScore: 100},
			},
			wantScores: []float64{50.0},
		},
		{
			name: "two repos min-max",
			repos: []ScoredRepo{
				{FullName: "a", RawScore: 0},
				{FullName: "b", RawScore: 100},
			},
			wantScores: []float64{0, 100},
		},
		{
			name: "three repos spread",
			repos: []ScoredRepo{
				{FullName: "a", RawScore: 0},
				{FullName: "b", RawScore: 50},
				{FullName: "c", RawScore: 100},
			},
			wantScores: []float64{0, 50, 100},
		},
		{
			name: "all same score",
			repos: []ScoredRepo{
				{FullName: "a", RawScore: 50},
				{FullName: "b", RawScore: 50},
				{FullName: "c", RawScore: 50},
			},
			wantScores: []float64{50, 50, 50},
		},
		{
			name: "negative scores",
			repos: []ScoredRepo{
				{FullName: "a", RawScore: -50},
				{FullName: "b", RawScore: 0},
				{FullName: "c", RawScore: 50},
			},
			wantScores: []float64{0, 50, 100},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeScores(tt.repos)

			if len(result) != len(tt.wantScores) {
				t.Fatalf("got %d results, want %d", len(result), len(tt.wantScores))
			}

			for i, r := range result {
				if math.Abs(r.NormalizedScore-tt.wantScores[i]) > 0.01 {
					t.Errorf("repo[%d].NormalizedScore = %v, want %v",
						i, r.NormalizedScore, tt.wantScores[i])
				}
			}
		})
	}
}

func TestNormalizeScoresPercentile(t *testing.T) {
	repos := []ScoredRepo{
		{FullName: "a", RawScore: 10},
		{FullName: "b", RawScore: 20},
		{FullName: "c", RawScore: 30},
		{FullName: "d", RawScore: 1000}, // Outlier
	}

	result := NormalizeScoresPercentile(repos)

	// Check that outlier doesn't compress others
	// Percentile ranks: a=0%, b=33.33%, c=66.67%, d=100%
	if result[0].NormalizedScore > 1 {
		t.Errorf("lowest percentile = %v, want ~0", result[0].NormalizedScore)
	}
	if result[3].NormalizedScore < 99 {
		t.Errorf("highest percentile = %v, want ~100", result[3].NormalizedScore)
	}
}

func TestCalculator_ScoreAll(t *testing.T) {
	calc := NewCalculatorWithDefaults()

	repos := map[string]RepoMetrics{
		"low/growth": {
			Stars:       110,
			StarsPrev:   100,
			DaysElapsed: 7.0,
		},
		"high/growth": {
			Stars:            500,
			StarsPrev:        100,
			Contributors:     50,
			ContributorsPrev: 10,
			MergedPRs7d:      70,
			NewIssues7d:      35,
			DaysElapsed:      7.0,
			PrevStarVelocity: 10.0,
		},
	}

	scored := calc.ScoreAll(repos)

	if len(scored) != 2 {
		t.Fatalf("got %d results, want 2", len(scored))
	}

	// Find repos by name
	var lowGrowth, highGrowth *ScoredRepo
	for i := range scored {
		if scored[i].FullName == "low/growth" {
			lowGrowth = &scored[i]
		}
		if scored[i].FullName == "high/growth" {
			highGrowth = &scored[i]
		}
	}

	if lowGrowth == nil || highGrowth == nil {
		t.Fatal("could not find expected repos")
	}

	// High growth should have higher normalized score
	if highGrowth.NormalizedScore <= lowGrowth.NormalizedScore {
		t.Errorf("high growth (%v) should score higher than low growth (%v)",
			highGrowth.NormalizedScore, lowGrowth.NormalizedScore)
	}
}

func TestRankByScore(t *testing.T) {
	repos := []ScoredRepo{
		{FullName: "c", NormalizedScore: 30},
		{FullName: "a", NormalizedScore: 90},
		{FullName: "b", NormalizedScore: 60},
	}

	ranked := RankByScore(repos)

	if ranked[0].FullName != "a" {
		t.Errorf("first = %v, want a", ranked[0].FullName)
	}
	if ranked[1].FullName != "b" {
		t.Errorf("second = %v, want b", ranked[1].FullName)
	}
	if ranked[2].FullName != "c" {
		t.Errorf("third = %v, want c", ranked[2].FullName)
	}
}

func TestTopN(t *testing.T) {
	repos := []ScoredRepo{
		{FullName: "a", NormalizedScore: 90},
		{FullName: "b", NormalizedScore: 80},
		{FullName: "c", NormalizedScore: 70},
		{FullName: "d", NormalizedScore: 60},
		{FullName: "e", NormalizedScore: 50},
	}

	top3 := TopN(repos, 3)

	if len(top3) != 3 {
		t.Fatalf("got %d results, want 3", len(top3))
	}

	if top3[0].FullName != "a" || top3[1].FullName != "b" || top3[2].FullName != "c" {
		t.Errorf("unexpected order: %v, %v, %v",
			top3[0].FullName, top3[1].FullName, top3[2].FullName)
	}

	// Test with n > len
	topAll := TopN(repos, 10)
	if len(topAll) != 5 {
		t.Errorf("got %d results, want 5 (all repos)", len(topAll))
	}
}

func TestCustomWeights(t *testing.T) {
	// Test with custom weights that heavily prioritize PR velocity
	weights := Weights{
		StarVelocity:      0.1, // Low weight for stars
		StarAcceleration:  0.1,
		ContributorGrowth: 0.1,
		PRVelocity:        100.0, // Very high weight
		IssueVelocity:     0.1,
	}

	calc := NewCalculator(weights)

	// Two repos: one with high stars, one with high PRs
	highStars := RepoMetrics{
		Stars:       1000,
		StarsPrev:   500,
		DaysElapsed: 7.0,
		MergedPRs7d: 7, // 1/day
	}

	highPRs := RepoMetrics{
		Stars:       110,
		StarsPrev:   100,
		DaysElapsed: 7.0,
		MergedPRs7d: 70, // 10/day
	}

	scoreHighStars := calc.Score("high/stars", highStars)
	scoreHighPRs := calc.Score("high/prs", highPRs)

	// With PR weight of 100, high PR repo should score higher
	// highPRs: PR velocity = 10/day × 100 = 1000
	// highStars: PR velocity = 1/day × 100 = 100
	if scoreHighPRs.RawScore <= scoreHighStars.RawScore {
		t.Errorf("high PR repo (%v) should score higher than high stars repo (%v) with PR weight=100",
			scoreHighPRs.RawScore, scoreHighStars.RawScore)
	}
}
