# Story 6.4: Configure Discovery Topics

Status: done

## Story

As an **operator**,
I want to specify which topics to scan,
So that discovery focuses on my areas of interest.

## Acceptance Criteria

1. **Given** config section `discovery.topics: [kubernetes, ebpf, llm]`
   **When** configuration is loaded
   **Then** topics list is validated (non-empty strings)

2. **Given** topics configured
   **When** discovery runs
   **Then** each topic is searched separately

3. **Given** empty topics list
   **When** DiscoverAll is called
   **Then** discovery is skipped with info log

## Tasks / Subtasks

- [x] Task 1: Add Topics to DiscoveryConfig type
- [x] Task 2: Add Topics to discovery.Config
- [x] Task 3: Implement DiscoverAll to iterate over topics
- [x] Task 4: Log when no topics configured

## Completion Notes

- Topics field added to config/types.go DiscoveryConfig
- Topics field in discovery.Config struct
- DiscoverAll iterates and calls DiscoverTopic for each
- Returns nil, nil when Topics list is empty
- Logs "No topics configured, skipping discovery" at INFO

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/config/types.go (Topics in DiscoveryConfig)
- internal/discovery/discovery.go (DiscoverAll)
