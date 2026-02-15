# Story 5.8: Handle Repository Errors Gracefully

Status: done

## Story

As the **system**,
I want to skip failed repos and continue,
So that partial results are still exported.

## Acceptance Criteria

1. **Given** an error occurs for a specific repo
   **When** processing continues
   **Then** the repo is skipped with a warning (FR52)

2. **Given** a 404 error
   **When** logged
   **Then** note "repo may be deleted/renamed" (FR53)

3. **Given** some repos fail
   **When** collection completes
   **Then** successfully collected repos are still exported

## Tasks / Subtasks

- [x] Task 1: Implement error classification (Epic 3)
- [x] Task 2: Add RepoErrorNotFound type
- [x] Task 3: Continue on individual repo failures
- [x] Task 4: Track failed repos in results

## Completion Notes

Implemented in Epic 3:
- RepoError with Type field (Transient, Permanent, NotFound, RateLimit)
- RepoErrorNotFound message: "repository not found (may be deleted or renamed)"
- Collector.CollectAll continues on individual failures
- ScanResult.FailedRepos tracks which repos failed

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/github/collector.go (error handling, RepoError)
- internal/github/scanner.go (partial failure handling)
