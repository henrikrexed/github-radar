# Story 6.6: Auto-Track High-Growth Discoveries

Status: done

## Story

As the **system**,
I want to automatically add discovered repos that exceed a growth threshold,
So that trending projects are tracked without manual intervention.

## Acceptance Criteria

1. **Given** `discovery.autoTrackThreshold: 50` in config
   **When** a discovered repo's growth score exceeds threshold
   **Then** the repo is automatically added to tracking (FR10)

2. **Given** auto-tracking enabled
   **When** repo is auto-added
   **Then** repo goes to "discovered" category

3. **Given** exclusion list
   **When** repo matches exclusion
   **Then** excluded repos are never auto-added

4. **Given** auto-track happens
   **When** logged
   **Then** INFO log announces each auto-tracked repo

## Tasks / Subtasks

- [x] Task 1: Add AutoTrackThreshold to Config
- [x] Task 2: Calculate NormalizedScore for discovered repos
- [x] Task 3: Set ShouldAutoTrack flag based on threshold
- [x] Task 4: Implement AutoTrack method to persist repos
- [x] Task 5: Check exclusion list before auto-tracking

## Completion Notes

- AutoTrackThreshold field in discovery.Config
- normalizeScores method normalizes scores using scoring.NormalizeScores
- ShouldAutoTrack flag set when NormalizedScore >= threshold
- AutoTrack method adds repos to state store
- Exclusion list checked via isExcluded method
- Logs "Auto-tracked repository" at INFO level

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/discovery/discovery.go (AutoTrackThreshold, AutoTrack, normalizeScores)
- internal/config/types.go (AutoTrackThreshold in DiscoveryConfig)
