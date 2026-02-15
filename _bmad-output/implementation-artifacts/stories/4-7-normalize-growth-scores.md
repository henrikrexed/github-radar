# Story 4.7: Normalize Growth Scores

Status: done

## Story

As the **system**,
I want to normalize scores to a 0-100 scale,
So that scores are comparable and dashboard-friendly.

## Acceptance Criteria

1. **Given** raw composite growth scores for all repos
   **When** normalization is applied
   **Then** scores are scaled to 0-100 range

2. **Given** normalization options
   **When** choosing method
   **Then** min-max or percentile scaling is available

3. **Given** the highest scoring repo
   **When** normalized
   **Then** score of 100 represents highest growth in the set

4. **Given** negative raw scores
   **When** normalized
   **Then** they map to low end (0-20 range)

## Tasks / Subtasks

- [x] Task 1: Implement NormalizeScores (min-max scaling)
- [x] Task 2: Implement NormalizeScoresPercentile (alternative)
- [x] Task 3: Handle edge cases (single repo, all same score)
- [x] Task 4: Implement Scanner.NormalizeAllScores
- [x] Task 5: Implement TopN and RankByScore helpers
- [x] Task 6: Write unit tests

## Completion Notes

- NormalizeScores uses min-max scaling (0-100)
- NormalizeScoresPercentile provides percentile-based alternative
- Single repo gets score of 50 (middle)
- All same scores get 50 (middle)
- RankByScore and TopN helpers for ranking
- Scanner.GetTopRepos returns top N by score

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/scoring/score.go (NormalizeScores, NormalizeScoresPercentile, RankByScore, TopN)
- internal/scoring/score_test.go (TestNormalizeScores, TestNormalizeScoresPercentile, TestRankByScore, TestTopN)
- internal/github/scanner.go (NormalizeAllScores, GetTopRepos)
- internal/state/store.go (NormalizedGrowthScore field)
