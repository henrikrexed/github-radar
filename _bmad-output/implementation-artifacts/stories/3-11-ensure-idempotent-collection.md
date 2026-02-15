# Story 3.11: Ensure Idempotent Collection

Status: done

## Story

As the **system**,
I want collection runs to be idempotent,
So that re-running produces consistent results.

## Acceptance Criteria

1. **Given** the same repos and time window
   **When** collection runs multiple times
   **Then** results are consistent (NFR13)

2. **Given** any collection run
   **When** state is updated
   **Then** state is not corrupted by partial runs

3. **Given** metrics
   **When** they are calculated
   **Then** metrics reflect current GitHub state, not accumulated

## Tasks / Subtasks

- [x] Task 1: Use atomic state writes (implemented in state.Store)
- [x] Task 2: Store absolute values, not deltas
- [x] Task 3: Calculate velocities from timestamps, not accumulation
- [x] Task 4: Handle partial runs gracefully

## Completion Notes

- State uses atomic write-rename pattern (NFR12)
- Scanner calculates velocities from timestamps: (new - prev) / days_elapsed
- Each scan replaces repo state with current values
- Partial failures don't corrupt state - only successful repos are updated
- Re-running produces same results (idempotent)

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/github/scanner.go (velocity calculation)
- internal/state/store.go (atomic writes)
