# Story 6.3: Filter by Repository Age

Status: done

## Story

As an **operator**,
I want to filter discovered repos by maximum age,
So that I focus on newer projects with growth potential.

## Acceptance Criteria

1. **Given** `discovery.maxAgeDays: 90` in config
   **When** repos are discovered
   **Then** repos older than threshold are excluded (FR9)

2. **Given** age filter configured
   **When** filter is applied
   **Then** age is calculated from repo creation date

3. **Given** maxAgeDays set to 0
   **When** discovery runs
   **Then** no age limit is applied

## Tasks / Subtasks

- [x] Task 1: Add MaxAgeDays to discovery Config
- [x] Task 2: Include created:>={cutoff} in search query
- [x] Task 3: Add age check to passesFilters
- [x] Task 4: Set default MaxAgeDays to 90

## Completion Notes

- Config.MaxAgeDays field in discovery/discovery.go
- Search query includes `created:>={date}` when MaxAgeDays > 0
- passesFilters method checks if time.Since(CreatedAt) > MaxAgeDays
- Default value is 90 days in DefaultConfig()
- Value of 0 disables age filtering

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/discovery/discovery.go (MaxAgeDays in Config, passesFilters)
- internal/config/types.go (MaxAgeDays in DiscoveryConfig)
