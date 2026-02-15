# Story 5.7: Log at Appropriate Levels

Status: done

## Story

As the **system**,
I want to log events at correct severity levels,
So that operators can filter and alert appropriately.

## Acceptance Criteria

1. **Given** logging is configured
   **When** scan progress/summary events occur
   **Then** log at INFO level (FR48)

2. **Given** API calls or metric values
   **When** logged
   **Then** log at DEBUG level (FR49)

3. **Given** rate limit warnings
   **When** they occur
   **Then** log at WARN level (FR50)

4. **Given** fatal errors
   **When** they occur
   **Then** log at ERROR level (FR51)

## Tasks / Subtasks

- [x] Task 1: Define Debug, Info, Warn, Error package functions
- [x] Task 2: Document expected log levels for event types
- [x] Task 3: Write tests verifying log levels

## Completion Notes

Log level guidelines:
- DEBUG: API calls, metric values, detailed progress
- INFO: Scan start/complete, summary stats
- WARN: Rate limits, partial failures, recoverable errors
- ERROR: Fatal errors, config failures, unrecoverable issues

Package functions: Debug(), Info(), Warn(), Error()

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/logging/logger.go (Debug, Info, Warn, Error functions)
- internal/logging/logger_test.go (TestLogLevels)
