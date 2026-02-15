# Story 1.1: Initialize Go Project Structure

Status: review

## Story

As an **operator**,
I want the project scaffolded with proper Go structure,
So that development can begin with correct conventions.

## Acceptance Criteria

1. **Given** a new project directory
   **When** `go mod init github.com/hrexed/github-radar` is run
   **Then** go.mod is created with Go 1.22+ requirement

2. **Given** the go.mod file exists
   **When** the directory structure is created
   **Then** it matches Architecture specification:
   - `cmd/github-radar/` - Single binary entry point
   - `internal/cli/` - CLI command implementations
   - `internal/daemon/` - Background scanner daemon
   - `internal/github/` - GitHub API client wrapper
   - `internal/scoring/` - Growth score calculation
   - `internal/state/` - JSON state persistence
   - `internal/metrics/` - OpenTelemetry metrics export
   - `internal/config/` - Configuration management
   - `pkg/types/` - Shared public types
   - `configs/` - Example configuration files
   - `tests/integration/` - Integration tests
   - `tests/fixtures/` - Test data

3. **Given** the project structure is created
   **When** developer inspects the files
   **Then** placeholder files exist in each directory with package declarations

4. **Given** the project is initialized
   **When** `go build ./...` is run
   **Then** the build succeeds with no errors

5. **Given** the project is initialized
   **When** `go test ./...` is run
   **Then** tests pass (even if empty/placeholder)

## Tasks / Subtasks

