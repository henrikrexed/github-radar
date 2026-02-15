# Story 1.4: Validate Configuration on Startup

Status: review

## Story

As an **operator**,
I want the CLI to validate my configuration on startup,
So that I get clear error messages for misconfigurations.

## Acceptance Criteria

1. **Given** a config with missing required fields (github.token, otel.endpoint)
   **When** the config is loaded
   **Then** a validation error lists all missing required fields

2. **Given** a config with invalid values (rate_limit < 0, min_stars < 0)
   **When** the config is loaded
   **Then** a validation error explains the constraint violation

3. **Given** a config with invalid OTLP endpoint URL
   **When** the config is loaded
   **Then** a validation error indicates the URL is malformed

4. **Given** a config with valid values for all fields
   **When** the config is validated
   **Then** no error is returned

5. **Given** a `config validate` CLI command
   **When** the operator runs it
   **Then** the config is validated and results are displayed

*(Addresses: FR47)*

## Tasks / Subtasks

- [x] Task 1: Define validation rules (AC: #1, #2, #3)
  - [x] 1.1: Create Validate method on Config struct
  - [x] 1.2: Define required fields: github.token, otel.endpoint
  - [x] 1.3: Define value constraints (rate_limit > 0, min_stars >= 0, etc.)
  - [x] 1.4: Validate URL format for otel.endpoint (http/https, valid host)

- [x] Task 2: Implement ValidationError type (AC: #1, #2, #3)
  - [x] 2.1: Create ValidationError with list of issues
  - [x] 2.2: Format error message to list all violations
  - [x] 2.3: Write unit tests for error formatting

- [x] Task 3: Write validation tests (AC: #1, #2, #3, #4)
  - [x] 3.1: Test missing required fields
  - [x] 3.2: Test invalid numeric values
  - [x] 3.3: Test invalid URL format (15 validation tests)
  - [x] 3.4: Test valid configuration passes

- [x] Task 4: Implement config validate command (AC: #5)
  - [x] 4.1: Add validate and show subcommands to CLI
  - [x] 4.2: Load and validate config, display results
  - [x] 4.3: Write CLI tests for config commands (10 tests)

## Dev Notes

### Architecture Requirements

Per FR47: System validates configuration on startup and reports errors.

### Required Fields

| Field | Requirement |
|-------|-------------|
| `github.token` | Must be non-empty |
| `otel.endpoint` | Must be valid URL (http:// or https://) |

### Value Constraints

| Field | Constraint |
|-------|------------|
| `github.rate_limit` | > 0 |
| `discovery.min_stars` | >= 0 |
| `discovery.max_age_days` | > 0 |
| `discovery.auto_track_threshold` | >= 0 |
| `scoring.weights.*` | >= 0 |

### ValidationError Format

```
configuration validation failed:
  - github.token: required field is empty
  - github.rate_limit: must be greater than 0, got -1
  - otel.endpoint: invalid URL format
```

### Implementation Pattern

```go
// Validate checks the configuration for errors.
// Returns nil if valid, or ValidationError with all issues.
func (c *Config) Validate() error {
    var issues []string

    if c.GitHub.Token == "" {
        issues = append(issues, "github.token: required field is empty")
    }
    // ... more checks

    if len(issues) > 0 {
        return &ValidationError{Issues: issues}
    }
    return nil
}
```

### References

- [Source: _bmad-output/planning-artifacts/architecture.md#Configuration Validation]
- [Source: _bmad-output/planning-artifacts/prd.md#FR47]
- [Source: _bmad-output/planning-artifacts/epics.md#Story 1.4]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### Debug Log References

- All tests pass (15 validation + 10 CLI config tests)
- go build ./... passes
- Tested: `go run ./cmd/github-radar config validate`
- Tested: `go run ./cmd/github-radar config show`
- Tested: `go run ./cmd/github-radar help`

### Completion Notes List

- Implemented Validate() method on Config struct
- Validates: required fields, numeric constraints, URL format
- ValidationError collects all issues and formats them clearly
- Added `config validate` command - validates and reports
- Added `config show` command - displays config with masked secrets
- Added `help` command with usage information
- Added subcommand routing to CLI
- Bonus: ValidateAndLoad() combines load + validate for startup

### File List

**New files:**
- internal/config/validate.go - Validation logic and ValidationError
- internal/config/validate_test.go - 15 validation tests
- internal/cli/config.go - Config subcommands (validate, show)
- internal/cli/config_test.go - 10 CLI config tests

**Modified files:**
- internal/cli/root.go - Added subcommand routing and help

