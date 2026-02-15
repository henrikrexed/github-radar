# Story 5.6: Configure Structured Logging with slog

Status: done

## Story

As the **system**,
I want structured JSON logging via slog,
So that logs are parseable and filterable.

## Acceptance Criteria

1. **Given** slog is the logging framework
   **When** logs are emitted
   **Then** output is structured JSON

2. **Given** log level configuration
   **When** setting via `--log-level` flag
   **Then** level is configurable (debug, info, warn, error)

3. **Given** attribute naming
   **When** logs are emitted
   **Then** attributes use snake_case per Architecture patterns

## Tasks / Subtasks

- [x] Task 1: Use slog with JSON handler
- [x] Task 2: Define standard attribute constants
- [x] Task 3: Implement Init with verbose flag
- [x] Task 4: Add ParseLevel for string-based config
- [x] Task 5: Write tests

## Completion Notes

- Uses log/slog with NewJSONHandler
- Standard attributes defined: repo_owner, repo_name, scan_id, etc.
- ParseLevel() parses debug/info/warn/error strings
- InitWithLevel() for string-based configuration
- All attributes use snake_case

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/logging/logger.go
- internal/logging/logger_test.go
