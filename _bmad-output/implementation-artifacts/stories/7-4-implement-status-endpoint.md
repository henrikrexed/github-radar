# Story 7.4: Implement Status Endpoint

Status: done

## Story

As an **operator**,
I want a `/status` endpoint showing daemon state,
So that I can check scan progress and metrics.

## Acceptance Criteria

1. **Given** daemon is running
   **When** `GET http://localhost:8080/status`
   **Then** returns JSON with status, last_scan, repos_tracked, next_scan, rate_limit_remaining

2. **Given** status endpoint exists
   **When** CLI `github-radar status` is run
   **Then** CLI queries the endpoint and displays results

## Tasks / Subtasks

- [x] Task 1: Define StatusResponse struct
- [x] Task 2: Implement handleStatus endpoint
- [x] Task 3: Track metrics in daemon (repos, rate limit)
- [x] Task 4: Create StatusCmd for CLI
- [x] Task 5: Add status to CLI router

## Completion Notes

- StatusResponse includes: status, last_scan, next_scan, repos_tracked, rate_limit_remaining, uptime
- handleStatus returns JSON response
- CLI status command queries daemon at configurable --addr
- Supports --format text|json

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/daemon/daemon.go (StatusResponse, handleStatus)
- internal/cli/status.go (StatusCmd)
- internal/cli/root.go (status command registration)
- internal/daemon/daemon_test.go (TestStatusEndpoint)
