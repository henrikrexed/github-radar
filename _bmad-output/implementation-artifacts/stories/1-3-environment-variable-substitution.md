# Story 1.3: Environment Variable Substitution in Config

Status: review

## Story

As an **operator**,
I want to use `${ENV_VAR}` syntax in my config file,
So that I can inject secrets without hardcoding them.

## Acceptance Criteria

1. **Given** a config with `token: ${GITHUB_TOKEN}`
   **When** the config is loaded and `GITHUB_TOKEN=abc123` is set
   **Then** the token value resolves to `abc123`

2. **Given** a config with `${UNDEFINED_VAR}`
   **When** the config is loaded and the variable is not set
   **Then** a clear error message indicates the missing variable name

3. **Given** a config with multiple env var references
   **When** the config is loaded
   **Then** all references are substituted correctly

4. **Given** a config with `${VAR:-default}` syntax
   **When** the variable is not set
   **Then** the default value is used

5. **Given** a config with literal `${...}` that should not be substituted
   **When** the escape sequence `$${...}` is used
   **Then** the literal `${...}` is preserved

*(Addresses: FR42, NFR6)*

## Tasks / Subtasks

- [x] Task 1: Implement env var substitution (AC: #1, #3)
  - [x] 1.1: Create ExpandEnvVars function in internal/config/expand.go
  - [x] 1.2: Parse ${VAR} syntax and replace with os.Getenv
  - [x] 1.3: Write unit tests for basic substitution (14 tests)

- [x] Task 2: Handle missing variables (AC: #2)
  - [x] 2.1: Track unset variables during expansion
  - [x] 2.2: Return ExpansionError listing all missing variables
  - [x] 2.3: Write unit tests for missing variable handling

- [x] Task 3: Support default values (AC: #4)
  - [x] 3.1: Parse ${VAR:-default} syntax
  - [x] 3.2: Use default when variable is unset (including empty default)
  - [x] 3.3: Write unit tests for default value syntax

- [x] Task 4: Support escape sequences (AC: #5)
  - [x] 4.1: Handle $${VAR} as literal ${VAR}
  - [x] 4.2: Write unit tests for escape handling

- [x] Task 5: Integrate with config loader (AC: #1-5)
  - [x] 5.1: Call ExpandEnvVars before YAML parsing in Load()
  - [x] 5.2: Added 3 integration tests for env var expansion
  - [x] 5.3: Tested with CLI using real env vars

## Dev Notes

### Architecture Requirements

Per NFR6: System must read credentials exclusively from environment variables.
Per FR42: Operator can configure GitHub token via environment variable substitution.

### Expansion Patterns

| Pattern | Behavior |
|---------|----------|
| `${VAR}` | Replace with env var value, error if unset |
| `${VAR:-default}` | Replace with env var or default if unset |
| `$${VAR}` | Literal `${VAR}` (escape) |

### Implementation Approach

```go
// expandEnvVars expands ${VAR} and ${VAR:-default} patterns in content.
// Returns error if any required variables are missing.
func expandEnvVars(content []byte) ([]byte, error) {
    // Regex: \$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}
    // Group 1: variable name
    // Group 2: optional default value
}
```

### Error Format

```
config expansion failed: missing environment variables: GITHUB_TOKEN, DT_API_TOKEN
```

### Security Considerations

- Never log the expanded values (may contain secrets)
- Error messages should only mention variable names, not values

### References

- [Source: _bmad-output/planning-artifacts/architecture.md#Configuration Validation]
- [Source: _bmad-output/planning-artifacts/prd.md#Configuration Schema]
- [Source: _bmad-output/planning-artifacts/epics.md#Story 1.3: Environment Variable Substitution in Config]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### Debug Log References

- All 27 config tests pass (14 expand + 13 loader tests)
- go build ./... passes
- CLI tested with env vars: `GITHUB_TOKEN=x DT_API_TOKEN=y go run ./cmd/github-radar --config ./tests/fixtures/config.yaml --verbose`

### Completion Notes List

- Implemented ExpandEnvVars() with regex pattern matching for ${VAR} and ${VAR:-default}
- Implemented ExpansionError type that lists all missing variables
- Implemented escape sequence handling: $${VAR} becomes literal ${VAR}
- Integrated expansion into Load() - runs before YAML parsing
- Fixed edge case: empty default values (${VAR:-}) now work correctly
- Error messages only show variable names, never values (security)

### File List

**New files:**
- internal/config/expand.go - Env var expansion logic
- internal/config/expand_test.go - 14 unit tests for expansion

**Modified files:**
- internal/config/loader.go - Added ExpandEnvVars call before YAML parsing
- internal/config/loader_test.go - Added 3 integration tests for env vars

