# Story 4.4: Calculate Issue Velocity

Status: done

## Story

As the **system**,
I want to calculate new issues per day,
So that I can measure community engagement.

## Acceptance Criteria

1. **Given** issues opened in last 7 days
   **When** velocity is calculated
   **Then** issue_velocity = new_issues / 7

2. **Given** high issue velocity
   **When** interpreting result
   **Then** it can indicate popularity OR problems

3. **Given** any issue count
   **When** velocity is calculated
   **Then** value is normalized to daily rate

## Tasks / Subtasks

- [x] Task 1: Implement CalculateIssueVelocity function
- [x] Task 2: Normalize to daily rate (divide by 7)
- [x] Task 3: Store NewIssues7d in state
- [x] Task 4: Write unit tests

## Completion Notes

- CalculateIssueVelocity normalizes 7-day issue count to daily rate
- High velocity is ambiguous (could be popularity or problems)
- Default weight is 0.5 (lowest) to reduce impact
- Stored in RepoState.IssueVelocity

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/scoring/score.go (CalculateIssueVelocity)
- internal/scoring/score_test.go (TestCalculateIssueVelocity)
- internal/state/store.go (NewIssues7d, IssueVelocity fields)
