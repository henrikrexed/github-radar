# Story 6.1: Query GitHub Search API by Topic

Status: done

## Story

As the **system**,
I want to search GitHub for repositories matching configured topics,
So that I can find trending projects in specific domains.

## Acceptance Criteria

1. **Given** topics configured (e.g., kubernetes, opentelemetry, ebpf)
   **When** discovery runs
   **Then** GitHub Search API is queried for each topic (FR7)

2. **Given** a topic query
   **When** search is executed
   **Then** search uses `topic:{topic}` qualifier

3. **Given** search results
   **When** processing results
   **Then** results are sorted by stars or recent activity

4. **Given** pagination needed
   **When** results exceed page size
   **Then** pagination retrieves up to 100 results per page

## Tasks / Subtasks

- [x] Task 1: Create SearchResult type in github package
- [x] Task 2: Implement SearchRepositories method on Client
- [x] Task 3: Support query string, sort, order, and per_page params
- [x] Task 4: Parse search API response JSON
- [x] Task 5: Write tests for search functionality

## Completion Notes

- SearchResult struct in internal/github/search.go
- SearchRepositories method with query, sort, order, perPage params
- Uses `/search/repositories` endpoint with URL encoding
- Per page capped at 100 (GitHub limit)
- Default per_page is 30 if not specified or <= 0
- Dates parsed from RFC3339 format

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/github/search.go (SearchResult, SearchRepositories)
- internal/github/search_test.go (TestSearchRepositories)
