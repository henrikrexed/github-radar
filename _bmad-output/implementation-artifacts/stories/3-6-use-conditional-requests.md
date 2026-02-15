# Story 3.6: Use Conditional Requests

Status: done

## Story

As the **system**,
I want to use If-Modified-Since headers,
So that unchanged repos don't consume rate limit quota.

## Acceptance Criteria

1. **Given** a previously collected repo with ETag/Last-Modified
   **When** re-collecting the same repo
   **Then** conditional request headers are sent (FR23)

2. **Given** a conditional request
   **When** server returns 304 Not Modified
   **Then** response is handled without re-processing

3. **Given** conditional requests are used
   **When** data is unchanged
   **Then** rate limit is preserved for unchanged repos

## Tasks / Subtasks

- [ ] Task 1: Add conditional request support to client
- [ ] Task 2: Store ETag/Last-Modified in state
- [ ] Task 3: Handle 304 responses
- [ ] Task 4: Write tests

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List
