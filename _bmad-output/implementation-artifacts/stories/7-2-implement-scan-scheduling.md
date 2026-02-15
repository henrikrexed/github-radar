# Story 7.2: Implement Scan Scheduling

Status: done

## Story

As the **system**,
I want to run collection on a configurable schedule,
So that metrics are updated automatically.

## Acceptance Criteria

1. **Given** `--interval 24h` flag
   **When** the daemon is running
   **Then** collection runs at scheduled intervals

2. **Given** daemon is running
   **When** next scan is scheduled
   **Then** next scheduled run is logged at INFO

3. **Given** SIGHUP signal
   **When** received by daemon
   **Then** config is reloaded

4. **Given** scan is already running
   **When** next scheduled scan time arrives
   **Then** overlapping runs are prevented (skip if previous still running)

## Tasks / Subtasks

- [x] Task 1: Implement interval-based scheduling in Run()
- [x] Task 2: Track lastScan and nextScan times
- [x] Task 3: Use sync.Mutex to prevent overlapping scans
- [x] Task 4: Log next scan time at INFO level
- [x] Task 5: Support SIGHUP for config reload

## Completion Notes

- Interval configured via --interval flag (default: 24h)
- Uses time.Ticker in main loop
- scanMu mutex prevents concurrent scans
- scheduleNextScan() calculates next run
- SIGHUP triggers reloadConfig()

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/daemon/daemon.go (Run, runScan, scheduleNextScan, reloadConfig)
