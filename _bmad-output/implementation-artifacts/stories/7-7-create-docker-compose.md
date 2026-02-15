# Story 7.7: Create Docker Compose Example

Status: done

## Story

As an **operator**,
I want a docker-compose.yml example,
So that I can quickly deploy with proper configuration.

## Acceptance Criteria

1. **Given** Docker image is available
   **When** `docker-compose up -d`
   **Then** daemon starts with mounted config

2. **Given** docker-compose
   **When** deploying
   **Then** environment variables are passed for secrets

3. **Given** docker-compose
   **When** running
   **Then** health check is configured

4. **Given** docker-compose
   **When** state is generated
   **Then** state file is persisted via volume

## Tasks / Subtasks

- [x] Task 1: Create docker-compose.yml
- [x] Task 2: Mount config volume
- [x] Task 3: Add environment variables for secrets
- [x] Task 4: Configure health check
- [x] Task 5: Create named volume for state persistence
- [x] Task 6: Add logging configuration

## Completion Notes

- docker-compose.yml with full configuration
- Mounts ./configs/config.yaml to /etc/github-radar/
- Environment vars: GITHUB_TOKEN, OTEL_ENDPOINT, DT_API_TOKEN
- Health check using wget to /health
- Named volume github-radar-data for state
- JSON logging with size limits

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- docker-compose.yml
