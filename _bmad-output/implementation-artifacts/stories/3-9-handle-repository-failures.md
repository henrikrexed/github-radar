# Story 3.9: Handle Individual Repository Failures

Status: done

## Story

As the **system**,
I want to continue processing when individual repos fail,
So that one bad repo doesn't block the entire scan.

## Acceptance Criteria

1. **Given** an API error for a specific repository
   **When** the error occurs
   **Then** the repo is skipped with a warning log (NFR10)

2. **Given** a 404 error for a repository
   **When** the error occurs
   **Then** logs "repo may be deleted/renamed" (FR53)

3. **Given** some repos fail
   **When** processing continues
   **Then** other repos continue processing
   **And** partial results are still saved

## Tasks / Subtasks

- [ ] Task 1: Create collector with error handling
- [ ] Task 2: Implement error classification
- [ ] Task 3: Return partial results
- [ ] Task 4: Write tests

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List
