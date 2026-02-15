# Story 4.1: Calculate Star Velocity

Status: done

## Story

As the **system**,
I want to calculate stars gained per day,
So that I can identify repos with growing popularity.

## Acceptance Criteria

1. **Given** current star count and previous star count from state
   **When** velocity is calculated
   **Then** star_velocity = (current_stars - previous_stars) / days_elapsed

2. **Given** a first-time repo
   **When** velocity is calculated
   **Then** 0 is used as baseline

3. **Given** a repo losing stars
   **When** velocity is calculated
   **Then** negative velocity (star loss) is captured accurately

## Tasks / Subtasks

- [x] Task 1: Implement CalculateStarVelocity function
- [x] Task 2: Handle zero/negative days elapsed
- [x] Task 3: Handle first-time repos (no previous data)
- [x] Task 4: Write unit tests

## Completion Notes

- CalculateStarVelocity function handles all edge cases
- Returns 0 for zero or negative days elapsed
- Negative velocity captured when stars decrease
- Integrated with Scanner.updateRepoState for automatic calculation

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/scoring/score.go (CalculateStarVelocity)
- internal/scoring/score_test.go (TestCalculateStarVelocity)
- internal/github/scanner.go (updateRepoState integration)
