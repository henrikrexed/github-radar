# Story 5.10: Implement Dry-Run Mode

Status: done

## Story

As an **operator**,
I want to run collection without exporting metrics,
So that I can test configuration without sending data.

## Acceptance Criteria

1. **Given** `--dry-run` flag is set
   **When** collection runs
   **Then** all data is collected and scored normally

2. **Given** dry-run mode
   **When** metrics export is attempted
   **Then** metrics are NOT exported to OTLP endpoint (FR55)

3. **Given** dry-run mode
   **When** logging occurs
   **Then** logs indicate dry-run mode is active

4. **Given** dry-run mode
   **When** collection completes
   **Then** state IS saved (for testing state persistence)

## Tasks / Subtasks

- [x] Task 1: Add --dry-run flag to CLI
- [x] Task 2: Implement DryRun in ExporterConfig
- [x] Task 3: Use ManualReader instead of PeriodicReader in dry-run
- [x] Task 4: Add IsDryRun() method
- [x] Task 5: Write tests

## Completion Notes

- CLI has --dry-run flag
- ExporterConfig.DryRun field controls behavior
- When DryRun=true, uses ManualReader (no export)
- Flush() is no-op in dry-run mode
- State is still saved (allows testing persistence)
- Logged at DEBUG level: "dry_run: true"

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/cli/root.go (--dry-run flag)
- internal/metrics/exporter.go (DryRun support)
- internal/metrics/exporter_test.go (TestNewExporter_DryRun)