- [x] Task 1: Initialize Go module (AC: #1)
  - [x] 1.1: Run `go mod init github.com/hrexed/github-radar`
  - [x] 1.2: Verify go.mod contains `go 1.22` directive (Go 1.24.4 - exceeds requirement)
  - [x] 1.3: Write unit test for go version verification

- [x] Task 2: Create directory structure (AC: #2)
  - [x] 2.1: Create `cmd/github-radar/` directory
  - [x] 2.2: Create all `internal/` subdirectories (cli, daemon, github, scoring, state, metrics, config)
  - [x] 2.3: Create `pkg/types/` directory
  - [x] 2.4: Create `configs/` directory
  - [x] 2.5: Create `tests/integration/` and `tests/fixtures/` directories

- [x] Task 3: Create placeholder files (AC: #3)
  - [x] 3.1: Create `cmd/github-radar/main.go` with main package and minimal main func
  - [x] 3.2: Create placeholder `.go` files in each internal package with correct package declaration
  - [x] 3.3: Create `pkg/types/types.go` with package declaration
  - [x] 3.4: Create `configs/github-radar.example.yaml` with minimal config structure

- [x] Task 4: Create supporting files (AC: #4, #5)
  - [x] 4.1: Create `.gitignore` with Go-specific ignores
  - [x] 4.2: Create `Makefile` with build, test, lint targets
  - [x] 4.3: Create `Dockerfile` placeholder with multi-stage build structure
  - [x] 4.4: Create basic `README.md` with project name and description

- [x] Task 5: Validate build and test (AC: #4, #5)
  - [x] 5.1: Run `go build ./...` and verify success
  - [x] 5.2: Run `go test ./...` and verify success
  - [x] 5.3: Run `go vet ./...` and verify no issues

## Dev Notes

### Architecture Requirements

**Language:** Go 1.22+ (supersedes PRD's TypeScript specification - this is intentional per Architecture Decision Document)

**Project Pattern:** Standard Go project layout with cmd/, internal/, pkg/ structure.

**Critical Convention:** This project uses a client/daemon separation architecture:
- CLI is non-blocking (reads config/state files, queries HTTP status)
- Daemon runs in background (scheduled scanning, OTLP export)

### Go Naming Conventions (MUST FOLLOW)

| Element | Convention | Example |
|---------|------------|---------|
| Packages | lowercase, single word | `github`, `scoring`, `state` |
| Exported functions | PascalCase | `LoadConfig()`, `NewClient()` |
| Unexported functions | camelCase | `parseToken()`, `buildURL()` |
| Files | snake_case.go | `rate_limiter.go`, `growth_score.go` |
| Test files | `*_test.go` same package | `score_test.go` |
| Interfaces | `-er` suffix when applicable | `Scanner`, `Exporter`, `Loader` |
| Constants | PascalCase (exported) | `DefaultRateLimit`, `MaxRetries` |

### Data Format Conventions

| File | Format | Key Convention |
|------|--------|----------------|
| Config | YAML | snake_case |
| State | JSON | snake_case |
| Env vars | SCREAMING_SNAKE_CASE | `GITHUB_TOKEN` |
| Timestamps | ISO 8601 / RFC 3339 | `2026-02-14T06:00:00Z` |

### Expected Directory Structure

```
github-radar/
├── README.md
├── LICENSE
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile
├── .gitignore
├── cmd/
│   └── github-radar/
│       └── main.go             # Single binary entry point
├── internal/
│   ├── cli/                    # CLI command implementations
│   │   └── root.go             # Placeholder
│   ├── daemon/                 # Background scanner daemon
│   │   └── daemon.go           # Placeholder
│   ├── github/                 # GitHub API client wrapper
│   │   └── client.go           # Placeholder
│   ├── scoring/                # Growth score calculation
│   │   └── score.go            # Placeholder
│   ├── state/                  # JSON state persistence
│   │   └── store.go            # Placeholder
│   ├── metrics/                # OpenTelemetry metrics export
│   │   └── exporter.go         # Placeholder
│   └── config/                 # Configuration management
│       └── loader.go           # Placeholder
├── pkg/
│   └── types/                  # Shared public types
│       └── types.go            # Placeholder
├── configs/
│   └── github-radar.example.yaml
└── tests/
    ├── integration/
    └── fixtures/
```

### Placeholder File Pattern

Each placeholder file should follow this pattern:

```go
// Package <name> provides <brief description>.
package <name>

// TODO: Implementation pending Story X.Y
```

### Example Placeholder for main.go

```go
// Package main is the entry point for github-radar CLI.
package main

import (
    "fmt"
    "os"
)

func main() {
    fmt.Println("github-radar - GitHub Trend Scanner")
    os.Exit(0)
}
```

### Makefile Targets Required

```makefile
.PHONY: build test lint clean

build:
	go build -o bin/github-radar ./cmd/github-radar

test:
	go test ./...

lint:
	go vet ./...

clean:
	rm -rf bin/
```

### .gitignore Contents

```
# Binaries
bin/
*.exe
github-radar

# Dependencies
vendor/

# IDE
.idea/
.vscode/
*.swp

# OS
.DS_Store

# Build
dist/

# Test
coverage.out

# Config with secrets
config.yaml
!configs/*.example.yaml

# State file
*.state.json
```

### Example Config Structure (configs/github-radar.example.yaml)

```yaml
# GitHub Radar Configuration
# Copy to config.yaml and fill in values

github:
  token: ${GITHUB_TOKEN}
  rate_limit: 4000

otel:
  endpoint: http://localhost:4318
  headers:
    Authorization: "Api-Token ${DT_API_TOKEN}"
  service_name: github-radar

discovery:
  enabled: true
  min_stars: 100
  max_age_days: 90
  auto_track_threshold: 50

scoring:
  weights:
    star_velocity: 2.0
    star_acceleration: 3.0
    contributor_growth: 1.5
    pr_velocity: 1.0
    issue_velocity: 0.5

exclusions:
  - example-org/example-repo
```

### Testing Requirements

- All placeholder packages should compile without errors
- Main function should be runnable (`go run ./cmd/github-radar`)
- `go vet` should pass with no issues

### Project Structure Notes

- All paths use standard Go project layout
- `internal/` packages are not importable by external packages
- `pkg/types/` contains shared types that may be imported externally
- Single binary approach: both CLI and daemon are built from same cmd entry point

### References

- [Source: _bmad-output/planning-artifacts/architecture.md#Updated Project Structure]
- [Source: _bmad-output/planning-artifacts/architecture.md#Go Code Naming Conventions]
- [Source: _bmad-output/planning-artifacts/architecture.md#Implementation Handoff]
- [Source: _bmad-output/planning-artifacts/epics.md#Story 1.1: Initialize Go Project Structure]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### Debug Log References

- go mod init completed successfully
- go build ./... passed with no errors
- go test ./... passed (1 test, all packages compiled)
- go vet ./... passed with no issues
- make build completed successfully

### Completion Notes List

- Initialized Go 1.24.4 module (exceeds 1.22+ requirement)
- Created standard Go project layout with cmd/, internal/, pkg/ structure
- All placeholder files include proper package declarations and TODO comments
- Main binary runs successfully: `github-radar dev - GitHub Trend Scanner`
- Makefile includes build, test, lint, clean, docker, release targets
- Dockerfile uses multi-stage build with distroless base image
- Example config includes all sections from architecture spec

### File List

**New files created:**
- go.mod
- cmd/github-radar/main.go
- cmd/github-radar/main_test.go
- internal/cli/root.go
- internal/daemon/daemon.go
- internal/github/client.go
- internal/scoring/score.go
- internal/state/store.go
- internal/metrics/exporter.go
- internal/config/loader.go
- pkg/types/types.go
- configs/github-radar.example.yaml
- .gitignore
- Makefile
- Dockerfile
- README.md

**Directories created:**
- cmd/github-radar/
- internal/cli/
- internal/daemon/
- internal/github/
- internal/scoring/
- internal/state/
- internal/metrics/
- internal/config/
- pkg/types/
- configs/
- tests/integration/
- tests/fixtures/
- bin/ (build artifact)

