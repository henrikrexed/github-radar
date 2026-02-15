# Story 5.5: Flush Metrics on Exit

Status: done

## Story

As the **system**,
I want to synchronously flush all metrics before process exit,
So that no data is lost on shutdown.

## Acceptance Criteria

1. **Given** metrics have been recorded
   **When** the process exits (normal or error)
   **Then** all pending metrics are flushed synchronously (FR40)

2. **Given** flush timeout is configurable
   **When** configured
   **Then** default: 10s

3. **Given** flush errors
   **When** they occur
   **Then** errors are logged but don't change exit code

## Tasks / Subtasks

- [x] Task 1: Implement Flush method
- [x] Task 2: Implement Shutdown method
- [x] Task 3: Add ShutdownWithTimeout helper
- [x] Task 4: Make flush timeout configurable
- [x] Task 5: Write tests

## Completion Notes

- Flush() forces immediate export of pending metrics
- Shutdown() gracefully closes exporter
- ShutdownWithTimeout() uses configured FlushTimeout (default 10s)
- OtelConfig extended with FlushTimeout field

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/metrics/exporter.go (Flush, Shutdown, ShutdownWithTimeout)
- internal/config/types.go (FlushTimeout field)
