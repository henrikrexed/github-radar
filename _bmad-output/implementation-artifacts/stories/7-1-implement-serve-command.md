# Story 7.1: Implement Serve Command

Status: done

## Story

As an **operator**,
I want to start the scanner as a foreground daemon,
So that Docker or systemd can manage it as a service.

## Acceptance Criteria

1. **Given** a valid configuration
   **When** `github-radar serve` is executed
   **Then** the daemon starts in foreground (not detached)

2. **Given** daemon is running
   **When** logs are generated
   **Then** logs are written to stdout/stderr

3. **Given** daemon is running
   **When** SIGTERM/SIGINT is received
   **Then** graceful shutdown occurs

4. **Given** daemon is started
   **When** it is running
   **Then** daemon runs until explicitly stopped

## Tasks / Subtasks

- [x] Task 1: Create Daemon struct in internal/daemon
- [x] Task 2: Implement New() constructor with config
- [x] Task 3: Implement Run() main loop
- [x] Task 4: Implement graceful shutdown
- [x] Task 5: Create ServeCmd in CLI
- [x] Task 6: Add serve to command router and help

## Completion Notes

- Daemon struct manages scanner, discoverer, exporter, state store
- Run() blocks and handles signals (SIGINT, SIGTERM)
- Graceful shutdown flushes metrics and saves state
- CLI serve command with --interval, --http-addr, --state flags

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/daemon/daemon.go (Daemon struct, New, Run, shutdown)
- internal/cli/serve.go (ServeCmd)
- internal/cli/root.go (serve command registration)
