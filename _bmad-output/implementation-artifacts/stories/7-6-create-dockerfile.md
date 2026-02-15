# Story 7.6: Create Dockerfile

Status: done

## Story

As an **operator**,
I want a Docker image for easy deployment,
So that I can run the scanner in any container environment.

## Acceptance Criteria

1. **Given** Go source code
   **When** `docker build` is run
   **Then** multi-stage build produces minimal image

2. **Given** Docker image
   **When** built
   **Then** base image is alpine (~15MB with dependencies)

3. **Given** container
   **When** running
   **Then** binary runs as non-root user

4. **Given** deployment
   **When** config needed
   **Then** config can be mounted at `/etc/github-radar/config.yaml`

## Tasks / Subtasks

- [x] Task 1: Create multi-stage Dockerfile
- [x] Task 2: Use alpine base for health check support
- [x] Task 3: Create non-root user
- [x] Task 4: Add HEALTHCHECK instruction
- [x] Task 5: Define VOLUME and EXPOSE

## Completion Notes

- Multi-stage build: golang:1.22-alpine -> alpine:3.19
- Non-root user: github-radar
- HEALTHCHECK using wget
- Volumes: /etc/github-radar (config), /data (state)
- EXPOSE 8080 for status endpoint
- Default CMD runs serve command

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- Dockerfile
