# Story 4.2: Calculate Star Acceleration

Status: done

## Story

As the **system**,
I want to calculate velocity change from previous period,
So that I can detect repos with accelerating growth.

## Acceptance Criteria

1. **Given** current velocity and previous velocity from state
   **When** acceleration is calculated
   **Then** star_acceleration = current_velocity - previous_velocity

2. **Given** positive acceleration
   **When** interpreting result
   **Then** it indicates speeding up (growth accelerating)

3. **Given** less than 2 data points
   **When** acceleration is calculated
   **Then** returns 0

## Tasks / Subtasks

- [x] Task 1: Implement CalculateStarAcceleration function
- [x] Task 2: Handle first measurement (no previous velocity)
- [x] Task 3: Store previous velocity in state
- [x] Task 4: Write unit tests

## Completion Notes

- CalculateStarAcceleration computes velocity change
- Positive value = growth speeding up
- Negative value = growth slowing down
- Previous velocity stored in RepoState for tracking

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/scoring/score.go (CalculateStarAcceleration)
- internal/scoring/score_test.go (TestCalculateStarAcceleration)
- internal/state/store.go (StarVelocity field in RepoState)
