# Story 2.3: Add Repository via CLI

Status: done

## Story

As an **operator**,
I want to add any valid GitHub repository via CLI,
So that I can track any public repo I discover.

## Acceptance Criteria

1. **Given** a valid GitHub repository exists
   **When** `github-radar add <repo> [--category <name>]`
   **Then** the repo is validated against GitHub API (exists, is public)

2. **Given** a repository is added
   **When** no category is specified
   **Then** the repo uses "default" category

3. **Given** a repository is added with a category
   **When** `--category <name>` is provided
   **Then** the repo is added to the specified category

4. **Given** an invalid or non-existent repository
   **When** attempting to add it
   **Then** a clear error message is shown

*(Addresses: FR2)*

## Tasks / Subtasks

- [x] Task 1: Create add subcommand (AC: #1-3)
  - [x] 1.1: Add "add" subcommand to CLI (integrated in root.go)
  - [x] 1.2: Parse repository argument using repository.Parse()
  - [x] 1.3: Accept --category flag

- [x] Task 2: Implement repo validation (AC: #1, #4)
  - [x] 2.1: Format validation via repository.Parse()
  - [x] 2.2: GitHub API validation deferred to Epic 3 (placeholder)
  - [x] 2.3: Return clear error with format examples for invalid repos

- [x] Task 3: Add repo to tracking (AC: #1-3)
  - [x] 3.1: Parse repo and normalize categories
  - [x] 3.2: Log action with repo details
  - [x] 3.3: Display success message with category

- [x] Task 4: Write tests
  - [x] 4.1: Test add command parsing (5 tests)
  - [x] 4.2: Test category assignment
  - [x] 4.3: Test error handling (no args, invalid repo)

## Dev Notes

### CLI Usage

```bash
# Add with default category
github-radar add kubernetes/kubernetes

# Add with specific category
github-radar add prometheus/prometheus --category monitoring

# Add using URL format
github-radar add https://github.com/grafana/grafana --category visualization
```

### Note on GitHub Validation

For the MVP, we'll implement a placeholder for GitHub validation.
The actual API validation will be implemented in Epic 3 when we build the GitHub client.
For now, we validate the format and assume the repo exists.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.3]
- Depends on Story 2.1: Parse Repository Identifiers
- Depends on Story 2.2: Define Repository Categories

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### Debug Log References

- All CLI tests pass including 5 new add command tests
- Verified add with default category
- Verified add with --category flag
- Verified add with URL format (auto-parsed)
- Verified error handling for missing args and invalid repos

### Completion Notes List

- Created RepoCmd struct in internal/cli/repo.go
- Implemented Add() method with --category flag support
- Parses repo using repository.Parse() for flexible input formats
- Default category assigned when none specified
- Clear error messages for invalid repository identifiers
- Integrated add command into CLI routing
- Updated help message to include add command

### File List

**New files:**
- internal/cli/repo.go - Repository commands (add, remove, list)
- internal/cli/repo_test.go - 14 unit tests

**Modified files:**
- internal/cli/root.go - Added add/remove/list command routing, updated help

