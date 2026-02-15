# Story 2.2: Define Repository Categories in Config

Status: done

## Story

As an **operator**,
I want to organize tracked repositories into categories,
So that I can group repos by topic for better dashboard organization.

## Acceptance Criteria

1. **Given** a config with category definitions
   **When** repos are added to categories
   **Then** categories are dynamic (created on-demand)

2. **Given** a repository without a specified category
   **When** it is added to tracking
   **Then** it goes to a "default" category

3. **Given** a repository
   **When** it is categorized
   **Then** it can belong to multiple categories

4. **Given** a configuration file
   **When** the system loads repositories
   **Then** each repository has its categories preserved

*(Addresses: FR1)*

## Tasks / Subtasks

- [x] Task 1: Extend configuration types (AC: #1-4)
  - [x] 1.1: Add Repositories section to Config struct
  - [x] 1.2: Add TrackedRepo struct with Repo, Categories fields
  - [x] 1.3: Support multiple categories per repo

- [x] Task 2: Update config loader (AC: #1, #4)
  - [x] 2.1: Parse repositories section from YAML
  - [x] 2.2: Default category is "default" when none specified (via NormalizeCategories)

- [x] Task 3: Add repository management functions (AC: #1-3)
  - [x] 3.1: Tracker.ByCategory() function
  - [x] 3.2: Tracker.Categories() function to list all categories
  - [x] 3.3: Tracker.Add() function with category parameters
  - [x] 3.4: LoadFromConfig() to create Tracker from config repos

- [x] Task 4: Write tests
  - [x] 4.1: Test config parsing with repositories (3 tests)
  - [x] 4.2: Test default category assignment
  - [x] 4.3: Test multiple categories per repo
  - [x] 4.4: Test category listing
  - [x] 4.5: Test LoadFromConfig with various inputs (4 tests)

## Dev Notes

### Config Structure

```yaml
repositories:
  - repo: kubernetes/kubernetes
    categories: [cncf, container-orchestration]
  - repo: prometheus/prometheus
    categories: [cncf, monitoring]
  - repo: grafana/grafana
    # No category specified - will use "default"
```

### TrackedRepo Struct

```go
type TrackedRepo struct {
    Owner      string
    Name       string
    Categories []string
}
```

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.2]
- Depends on Story 2.1: Parse Repository Identifiers

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### Debug Log References

- All tests pass: 40+ tests in repository package, config package
- Verified YAML parsing of repositories section
- Verified default category assignment
- Verified multiple categories per repository
- Verified LoadFromConfig with URLs, invalid repos, and empty lists

### Completion Notes List

- Extended Config struct with Repositories field ([]TrackedRepo)
- Created TrackedRepo struct with Repo string and Categories []string
- Created Tracker struct for managing tracked repositories in memory
- Implemented Tracker methods: Add, Remove, All, ByCategory, Categories, Count, Get, HasRepo
- Implemented LoadFromConfig() to parse config repos into Tracker
- NormalizeCategories() ensures default category when none specified
- Categories are merged when adding same repo multiple times
- Updated example config with repositories section

### File List

**New files:**
- internal/repository/tracking.go - Tracker and category management
- internal/repository/tracking_test.go - 20 unit tests

**Modified files:**
- internal/config/types.go - Added TrackedRepo struct, Repositories field
- internal/config/loader_test.go - Added 3 tests for repositories parsing
- configs/github-radar.example.yaml - Added repositories section example

