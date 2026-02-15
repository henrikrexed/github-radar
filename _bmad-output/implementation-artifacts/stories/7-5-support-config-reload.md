# Story 7.5: Support Config Reload

Status: done

## Story

As an **operator**,
I want to reload config without restarting the daemon,
So that I can add repos without downtime.

## Acceptance Criteria

1. **Given** daemon is running
   **When** SIGHUP is received
   **Then** config is reloaded

2. **Given** config is reloaded
   **When** new repos are in config
   **Then** new repos are picked up on next scan

3. **Given** invalid config reload
   **When** validation fails
   **Then** reload is rejected with error log (keeps old config)

## Tasks / Subtasks

- [x] Task 1: Listen for SIGHUP signal
- [x] Task 2: Implement reloadConfig method
- [x] Task 3: Update scoring weights on reload
- [x] Task 4: Log reload success/failure

## Completion Notes

- SIGHUP signal handling in main loop
- reloadConfig() loads config from path
- On success: updates cfg, scoring weights, logs info
- On failure: logs error, keeps old config
- New repos picked up on next scheduled scan

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/daemon/daemon.go (reloadConfig, SIGHUP handling)
