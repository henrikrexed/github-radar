# Story 6.2: Filter by Minimum Star Count

Status: done

## Story

As an **operator**,
I want to filter discovered repos by minimum stars,
So that I only see established projects, not noise.

## Acceptance Criteria

1. **Given** `discovery.minStars: 100` in config
   **When** repos are discovered
   **Then** repos with fewer stars are excluded (FR8)

2. **Given** min stars filter configured
   **When** filter is applied
   **Then** filter happens at search query level (efficient)

3. **Given** default configuration
   **When** no min_stars specified
   **Then** default minimum is 100

## Tasks / Subtasks

- [x] Task 1: Add MinStars to discovery Config
- [x] Task 2: Include stars:>={minStars} in search query
- [x] Task 3: Add passesFilters method with star check
- [x] Task 4: Set default MinStars to 100

## Completion Notes

- Config.MinStars field in discovery/discovery.go
- Search query includes `stars:>={minStars}` qualifier
- passesFilters method double-checks stars in case API returns edge cases
- Default value is 100 in DefaultConfig()

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/discovery/discovery.go (MinStars in Config, passesFilters)
- internal/config/types.go (MinStars in DiscoveryConfig)
