# Story 1.2: Load YAML Configuration File

Status: review

## Story

As an **operator**,
I want to specify my configuration in a YAML file,
So that I can easily manage settings without recompiling.

## Acceptance Criteria

1. **Given** a valid YAML config file exists at the specified path
   **When** the application starts with `--config ./config.yaml`
   **Then** all configuration values are loaded into memory

2. **Given** no `--config` flag is provided
   **When** the `GITHUB_RADAR_CONFIG` environment variable is set
   **Then** the config file path is read from that environment variable

3. **Given** neither flag nor env var is provided
   **When** the application starts
   **Then** it looks for `./github-radar.yaml` in the current directory

4. **Given** a config file is loaded
   **When** the configuration struct is populated
   **Then** all sections are parsed correctly: github, otel, discovery, scoring, exclusions

5. **Given** a config file path is specified
   **When** the file does not exist
   **Then** a clear error message is returned indicating the file path

*(Addresses: FR46)*

## Tasks / Subtasks

- [x] Task 1: Define configuration types (AC: #4)
  - [x] 1.1: Create config struct types in internal/config/types.go
  - [x] 1.2: Define GithubConfig, OtelConfig, DiscoveryConfig, ScoringConfig structs
  - [x] 1.3: Write unit tests for struct field tags

- [x] Task 2: Implement YAML loader (AC: #1, #5)
  - [x] 2.1: Add gopkg.in/yaml.v3 dependency
  - [x] 2.2: Implement Load function that reads and parses YAML file
  - [x] 2.3: Handle file not found with clear error message
  - [x] 2.4: Write unit tests for YAML loading (10 tests)

- [x] Task 3: Implement config path resolution (AC: #1, #2, #3)
  - [x] 3.1: Implement ResolveConfigPath function
  - [x] 3.2: Check --config flag first, then GITHUB_RADAR_CONFIG env, then default path
  - [x] 3.3: Write unit tests for path resolution priority

- [x] Task 4: Integrate with CLI (AC: #1, #2, #3)
  - [x] 4.1: Add --config flag to root command
  - [x] 4.2: Load config on application start
  - [x] 4.3: Write integration test for CLI config loading (9 tests)

## Dev Notes

### Architecture Requirements

**Config Format:** YAML with snake_case keys per architecture spec.

**Config Path Resolution Priority:**
1. `--config <path>` CLI flag (highest priority)
2. `GITHUB_RADAR_CONFIG` environment variable
3. `./github-radar.yaml` in current directory (default)

### Configuration Struct Design

```go
// Config represents the complete github-radar configuration.
type Config struct {
    GitHub     GithubConfig     `yaml:"github"`
    Otel       OtelConfig       `yaml:"otel"`
    Discovery  DiscoveryConfig  `yaml:"discovery"`
    Scoring    ScoringConfig    `yaml:"scoring"`
    Exclusions []string         `yaml:"exclusions"`
}

type GithubConfig struct {
    Token     string `yaml:"token"`
    RateLimit int    `yaml:"rate_limit"`
}

type OtelConfig struct {
    Endpoint    string            `yaml:"endpoint"`
    Headers     map[string]string `yaml:"headers"`
    ServiceName string            `yaml:"service_name"`
}

type DiscoveryConfig struct {
    Enabled            bool `yaml:"enabled"`
    MinStars           int  `yaml:"min_stars"`
    MaxAgeDays         int  `yaml:"max_age_days"`
    AutoTrackThreshold int  `yaml:"auto_track_threshold"`
}

type ScoringConfig struct {
    Weights WeightConfig `yaml:"weights"`
}

type WeightConfig struct {
    StarVelocity      float64 `yaml:"star_velocity"`
    StarAcceleration  float64 `yaml:"star_acceleration"`
    ContributorGrowth float64 `yaml:"contributor_growth"`
    PRVelocity        float64 `yaml:"pr_velocity"`
    IssueVelocity     float64 `yaml:"issue_velocity"`
}
```

### Error Handling Pattern

```go
// ConfigError wraps config-related errors with context
type ConfigError struct {
    Path    string
    Message string
    Err     error
}

func (e *ConfigError) Error() string {
    if e.Err != nil {
        return fmt.Sprintf("config %s: %s: %v", e.Path, e.Message, e.Err)
    }
    return fmt.Sprintf("config %s: %s", e.Path, e.Message)
}
```

### Dependencies

Add to go.mod:
```
gopkg.in/yaml.v3 v3.0.1
```

### File Locations

- `internal/config/types.go` - Config struct definitions
- `internal/config/loader.go` - YAML loading logic
- `internal/config/loader_test.go` - Unit tests
- `tests/fixtures/config.yaml` - Test fixture

### Testing Strategy

1. Unit tests for struct parsing with various YAML inputs
2. Unit tests for path resolution with mock env vars
3. Integration test with real file loading
4. Error case tests: missing file, invalid YAML, missing required fields

### References

- [Source: _bmad-output/planning-artifacts/architecture.md#Data Format Conventions]
- [Source: _bmad-output/planning-artifacts/architecture.md#Configuration Validation]
- [Source: _bmad-output/planning-artifacts/prd.md#Configuration Schema]
- [Source: _bmad-output/planning-artifacts/epics.md#Story 1.2: Load YAML Configuration File]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### Debug Log References

- Added gopkg.in/yaml.v3 v3.0.1 dependency
- All 19 tests pass (10 config + 9 CLI tests)
- go build ./... passes
- CLI tested: `go run ./cmd/github-radar --config ./tests/fixtures/config.yaml --verbose`

### Completion Notes List

- Implemented Config struct with all required sections (github, otel, discovery, scoring, exclusions)
- Implemented ConfigError type with context wrapping for clear error messages
- Implemented DefaultConfig() function for sensible defaults
- Implemented Load() function for YAML parsing with error handling
- Implemented ResolveConfigPath() with 3-tier priority (flag > env > default)
- Implemented CLI integration with --config, --verbose, --dry-run flags
- Partial config files use defaults for unspecified values

### File List

**New files:**
- internal/config/types.go - Config struct definitions and defaults
- internal/config/loader_test.go - 10 unit tests for config loading
- internal/cli/root_test.go - 9 unit tests for CLI
- tests/fixtures/config.yaml - Test configuration fixture

**Modified files:**
- internal/config/loader.go - YAML loading implementation
- internal/cli/root.go - CLI with config integration
- cmd/github-radar/main.go - Updated to use CLI
- go.mod - Added yaml.v3 dependency
- go.sum - Updated with yaml.v3 checksums

