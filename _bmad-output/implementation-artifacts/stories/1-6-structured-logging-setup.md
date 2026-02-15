# Story 1.6: Structured Logging Setup

Status: review

## Story

As an **operator**,
I want structured JSON logging with configurable levels,
So that I can debug issues and integrate with log aggregation systems.

## Acceptance Criteria

1. **Given** the application is running
   **When** log output is generated
   **Then** logs are in structured JSON format with timestamp, level, message, and attributes

2. **Given** the --verbose flag is set
   **When** the application runs
   **Then** DEBUG level logs are included in output

3. **Given** no --verbose flag
   **When** the application runs
   **Then** only INFO and above logs are shown

4. **Given** any log entry
   **When** attributes are added (repo_name, scan_id, etc.)
   **Then** they appear as snake_case JSON keys

5. **Given** a credential or token value
   **When** it could appear in logs
   **Then** it is never logged (per NFR7)

*(Addresses: FR48-FR51, NFR7)*

## Tasks / Subtasks

- [x] Task 1: Initialize slog with JSON handler (AC: #1)
  - [x] 1.1: Create logging package in internal/logging
  - [x] 1.2: Configure slog.JSONHandler as default
  - [x] 1.3: Add timestamp, level, message formatting (automatic with JSON handler)
  - [x] 1.4: Write tests for JSON output format (13 tests)

- [x] Task 2: Implement log level control (AC: #2, #3)
  - [x] 2.1: Default to INFO level
  - [x] 2.2: Switch to DEBUG when --verbose is set
  - [x] 2.3: Write tests for level filtering

- [x] Task 3: Define attribute conventions (AC: #4)
  - [x] 3.1: Create helper functions: Repo(), RepoFull(), Scan(), Duration(), Err()
  - [x] 3.2: All attribute constants use snake_case (verified with test)
  - [x] 3.3: Documented 13 standard attribute names as constants

- [x] Task 4: Integrate with CLI (AC: #1-4)
  - [x] 4.1: Initialize logger on startup with logging.Init(verbose)
  - [x] 4.2: Added DEBUG log for CLI initialization
  - [x] 4.3: Verified integration with manual testing

## Dev Notes

### Architecture Requirements

Per architecture spec:
- Use slog (Go stdlib, 1.21+)
- JSON output format
- snake_case for all attribute keys

### Log Levels (per FR48-FR51)

| Level | Usage |
|-------|-------|
| DEBUG | API calls, detailed values, scan progress |
| INFO | Summary statistics, progress milestones |
| WARN | Rate limit approaching, repo 404s |
| ERROR | Fatal issues requiring attention |

### Standard Attributes

| Attribute | Type | Usage |
|-----------|------|-------|
| repo_owner | string | Repository owner |
| repo_name | string | Repository name |
| scan_id | string | Unique scan identifier |
| duration_ms | int64 | Operation duration |
| error | string | Error message |

### Example Output

```json
{"time":"2026-02-15T10:30:00Z","level":"INFO","msg":"scan started","scan_id":"abc123","repos_total":47}
{"time":"2026-02-15T10:30:05Z","level":"DEBUG","msg":"fetched repo","repo_owner":"kubernetes","repo_name":"kubernetes","stars":100000}
{"time":"2026-02-15T10:30:10Z","level":"INFO","msg":"scan complete","scan_id":"abc123","repos_scanned":47,"duration_ms":10000}
```

### Security (NFR7)

Never log:
- github.token
- otel.headers values (may contain tokens)
- Any ${...} expanded values that could be secrets

### References

- [Source: _bmad-output/planning-artifacts/architecture.md#Logging Conventions]
- [Source: _bmad-output/planning-artifacts/prd.md#FR48-FR51]
- [Source: _bmad-output/planning-artifacts/epics.md#Story 1.6]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### Debug Log References

- All tests pass (13 logging tests + all existing tests)
- Verified JSON output with --verbose flag
- Verified DEBUG filtered without --verbose flag

### Completion Notes List

- Created internal/logging package with slog JSON handler
- Package-level functions: Debug(), Info(), Warn(), Error(), With()
- Init(verbose) initializes logger with appropriate level
- Helper functions for common attributes: Repo(), Scan(), Duration(), Err()
- 13 attribute constants defined with snake_case naming
- Test validates all attribute names use snake_case
- Logger integrated into CLI via logging.Init(c.Verbose)
- Security: No credential logging (tokens never passed to logger)

### File List

**New files:**
- internal/logging/logger.go - Logging package with slog JSON handler
- internal/logging/logger_test.go - 13 unit tests

**Modified files:**
- internal/cli/root.go - Added logging initialization and DEBUG log

