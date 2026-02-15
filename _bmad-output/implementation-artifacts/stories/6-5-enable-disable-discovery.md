# Story 6.5: Enable/Disable Discovery

Status: done

## Story

As an **operator**,
I want to toggle automatic discovery on/off,
So that I can control when new repos are found.

## Acceptance Criteria

1. **Given** `discovery.enabled: true|false` in config
   **When** daemon runs
   **Then** discovery only runs if enabled (FR12)

2. **Given** discovery disabled
   **When** `github-radar discover` CLI is run
   **Then** CLI works regardless of enabled setting

3. **Given** disabled discovery
   **When** skipped
   **Then** INFO message explains why skipped

## Tasks / Subtasks

- [x] Task 1: Add Enabled field to DiscoveryConfig
- [x] Task 2: Default Enabled to true in DefaultConfig
- [x] Task 3: CLI discover command works regardless of Enabled flag

## Completion Notes

- Enabled field in config/types.go DiscoveryConfig
- Default value is true in DefaultConfig()
- CLI discover command doesn't check Enabled flag (always works)
- Daemon can check Enabled before running discovery (future Epic 7)

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/config/types.go (Enabled in DiscoveryConfig)
