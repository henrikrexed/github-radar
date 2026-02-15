# Story 2.1: Parse Repository Identifiers Flexibly

Status: done

## Story

As an **operator**,
I want the system to accept repositories in multiple formats,
So that I can add repos from various sources without format constraints.

## Acceptance Criteria

1. **Given** a repository in `owner/repo` format
   **When** the system parses it
   **Then** owner and repo are extracted correctly

2. **Given** a repository URL `https://github.com/owner/repo`
   **When** the system parses it
   **Then** owner and repo are extracted correctly

3. **Given** a repository URL `github.com/owner/repo` (no scheme)
   **When** the system parses it
   **Then** owner and repo are extracted correctly

4. **Given** an invalid repository identifier
   **When** the system parses it
   **Then** a clear error is returned with expected format examples

5. **Given** a URL with trailing slashes or .git suffix
   **When** the system parses it
   **Then** these are handled gracefully

## Tasks / Subtasks

- [x] Task 1: Create repository package (AC: #1-5)
  - [x] 1.1: Create internal/repository/repo.go
  - [x] 1.2: Define Repo struct with Owner and Name fields
  - [x] 1.3: Implement Parse function (named Parse instead of ParseRepo for cleaner API)

- [x] Task 2: Handle multiple formats (AC: #1, #2, #3, #5)
  - [x] 2.1: Parse owner/repo format
  - [x] 2.2: Parse https://github.com/owner/repo URLs
  - [x] 2.3: Parse github.com/owner/repo (no scheme)
  - [x] 2.4: Handle .git suffix and trailing slashes

- [x] Task 3: Error handling (AC: #4)
  - [x] 3.1: Return clear error with format examples
  - [x] 3.2: Validate owner and repo are non-empty

- [x] Task 4: Write comprehensive tests
  - [x] 4.1: Test all valid formats (20 tests)
  - [x] 4.2: Test error cases
  - [x] 4.3: Test edge cases (trailing slashes, .git)

## Dev Notes

### Repo Struct

```go
type Repo struct {
    Owner string
    Name  string
}

func (r Repo) FullName() string {
    return r.Owner + "/" + r.Name
}
```

### Supported Formats

| Input | Owner | Name |
|-------|-------|------|
| `kubernetes/kubernetes` | kubernetes | kubernetes |
| `https://github.com/kubernetes/kubernetes` | kubernetes | kubernetes |
| `github.com/kubernetes/kubernetes` | kubernetes | kubernetes |
| `https://github.com/kubernetes/kubernetes.git` | kubernetes | kubernetes |
| `https://github.com/kubernetes/kubernetes/` | kubernetes | kubernetes |

### Error Message Format

```
invalid repository identifier: "foo"
Expected formats:
  - owner/repo
  - https://github.com/owner/repo
  - github.com/owner/repo
```

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.1]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### Debug Log References

- All 20 tests pass in internal/repository package
- Verified parsing of all formats: owner/repo, HTTPS URLs, HTTP URLs, no-scheme URLs
- Verified handling of .git suffix and trailing slashes
- Verified whitespace trimming
- Verified error handling with clear format examples

### Completion Notes List

- Created internal/repository package with Repo struct and Parse function
- Parse() supports 7 input formats as specified
- ParseError provides clear error message with expected format examples
- MustParse() convenience function for tests/initialization
- FullName() and String() methods return owner/repo format
- Internal helpers: parseGitHubURL() and parseOwnerRepo()
- Validates non-empty owner/name and no whitespace in identifiers

### File List

**New files:**
- internal/repository/repo.go - Repository parsing implementation
- internal/repository/repo_test.go - 20 comprehensive unit tests

