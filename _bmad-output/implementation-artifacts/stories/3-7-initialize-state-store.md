# Story 3.7: Initialize State Store

Status: done

## Story

As the **system**,
I want a JSON-based state persistence layer,
So that data survives between runs for delta calculation.

## Acceptance Criteria

1. **Given** a state file path (default or configured via FR35)
   **When** the system starts
   **Then** existing state is loaded if present (FR32)

2. **Given** no state file exists
   **When** the system starts
   **Then** missing state file initializes empty state

3. **Given** state file path configuration
   **When** the system starts
   **Then** state file path can be set via `--state` flag or env var

## Tasks / Subtasks

- [x] Task 1: Define state data structures
  - [x] 1.1: Create State struct with repos and metadata
  - [x] 1.2: Create RepoState struct for per-repo data
  - [x] 1.3: Create DiscoveryState for topic tracking

- [x] Task 2: Implement state loading
  - [x] 2.1: Load from JSON file
  - [x] 2.2: Handle missing file gracefully (initialize empty)
  - [x] 2.3: Handle corrupt file with error

- [x] Task 3: Implement state saving
  - [x] 3.1: Write to JSON file (pretty printed)
  - [x] 3.2: Use atomic write-rename pattern (NFR12)

- [x] Task 4: Write tests

## Completion Notes

- Created State, RepoState, DiscoveryState structs
- Store is thread-safe with sync.RWMutex
- GetRepoState returns copies to prevent external mutation
- Atomic save with temp file + rename
- Auto-creates directories for state file path
- Tracks modification status for efficient saves

### Files Created

- internal/state/store.go (complete rewrite)
- internal/state/store_test.go (new)

## Dev Notes

### State Structure

```go
type State struct {
    Version     int                     `json:"version"`
    LastScan    time.Time               `json:"last_scan"`
    Repos       map[string]RepoState    `json:"repos"`
    Discovery   DiscoveryState          `json:"discovery"`
}

type RepoState struct {
    Stars           int       `json:"stars"`
    StarsPrev       int       `json:"stars_prev"`
    LastCollected   time.Time `json:"last_collected"`
    StarVelocity    float64   `json:"star_velocity"`
    StarAcceleration float64  `json:"star_acceleration"`
    ETag            string    `json:"etag"`
    LastModified    string    `json:"last_modified"`
}
```

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List
