# Story 4.3: Calculate PR Velocity

Status: done

## Story

As the **system**,
I want to calculate PRs merged per day,
So that I can measure development activity.

## Acceptance Criteria

1. **Given** PRs merged in last 7 days
   **When** velocity is calculated
   **Then** pr_velocity = merged_prs / 7

2. **Given** higher velocity
   **When** interpreting result
   **Then** it indicates active development

3. **Given** any PR count
   **When** velocity is calculated
   **Then** value is normalized to daily rate

## Tasks / Subtasks

- [x] Task 1: Implement CalculatePRVelocity function
- [x] Task 2: Normalize to daily rate (divide by 7)
- [x] Task 3: Store MergedPRs7d in state
- [x] Task 4: Write unit tests

## Completion Notes

- CalculatePRVelocity normalizes 7-day PR count to daily rate
- Stored in RepoState.PRVelocity after calculation
- MergedPRs7d from activity metrics used as input

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/scoring/score.go (CalculatePRVelocity)
- internal/scoring/score_test.go (TestCalculatePRVelocity)
- internal/state/store.go (MergedPRs7d, PRVelocity fields)
