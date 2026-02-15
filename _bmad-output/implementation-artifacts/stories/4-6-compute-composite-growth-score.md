# Story 4.6: Compute Composite Growth Score

Status: done

## Story

As the **system**,
I want to calculate a weighted composite score,
So that I can rank repos by overall growth potential.

## Acceptance Criteria

1. **Given** all velocity metrics and configurable weights
   **When** composite score is calculated
   **Then** formula applies:
   ```
   growth_score = (star_velocity × weight_star) +
                  (star_acceleration × weight_accel) +
                  (contributor_growth × weight_contrib) +
                  (pr_velocity × weight_pr) +
                  (issue_velocity × weight_issue)
   ```

2. **Given** no custom weights
   **When** calculating score
   **Then** default weights: star=2.0, accel=3.0, contrib=1.5, pr=1.0, issue=0.5

3. **Given** custom weights in config
   **When** calculating score
   **Then** custom weights are used (FR44)

## Tasks / Subtasks

- [x] Task 1: Define Weights struct with defaults
- [x] Task 2: Implement Calculator.CalculateRawScore
- [x] Task 3: Integrate with config weights
- [x] Task 4: Write unit tests

## Completion Notes

- Calculator struct holds weights and provides scoring methods
- DefaultWeights() returns: star=2.0, accel=3.0, contrib=1.5, pr=1.0, issue=0.5
- Scanner.SetScoringWeights() allows custom weights from config
- GrowthScore stored in RepoState after each scan

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/scoring/score.go (Weights, Calculator, CalculateRawScore)
- internal/scoring/score_test.go (TestCalculator_CalculateRawScore, TestCustomWeights)
- internal/github/scanner.go (SetScoringWeights, calculator field)
