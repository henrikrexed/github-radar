# Story 1.5: Graceful Error Handling for Config Failures

Status: review

## Story

As an **operator**,
I want clear error messages when configuration fails,
So that I can quickly diagnose and fix issues.

## Acceptance Criteria

1. **Given** a config file that cannot be read (permissions, corruption)
   **When** the application starts
   **Then** an error message explains the file access issue

2. **Given** invalid YAML syntax in the config file
   **When** the application starts
   **Then** an error message indicates the YAML parsing error with line context

3. **Given** any configuration error
   **When** the application exits
   **Then** exit code is 1 (not 0)

4. **Given** a successful configuration load
   **When** the application starts
   **Then** exit code is 0

5. **Given** the --verbose flag
   **When** a configuration error occurs
   **Then** additional debug information is displayed

*(Addresses: FR54, NFR10)*

## Tasks / Subtasks

- [x] Task 1: Enhance error messages (AC: #1, #2)
  - [x] 1.1: Improve file read error messages with path context
  - [x] 1.2: Add hints to error messages (how to fix)
  - [x] 1.3: Handle permission errors separately
  - [x] 1.4: Write tests for enhanced error messages (5 new tests)

- [x] Task 2: Implement proper exit codes (AC: #3, #4)
  - [x] 2.1: Return exit code 1 for config errors
  - [x] 2.2: Return exit code 0 for success
  - [x] 2.3: Verified exit codes in CLI

- [x] Task 3: Add verbose error output (AC: #5)
  - [x] 3.1: Add Verbose() method to ConfigError with cause and hint
  - [x] 3.2: Display config path resolution steps in verbose mode
  - [x] 3.3: Show which resolution method was used (flag, env, default)

- [x] Task 4: Integration testing (AC: #1-5)
  - [x] 4.1: Test with nonexistent file (verified)
  - [x] 4.2: Test with malformed YAML (verified)
  - [x] 4.3: Test exit codes (0 for success, 1 for error)

## Dev Notes

### Error Message Format

```
Error: config /path/to/config.yaml: file not found
  Tried: /path/to/config.yaml
  Hint: Create config file or use --config flag to specify path
```

### Exit Codes (per FR54)

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Fatal error (config missing, auth failure, validation error) |

### Verbose Mode Additions

When --verbose is set:
- Show config path resolution steps
- Show which env vars are being expanded
- Show validation checks being performed

### References

- [Source: _bmad-output/planning-artifacts/architecture.md#Error Handling Patterns]
- [Source: _bmad-output/planning-artifacts/prd.md#FR54]
- [Source: _bmad-output/planning-artifacts/epics.md#Story 1.5]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### Debug Log References

- All tests pass
- Verified exit code 1 for errors, 0 for success
- Tested verbose mode with config path resolution display
- Tested hints appear in verbose error output

### Completion Notes List

- Added Hint field to ConfigError for user guidance
- Added Verbose() method to ConfigError for detailed output
- Added hints for: file not found, permission denied, env var errors, YAML syntax
- Enhanced CLI config commands to show path resolution in verbose mode
- Permission errors now handled separately with specific hint
- Changed "invalid YAML" message to "invalid YAML syntax" for clarity

### File List

**Modified files:**
- internal/config/types.go - Added Hint field and Verbose() method to ConfigError
- internal/config/loader.go - Added hints to all error cases, handle permission errors
- internal/config/loader_test.go - Added 5 tests for hint and verbose functionality
- internal/cli/config.go - Added verbose path resolution, use Verbose() for errors

