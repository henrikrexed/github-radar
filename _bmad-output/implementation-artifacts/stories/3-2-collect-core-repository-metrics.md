# Story 3.2: Collect Core Repository Metrics

Status: done

## Story

As the **system**,
I want to collect basic metrics for each tracked repository,
So that I have foundational data for growth analysis.

## Acceptance Criteria

1. **Given** a list of tracked repositories
   **When** collection runs
   **Then** star count is collected (FR13)

2. **Given** a tracked repository
   **When** collection runs
   **Then** fork count is collected (FR14)

3. **Given** a tracked repository
   **When** collection runs
   **Then** open issues count is collected (FR15)

4. **Given** a tracked repository
   **When** collection runs
   **Then** open PRs count is collected (FR16)

5. **Given** a tracked repository
   **When** collection runs
   **Then** primary language and topics are collected (FR21)

## Tasks / Subtasks

- [x] Task 1: Define repository metrics types
  - [x] 1.1: Create RepoMetrics struct in internal/github/
  - [x] 1.2: Include all core fields (stars, forks, issues, language, topics)

- [x] Task 2: Implement GetRepository method
  - [x] 2.1: Call /repos/:owner/:repo endpoint
  - [x] 2.2: Parse response into RepoMetrics
  - [x] 2.3: Handle 404 and other errors

- [x] Task 3: Implement GetOpenPRCount method
  - [x] 3.1: Call /repos/:owner/:repo/pulls endpoint with state=open
  - [x] 3.2: Count results (fallback approach)

- [x] Task 4: Write tests
  - [x] 4.1: Test GetRepository success
  - [x] 4.2: Test GetOpenPRCount
  - [x] 4.3: Test error handling (404, rate limit)

## Dev Notes

### GitHub API Endpoints

- Repository: GET /repos/:owner/:repo
- Open PRs: GET /repos/:owner/:repo/pulls?state=open

### RepoMetrics Structure

```go
type RepoMetrics struct {
    Owner       string
    Name        string
    FullName    string
    Stars       int
    Forks       int
    OpenIssues  int
    OpenPRs     int
    Language    string
    Topics      []string
    Description string
}
```

### References

- [GitHub Repos API](https://docs.github.com/en/rest/repos/repos#get-a-repository)
- [Source: epics.md#Story 3.2]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### Debug Log References

### Completion Notes List

- Created RepoMetrics struct with all core fields
- Implemented GetRepository() to fetch from /repos/:owner/:repo
- Implemented GetOpenPRCount() using /repos/:owner/:repo/pulls?state=open
- Implemented GetRepositoryWithPRs() convenience method
- Rate limit tracking continues to work across all repository calls
- Comprehensive test coverage with httptest mocking

### File List

- internal/github/repository.go (new)
- internal/github/repository_test.go (new)
