# Story 4.5: Calculate Contributor Growth

Status: done

## Story

As the **system**,
I want to track contributor count changes,
So that I can identify repos attracting new developers.

## Acceptance Criteria

1. **Given** current and previous contributor counts
   **When** growth is calculated
   **Then** contributor_growth = (current - previous) / days_elapsed

2. **Given** a new repo (no previous data)
   **When** growth is calculated
   **Then** current count is used as baseline (returns 0)

3. **Given** contributor data
   **When** collecting metrics
   **Then** data comes from GitHub API

## Tasks / Subtasks

- [x] Task 1: Implement CalculateContributorGrowth function
- [x] Task 2: Handle zero days elapsed
- [x] Task 3: Store previous contributor count in state
- [x] Task 4: Write unit tests

## Completion Notes

- CalculateContributorGrowth tracks new contributors per day
- ContributorsPrev stored in RepoState for tracking
- Returns 0 for first-time repos or zero elapsed time
- Integrated with Scanner.updateRepoState

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/scoring/score.go (CalculateContributorGrowth)
- internal/scoring/score_test.go (TestCalculateContributorGrowth)
- internal/state/store.go (ContributorsPrev, ContributorGrowth fields)
