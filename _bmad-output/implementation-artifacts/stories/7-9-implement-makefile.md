# Story 7.9: Implement Makefile

Status: done

## Story

As a **developer**,
I want a Makefile for common tasks,
So that build commands are standardized.

## Acceptance Criteria

1. **Given** project source
   **When** `make build` is run
   **Then** builds local binary

2. **Given** project source
   **When** `make test` is run
   **Then** runs all tests

3. **Given** project source
   **When** `make docker` is run
   **Then** builds Docker image

4. **Given** project source
   **When** `make release` is run
   **Then** cross-compiles all platforms

5. **Given** project source
   **When** `make lint` is run
   **Then** runs go vet + staticcheck

## Tasks / Subtasks

- [x] Task 1: Create Makefile with standard targets
- [x] Task 2: Add build, test, lint, clean targets
- [x] Task 3: Add docker targets
- [x] Task 4: Add release target
- [x] Task 5: Add help target

## Completion Notes

Makefile targets:
- build: Build local binary
- test, test-v, test-coverage: Run tests
- lint: Run go vet and staticcheck
- fmt, fmt-check: Format code
- clean: Remove artifacts
- run, serve: Run binary
- docker, docker-push: Docker operations
- docker-up, docker-down: docker-compose
- release: Cross-platform builds
- help: Show all targets

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- Makefile
