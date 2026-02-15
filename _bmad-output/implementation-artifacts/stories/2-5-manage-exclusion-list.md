# Story 2.5: Manage Exclusion List

Status: done

## Story

As an **operator**,
I want to maintain a flexible exclusion list,
So that specific repos are never tracked regardless of discovery.

## Acceptance Criteria

1. **Given** an exclusion pattern (repo or org/*)
   **When** `github-radar exclude <pattern>`
   **Then** exact repos (`owner/repo`) are excluded

2. **Given** a wildcard pattern
   **When** `github-radar exclude <org/*>`
   **Then** entire orgs are excluded

3. **Given** an exclusion exists
   **When** any add or discovery operation runs
   **Then** exclusions override discovery or manual adds

*(Addresses: FR4)*

## Tasks / Subtasks

- [x] Task 1: Create exclude subcommand (AC: #1-2)
  - [x] 1.1: Add "exclude" subcommand with add/remove/list actions
  - [x] 1.2: Support exact repo patterns (owner/repo)
  - [x] 1.3: Support wildcard patterns (org/*)

- [x] Task 2: Implement exclusion matching (AC: #3)
  - [x] 2.1: Create ExclusionList with IsExcluded() method
  - [x] 2.2: Match exact patterns (owner/repo)
  - [x] 2.3: Match wildcard patterns (owner/*)

- [x] Task 3: Write tests
  - [x] 3.1: Test exclude add/remove/list commands (11 tests)
  - [x] 3.2: Test ExclusionList functionality (9 tests)
  - [x] 3.3: Test wildcard matching
  - [x] 3.4: Test exact matching
  - [x] 3.5: Test pattern validation

## Dev Notes

### CLI Usage

```bash
# Add exclusion
github-radar exclude add example-org/example-repo

# Add wildcard exclusion (entire org)
github-radar exclude add example-org/*

# List exclusions
github-radar exclude list

# Remove exclusion
github-radar exclude remove example-org/example-repo
```

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.5]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### Debug Log References

- All tests pass including 20 new tests
- Verified exclude add with exact and wildcard patterns
- Verified exclude remove command
- Verified exclude list command
- Verified pattern validation

### Completion Notes List

- Created ExclusionList struct in internal/repository/exclusion.go
- Implemented Add, Remove, IsExcluded, IsExcludedRepo methods
- Support for exact patterns (owner/repo) and wildcard (owner/*)
- ValidatePattern() validates exclusion patterns
- Created ExcludeCmd in internal/cli/exclude.go
- Commands: exclude add, exclude remove, exclude list
- List shows pattern type (exact vs wildcard)
- Updated help message with exclude command

### File List

**New files:**
- internal/repository/exclusion.go - Exclusion list management
- internal/repository/exclusion_test.go - 9 unit tests
- internal/cli/exclude.go - Exclude CLI commands
- internal/cli/exclude_test.go - 11 unit tests

**Modified files:**
- internal/cli/root.go - Added exclude command routing, updated help

