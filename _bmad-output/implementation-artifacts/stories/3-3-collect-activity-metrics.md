# Story 3.3: Collect Activity Metrics (7-Day Window)

Status: done

## Story

As the **system**,
I want to collect recent activity metrics,
So that I can calculate velocity and detect sudden growth.

## Acceptance Criteria

1. **Given** a tracked repository
   **When** activity metrics are collected
   **Then** PRs merged in last 7 days is counted (FR17)

2. **Given** a tracked repository
   **When** activity metrics are collected
   **Then** issues opened in last 7 days is counted (FR18)

3. **Given** a tracked repository
   **When** activity metrics are collected
   **Then** contributor count is retrieved (FR19)

4. **Given** a tracked repository
   **When** activity metrics are collected
   **Then** latest release tag and date are retrieved (FR20)

## Tasks / Subtasks

- [x] Task 1: Define activity metrics types
  - [x] 1.1: Create ActivityMetrics struct
  - [x] 1.2: Create ReleaseInfo struct

- [x] Task 2: Implement GetMergedPRsCount method
  - [x] 2.1: Call /repos/:owner/:repo/pulls?state=closed
  - [x] 2.2: Filter by merged_at within 7 days

- [x] Task 3: Implement GetRecentIssuesCount method
  - [x] 3.1: Call /repos/:owner/:repo/issues?state=all&since=7days
  - [x] 3.2: Count issues opened in window (filter out PRs)

- [x] Task 4: Implement GetContributorCount method
  - [x] 4.1: Call /repos/:owner/:repo/contributors
  - [x] 4.2: Count unique contributors

- [x] Task 5: Implement GetLatestRelease method
  - [x] 5.1: Call /repos/:owner/:repo/releases/latest
  - [x] 5.2: Handle no releases case (return nil)

- [x] Task 6: Write tests

## Dev Notes

### GitHub API Endpoints

- Merged PRs: GET /repos/:owner/:repo/pulls?state=closed
- Recent Issues: GET /repos/:owner/:repo/issues?since=ISO8601
- Contributors: GET /repos/:owner/:repo/contributors
- Latest Release: GET /repos/:owner/:repo/releases/latest

### ActivityMetrics Structure

```go
type ActivityMetrics struct {
    MergedPRs7d    int
    NewIssues7d    int
    Contributors   int
    LatestRelease  *ReleaseInfo
}

type ReleaseInfo struct {
    TagName     string
    PublishedAt time.Time
}
```

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### Completion Notes List

- Created ActivityMetrics and ReleaseInfo structs
- Implemented GetMergedPRsCount with 7-day window and pagination
- Implemented GetRecentIssuesCount with PR filtering
- Implemented GetContributorCount with fallback counting
- Implemented GetLatestRelease with 404 handling
- Created GetActivityMetrics convenience method

### File List

- internal/github/activity.go (new)
- internal/github/activity_test.go (new)
- internal/github/client.go (modified - added decodeJSON helper)
