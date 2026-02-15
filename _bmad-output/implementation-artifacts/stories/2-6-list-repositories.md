# Story 2.6: List All Tracked Repositories

Status: done

## Story

As an **operator**,
I want to view all tracked repositories dynamically,
So that I can see the current state regardless of how repos were added.

## Acceptance Criteria

1. **Given** repos from config, CLI adds, and discovery
   **When** `github-radar list`
   **Then** all tracked repos are shown with current metrics

2. **Given** a category filter
   **When** `github-radar list --category <name>`
   **Then** only repos in that category are shown

3. **Given** a format option
   **When** `github-radar list --format <table|json|csv>`
   **Then** output is formatted accordingly for scripting

*(Addresses: FR5)*

## Tasks / Subtasks

- [x] Task 1: Create list subcommand (AC: #1-3)
  - [x] 1.1: Add "list" subcommand to CLI
  - [x] 1.2: Accept --category flag for filtering
  - [x] 1.3: Accept --format flag (table, json, csv)

- [x] Task 2: Load repositories (AC: #1)
  - [x] 2.1: Load config and parse repositories
  - [x] 2.2: Create Tracker from config repos
  - [x] 2.3: Handle parsing errors gracefully

- [x] Task 3: Implement output formats (AC: #3)
  - [x] 3.1: Table format (default) with headers and totals
  - [x] 3.2: JSON format for scripting
  - [x] 3.3: CSV format for spreadsheets

- [x] Task 4: Write tests
  - [x] 4.1: Test list with empty repos
  - [x] 4.2: Test list with repos
  - [x] 4.3: Test category filtering
  - [x] 4.4: Test JSON format
  - [x] 4.5: Test CSV format

## Dev Notes

### CLI Usage

```bash
# List all repositories (table format)
github-radar list

# Filter by category
github-radar list --category monitoring

# Output as JSON
github-radar list --format json

# Output as CSV
github-radar list --format csv
```

### Output Formats

**Table (default):**
```
REPOSITORY                               CATEGORIES
----------                               ----------
kubernetes/kubernetes                    cncf, container-orchestration
prometheus/prometheus                    cncf, monitoring

Total: 2 repositories
```

**JSON:**
```json
[
  {"repo": "kubernetes/kubernetes", "owner": "kubernetes", "name": "kubernetes", "categories": ["cncf"]}
]
```

**CSV:**
```
repo,owner,name,categories
kubernetes/kubernetes,kubernetes,kubernetes,cncf
```

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.6]
- Depends on Story 2.2: Define Repository Categories

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### Debug Log References

- All CLI tests pass including 5 list command tests
- Verified list with empty config
- Verified list with repositories
- Verified --category filtering
- Verified --format json output
- Verified --format csv output

### Completion Notes List

- Implemented List() method in RepoCmd
- Loads config and converts to TrackedRepoConfig
- Creates Tracker from config using LoadFromConfig()
- Supports --category flag for filtering
- Three output formats: table, json, csv
- Table format shows headers, repo, categories, and total count
- Empty list shows helpful message

### File List

**Modified files:**
- internal/cli/repo.go - Added List() and format methods
- internal/cli/repo_test.go - Added 5 tests
- internal/cli/root.go - Added list command routing
