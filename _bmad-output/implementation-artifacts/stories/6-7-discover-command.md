# Story 6.7: Discover Command (Manual Trigger)

Status: done

## Story

As an **operator**,
I want to manually trigger discovery via CLI,
So that I can find new repos on-demand.

## Acceptance Criteria

1. **Given** discovery is configured
   **When** `github-radar discover [--topic <name>]`
   **Then** discovery runs for specified topic (or all if not specified)

2. **Given** discovery completes
   **When** results are displayed
   **Then** results show repos with scores

3. **Given** --auto-track flag
   **When** discovery completes
   **Then** eligible repos are auto-tracked

4. **Given** no --auto-track flag
   **When** discovery completes
   **Then** shows what would be tracked without adding

## Tasks / Subtasks

- [x] Task 1: Create DiscoverCmd struct
- [x] Task 2: Implement flag parsing (topics, min-stars, max-age, threshold, auto-track, format)
- [x] Task 3: Load config and apply CLI overrides
- [x] Task 4: Create Discoverer and run discovery
- [x] Task 5: Implement table, JSON, CSV output formats
- [x] Task 6: Implement --auto-track flag behavior
- [x] Task 7: Add discover command to CLI router
- [x] Task 8: Update help text

## Completion Notes

- DiscoverCmd in internal/cli/discover.go
- Supports --topics, --min-stars, --max-age, --threshold, --auto-track, --format flags
- CLI overrides merge with config file values
- Three output formats: table (default), json, csv
- --auto-track persists repos to state and saves state file
- Added to runCommand switch and help text in root.go

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/cli/discover.go (DiscoverCmd, Run, printTable, printJSON, printCSV)
- internal/cli/discover_test.go (TestDiscoverCmd_NoTopics, TestTruncate)
- internal/cli/root.go (discover case in runCommand, help text)
