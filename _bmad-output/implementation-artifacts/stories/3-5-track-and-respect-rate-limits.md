# Story 3.5: Track and Respect Rate Limits

Status: done

## Story

As the **system**,
I want to monitor GitHub API rate limits,
So that I never exceed quota and get blocked.

## Acceptance Criteria

1. **Given** a configurable rate limit threshold (e.g., 4000 of 5000)
   **When** remaining requests approach threshold
   **Then** collection pauses or slows down (NFR11)

2. **Given** rate limit is near exhaustion
   **When** making requests
   **Then** rate limit status is logged at WARN level

3. **Given** any API response
   **When** response is received
   **Then** X-RateLimit headers are parsed and tracked (FR22)

## Tasks / Subtasks

- [x] Task 1: Add rate limit threshold configuration
  - [x] 1.1: Add RateLimitOptions to client
  - [x] 1.2: Add ShouldBackoff() method

- [x] Task 2: Implement rate limit checking
  - [x] 2.1: Check rate limit before each request
  - [x] 2.2: WaitForRateLimit() blocks until reset

- [x] Task 3: Add callback for rate limit warnings
  - [x] 3.1: Add OnWarning callback in RateLimitOptions
  - [x] 3.2: Trigger when below threshold

- [x] Task 4: Write tests

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### Completion Notes List

- Created RateLimitOptions with configurable threshold
- Added ShouldBackoff() to check if client should slow down
- Added IsRateLimitExhausted() for exhaustion detection
- Added WaitForRateLimit() with context cancellation support
- Added OnWarning callback for rate limit notifications
- Created RateLimitError type for rate limit failures

### File List

- internal/github/ratelimit.go (new)
- internal/github/ratelimit_test.go (new)
- internal/github/client.go (modified - added rateLimitOpts field)
