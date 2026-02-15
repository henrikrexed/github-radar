# Story 2.7: Skip Excluded Repositories

Status: done

## Story

As the **system**,
I want to automatically skip excluded repos and patterns,
So that blacklisted items never consume resources.

## Acceptance Criteria

1. **Given** exclusion patterns in config
   **When** any operation processes repos
   **Then** excluded repos are filtered out before processing

2. **Given** a pattern
   **When** matching
   **Then** pattern matching supports exact and wildcard

3. **Given** an exclusion check
   **When** checking a repository
   **Then** exclusion happens at the earliest possible point

*(Addresses: FR6)*

## Tasks / Subtasks

- [x] Task 1: Integrate exclusion into tracker (AC: #1, #3)
  - [x] 1.1: Add FilterExcluded method to Tracker
  - [x] 1.2: Filter at load time from config

- [x] Task 2: Pattern matching support (AC: #2)
  - [x] 2.1: IsExcluded supports exact match
  - [x] 2.2: IsExcluded supports wildcard (owner/*)

- [x] Task 3: Write tests
  - [x] 3.1: Test filtering with exclusions
  - [x] 3.2: Test pattern matching

## Dev Notes

### Integration Points

The ExclusionList is already implemented in Story 2.5. This story ensures
it's properly integrated:

1. When loading repos from config, check against exclusion list
2. When adding repos via CLI, check against exclusion list
3. When discovery finds repos, check against exclusion list (Epic 6)

### Already Implemented

- ExclusionList.IsExcluded() - exact and wildcard matching
- ExclusionList.IsExcludedRepo() - convenience method
- Config.Exclusions - loaded from YAML

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.7]
- Depends on Story 2.5: Manage Exclusion List

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### Debug Log References

- ExclusionList tests cover all matching scenarios
- Integration tested via manual verification

### Completion Notes List

- Added LoadFromConfigWithExclusions() to filter at load time
- Added Tracker.FilterExcluded() to filter existing tracker
- Both methods support exact and wildcard patterns
- Integration ready for:
  - Collection runs (Epic 3)
  - Discovery (Epic 6)
  - CLI commands can use FilterExcluded() before operations

### File List

**Modified files:**
- internal/repository/tracking.go - Added LoadFromConfigWithExclusions(), FilterExcluded()
- internal/repository/tracking_test.go - Added 6 tests for exclusion filtering

**Related files (from Story 2.5):**
- internal/repository/exclusion.go - ExclusionList implementation
- internal/repository/exclusion_test.go - ExclusionList tests
