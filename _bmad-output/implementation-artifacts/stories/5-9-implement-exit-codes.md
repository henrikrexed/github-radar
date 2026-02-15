# Story 5.9: Implement Exit Codes

Status: done

## Story

As the **system**,
I want to return appropriate exit codes,
So that scripts and CI can detect success/failure.

## Acceptance Criteria

1. **Given** a collection run completes successfully
   **When** the process exits
   **Then** exit code 0 (FR54)

2. **Given** fatal errors occur
   **When** the process exits
   **Then** exit code 1 (FR54)

3. **Given** partial success (some repos failed)
   **When** the process exits
   **Then** still exits 0 (partial results are valid)

## Tasks / Subtasks

- [x] Task 1: CLI.Run returns int exit code
- [x] Task 2: Return 0 on success
- [x] Task 3: Return 1 on fatal errors
- [x] Task 4: Document exit code behavior

## Completion Notes

- CLI.Run() returns int (0 or 1)
- Success: exit 0
- Fatal error (config, auth): exit 1
- Partial failure (some repos): exit 0 (partial results exported)
- Unknown command: exit 1

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/cli/root.go (Run returns int)
- cmd/github-radar/main.go (os.Exit with return value)
