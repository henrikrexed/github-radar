# Story 7.8: Implement Cross-Platform Build

Status: done

## Story

As a **developer**,
I want to build binaries for Mac/Windows/Linux,
So that users can run natively without Docker.

## Acceptance Criteria

1. **Given** Go source code
   **When** `make release` is run
   **Then** binaries are built for: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64

2. **Given** release build
   **When** binaries are created
   **Then** binaries are named `github-radar-{os}-{arch}`

## Tasks / Subtasks

- [x] Task 1: Add release target to Makefile
- [x] Task 2: Cross-compile for all platforms
- [x] Task 3: Output to dist/ directory
- [x] Task 4: Include version in binary

## Completion Notes

- `make release` builds for 5 platforms
- Binaries output to dist/ directory
- VERSION variable for ldflags
- CGO disabled for cross-compilation

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- Makefile (release target)
