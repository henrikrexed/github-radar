# Story 3.8: Persist Collection State

Status: done

## Story

As the **system**,
I want to save collection state after each run,
So that week-over-week comparisons are possible.

## Acceptance Criteria

1. **Given** collection completes (fully or partially)
   **When** state is persisted
   **Then** per-repo data is saved: stars, timestamp, velocities (FR33)

2. **Given** collection completes
   **When** state is persisted
   **Then** discovery state is saved: last scan, known repos (FR34)

3. **Given** state persistence
   **When** writes occur
   **Then** writes use atomic rename pattern (NFR12)

## Tasks / Subtasks

- [x] Task 1: Create scan service that ties collector and state
- [x] Task 2: Update state from collection results
- [x] Task 3: Calculate velocities from previous state
- [x] Task 4: Write tests

## Completion Notes

- Created Scanner that orchestrates Collector and State Store
- Automatic velocity calculation from previous/current stars
- Conditional requests supported via ETag/Last-Modified from state
- State atomically persisted after each scan
- Partial failure handling - continues with remaining repos

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/github/scanner.go (new)
- internal/github/scanner_test.go (new)
