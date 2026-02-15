---
stepsCompleted: [1, 2, 3, 4, 5, 6, 7, 8]
workflowStatus: complete
completedAt: 2026-02-14
inputDocuments:
  - type: prd
    path: prd.md
    description: Complete PRD with 55 FRs, 14 NFRs, 5 user journeys for GitHub Radar CLI tool
  - type: prd-validation
    path: prd-validation-report.md
    description: Validation report confirming PRD quality (5/5 rating, all checks passed)
  - type: product-brief
    path: product-brief-github-trend-scanner-2026-02-14.md
    description: Initial product brief for GitHub Trend Scanner
workflowType: 'architecture'
project_name: 'github trend scanner'
user_name: 'Henrik.rexed'
date: '2026-02-14'
---

# Architecture Decision Document

_This document builds collaboratively through step-by-step discovery. Sections are appended as we work through each architectural decision together._

## Project Context Analysis

### Requirements Overview

**Functional Requirements:**
55 FRs across 8 capability areas defining a CLI tool that:
- Tracks and manages GitHub repositories by category
- Discovers trending repos via topic-based search
- Collects GitHub metrics (stars, forks, PRs, issues, contributors)
- Computes growth scores using configurable weighted formula
- Persists state for week-over-week delta analysis
- Exports all metrics via OTLP HTTP to any OTLP-compatible backend

**Non-Functional Requirements:**
14 NFRs establishing:
- **Integration:** GitHub REST API v3, OTLP/HTTP 1.0.0 specification compliance
- **Security:** Environment-only credentials, no credential logging, HTTPS required
- **Reliability:** Graceful degradation, atomic state writes, idempotent operations, retry logic

**Scale & Complexity:**

- Primary domain: CLI / Backend API Integration
- Complexity level: Medium
- Estimated architectural components: 6-8 modules

### Technical Constraints & Dependencies

| Constraint | Impact |
|------------|--------|
| GitHub API rate limit (5,000/hr) | Requires request tracking, conditional requests, configurable caps |
| No UI | All visualization delegated to OTLP-compatible backend |
| No database | JSON state file must be atomic and corruption-resistant |
| No alerting logic | Metrics-only output; alerting handled by backend |
| TypeScript + Node.js | Runtime and ecosystem decisions constrained |
| OTLP-compatible backend | Vendor-agnostic export via standard OTLP/HTTP protocol |

**External Dependencies:**
- @octokit/rest — GitHub API client
- @opentelemetry/sdk-metrics — OTel metrics SDK
- YAML parser — Configuration loading
- Environment variables — Credential injection

**Target Backend:**
- Any OTLP/HTTP 1.0.0 compatible endpoint
- Primary example: Dynatrace OTLP HTTP API ingest
- Configurable endpoint URL and authentication headers

### Cross-Cutting Concerns Identified

| Concern | Affected Components | Architectural Implication |
|---------|---------------------|---------------------------|
| Rate Limiting | Data Collection, Topic Discovery | Centralized rate tracker, request budgeting |
| Error Resilience | All GitHub operations | Skip-on-failure pattern, partial success handling |
| State Persistence | Collection, Discovery, Growth | Atomic file writes, state schema versioning |
| Metrics Instrumentation | All modules | Consistent metric naming, resource attributes |
| Configuration | All modules | Single config loader, validation on startup |
| Logging | All modules | Structured logging with consistent levels |
| Backend Agnosticism | Metrics Export | Standard OTLP/HTTP only, no vendor-specific APIs |

## Starter Template Evaluation

### Primary Technology Domain

**CLI Tool** — TypeScript/Node.js command-line application for scheduled execution

### Starter Options Considered

| Option | Pros | Cons | Verdict |
|--------|------|------|---------|
| **oclif** | Plugin architecture, auto-generated help, Salesforce-backed | Heavy for 6 commands, opinionated structure | Too heavyweight |
| **Commander.js** | Lightweight, flexible, good TS support | Less scaffolding out of box | Best fit |
| **Optique** | Modern type-safe approach | Newer, smaller ecosystem | Too early to adopt |
| **Custom (tsx + tsup)** | Full control, minimal dependencies | More setup work | Viable alternative |

### Selected Approach: Commander.js with Custom TypeScript Setup

**Rationale:**
- 6 commands don't justify oclif's plugin architecture
- Commander.js is mature, well-documented, TypeScript-friendly
- Custom setup allows precise control over dependencies (@octokit, @opentelemetry)
- tsup for fast ESM builds, tsx for development

**Initialization:**

```bash
mkdir github-radar && cd github-radar
npm init -y
npm install typescript @types/node tsx tsup commander yaml --save-dev
npm install @octokit/rest @opentelemetry/sdk-metrics @opentelemetry/exporter-metrics-otlp-http
```

### Architectural Decisions Provided

**Language & Runtime:**
- TypeScript 5.x with strict mode enabled
- Node.js 20+ (LTS)
- ESM modules (type: "module" in package.json)

**Build Tooling:**
- tsup for production builds (fast, zero-config bundler)
- tsx for development execution (TypeScript execution without compile step)

**Project Structure:**
```
src/
├── cli.ts              # Entry point, command registration
├── commands/           # Command implementations
│   ├── collect.ts
│   ├── discover.ts
│   ├── add.ts
│   ├── remove.ts
│   ├── list.ts
│   └── config.ts
├── services/           # Business logic
│   ├── github.ts       # GitHub API client wrapper
│   ├── scoring.ts      # Growth score calculation
│   ├── state.ts        # JSON state management
│   └── metrics.ts      # OTel metrics export
├── config/             # Configuration loading
│   └── loader.ts
└── types/              # TypeScript interfaces
    └── index.ts
```

**Testing Framework:**
- Vitest (fast, ESM-native, TypeScript-first)

**Code Quality:**
- ESLint with @typescript-eslint
- Prettier for formatting

**Note:** Project initialization is the first implementation story.

## Core Architectural Decisions

### Decision Priority Analysis

**Critical Decisions (Block Implementation):**
- Language: Go (replaces TypeScript from PRD)
- Architecture: Client/daemon separation
- CLI ↔ Daemon communication pattern

**Important Decisions (Shape Architecture):**
- State persistence strategy
- Rate limiting implementation
- Logging framework
- Configuration validation

**Deferred Decisions (Post-MVP):**
- Horizontal scaling (single daemon sufficient for MVP)
- Multi-config profiles

### Language Decision

| Aspect | Decision |
|--------|----------|
| **Language** | Go |
| **Rationale** | Smaller Docker images (~15MB), lower memory for long scans, trivial cross-compilation, better daemon support |
| **Replaces** | TypeScript (originally in PRD) |
| **Go Version** | 1.22+ (latest stable) |

### System Architecture

**Pattern: Client/Daemon Separation**

```
┌─────────────────┐         ┌─────────────────────────────────┐
│   CLI (client)  │ ──────► │   Scanner (background daemon)   │
│                 │         │                                 │
│ • add/remove    │  IPC    │ • Long-running process          │
│ • config        │ ◄────── │ • Scheduled scanning            │
│ • status        │         │ • OTLP metrics export           │
│ • list          │         │ • Reads shared config           │
│                 │         │                                 │
│ Non-blocking    │         │ Runs as: Docker / systemd       │
└─────────────────┘         └─────────────────────────────────┘
```

**Rationale:** CLI must not block while scanning. User configures via CLI, daemon runs independently.

### CLI ↔ Daemon Communication

| Mechanism | Purpose |
|-----------|---------|
| **Shared config file** | CLI writes YAML, daemon reads on startup + SIGHUP |
| **Shared state file** | Daemon writes JSON, CLI reads for `list` command |
| **HTTP status endpoint** | Daemon exposes `/health`, `/status` for CLI queries |

**Rationale:** Simple, decoupled, no complex IPC. Docker/systemd manages daemon lifecycle.

### Updated Command Structure

| Command | Blocking | Action |
|---------|----------|--------|
| `github-radar add <repo>` | No | Updates config file |
| `github-radar remove <repo>` | No | Updates config file |
| `github-radar list` | No | Reads state file, prints repos + scores |
| `github-radar config show` | No | Prints current config |
| `github-radar config validate` | No | Validates config syntax |
| `github-radar status` | No | Queries daemon `/status` endpoint |
| `github-radar serve` | Yes | Starts daemon (foreground, Docker daemonizes) |

### Data Architecture

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **State persistence** | Write-rename pattern | Atomic writes, corruption-resistant (NFR12) |
| **State format** | JSON | Human-readable, easy debugging |
| **Config format** | YAML | User-friendly, supports env var substitution |
| **Config reload** | SIGHUP + file watch | Daemon picks up changes without restart |

### API & Communication

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **GitHub client** | go-github/v60 | Official, well-maintained |
| **Rate limiting** | go-github built-in + configurable cap | Handles 429s, respects X-RateLimit headers |
| **OTLP export** | go.opentelemetry.io/otel | Official Go SDK |
| **HTTP framework** | net/http (stdlib) | Minimal status API, no framework needed |

### Logging & Observability

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **Logging** | slog (stdlib) | Built into Go 1.21+, structured JSON, zero dependencies |
| **Log levels** | DEBUG, INFO, WARN, ERROR | Per FR48-51 |
| **Self-metrics** | OTel metrics | Scan duration, errors, repos processed |

### Configuration Validation

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **Validation** | Go struct tags + custom validation | Type-safe, clear error messages |
| **Env var substitution** | os.ExpandEnv or envsubst pattern | ${GITHUB_TOKEN} syntax |

### Infrastructure & Deployment

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **Distribution** | Docker image (primary) | Cross-platform, background execution via `-d` |
| **Base image** | distroless/static or scratch | Minimal attack surface, ~15MB |
| **Native binaries** | Optional GitHub releases | `GOOS/GOARCH` cross-compilation |
| **Daemon lifecycle** | Docker / systemd / k8s | Not CLI responsibility |

### Updated Project Structure

```
github-radar/
├── cmd/
│   ├── cli/           # CLI entrypoint
│   │   └── main.go
│   └── serve/         # Daemon entrypoint (or unified with cli)
│       └── main.go
├── internal/
│   ├── cli/           # CLI command implementations
│   │   ├── add.go
│   │   ├── remove.go
│   │   ├── list.go
│   │   ├── config.go
│   │   └── status.go
│   ├── daemon/        # Scanner daemon
│   │   ├── server.go  # HTTP status endpoints
│   │   ├── scanner.go # Main scan loop
│   │   └── scheduler.go
│   ├── github/        # GitHub API client wrapper
│   │   └── client.go
│   ├── scoring/       # Growth score calculation
│   │   └── score.go
│   ├── state/         # JSON state management
│   │   └── store.go
│   ├── metrics/       # OTel metrics export
│   │   └── exporter.go
│   └── config/        # Config loading + validation
│       └── loader.go
├── pkg/
│   └── types/         # Shared types (config, state schemas)
│       └── types.go
├── Dockerfile
├── go.mod
└── go.sum
```

### Decision Impact Analysis

**Implementation Sequence:**
1. Project scaffolding (go mod init, directory structure)
2. Config loading + validation
3. State management (read/write JSON)
4. GitHub client wrapper
5. Growth scoring algorithm
6. CLI commands (add, remove, list, config)
7. Daemon scanner loop
8. OTel metrics export
9. HTTP status endpoint
10. Docker packaging

**Cross-Component Dependencies:**
- Config is loaded by both CLI and daemon
- State is written by daemon, read by CLI
- GitHub client used by daemon only
- Metrics exporter used by daemon only

## Implementation Patterns & Consistency Rules

### Critical Conflict Points

6 areas where AI agents must follow consistent patterns to ensure compatible code.

### Go Code Naming Conventions

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

| File | Format | Key Convention | Example |
|------|--------|----------------|---------|
| Config | YAML | snake_case | `rate_limit: 4000` |
| State | JSON | snake_case | `"last_scan_time": "2026-02-14T06:00:00Z"` |
| Env vars | SCREAMING_SNAKE_CASE | `GITHUB_TOKEN`, `OTEL_ENDPOINT` |
| Timestamps | ISO 8601 / RFC 3339 | `2026-02-14T06:00:00Z` |

### Error Handling Patterns

**Rule 1: Return errors, never panic for recoverable situations**
```go
func (c *Client) GetRepo(owner, name string) (*Repo, error) {
    repo, _, err := c.github.Repositories.Get(ctx, owner, name)
    if err != nil {
        return nil, fmt.Errorf("get repo %s/%s: %w", owner, name, err)
    }
    return repo, nil
}
```

**Rule 2: Wrap errors with context using `fmt.Errorf` and `%w`**

**Rule 3: Skip failed items, don't fail entire operation (per NFR10)**
```go
for _, repo := range repos {
    data, err := client.Collect(repo)
    if err != nil {
        slog.Warn("skipping repo", "repo", repo, "error", err)
        continue
    }
    results = append(results, data)
}
```

**Rule 4: Use structured errors for specific handling**
```go
var ErrRateLimited = errors.New("rate limit exceeded")
var ErrNotFound = errors.New("repository not found")
```

### Logging Conventions (slog)

**Log Levels:**
| Level | Usage | Example |
|-------|-------|---------|
| `DEBUG` | API calls, detailed values | `slog.Debug("fetched repo", "repo", name, "stars", count)` |
| `INFO` | Progress, summaries | `slog.Info("scan complete", "repos_scanned", 47)` |
| `WARN` | Recoverable issues, approaching limits | `slog.Warn("rate limit low", "remaining", 100)` |
| `ERROR` | Fatal issues requiring attention | `slog.Error("config invalid", "error", err)` |

**Attribute Naming:** Always snake_case
- Good: `repo_name`, `star_count`, `scan_duration_ms`
- Bad: `repoName`, `StarCount`, `scan-duration`

**Required Attributes per Context:**
- Repo operations: `repo_owner`, `repo_name`
- Scan operations: `scan_id`, `repos_total`
- Config operations: `config_path`

### OTel Metric Conventions

**Naming:** Lowercase with dots, snake_case within segments
- Namespace: `github.repo.*` for repository metrics
- Namespace: `github_radar.*` for operational metrics

**Standard Metrics:**
| Metric | Name | Type | Unit |
|--------|------|------|------|
| Stars | `github.repo.stars` | Gauge | `{stars}` |
| Forks | `github.repo.forks` | Gauge | `{forks}` |
| Star velocity | `github.repo.star_velocity` | Gauge | `{stars}/d` |
| Growth score | `github.repo.growth_score` | Gauge | `1` |
| Scan duration | `github_radar.scan.duration` | Histogram | `s` |
| Repos scanned | `github_radar.scan.repos` | Counter | `{repos}` |
| Scan errors | `github_radar.scan.errors` | Counter | `{errors}` |

**Attribute Keys:** snake_case
- `repo_owner`, `repo_name`, `repo_full_name`
- `category`, `language`, `topic`

### HTTP API Conventions

**Status Endpoint Response Format:**
```json
{
  "status": "running|idle|error",
  "last_scan": "2026-02-14T06:00:00Z",
  "repos_tracked": 47,
  "next_scan": "2026-02-21T06:00:00Z",
  "rate_limit_remaining": 4500,
  "error": null
}
```

**Health Endpoint (for probes):**
```json
{"healthy": true}
```

### Context & Cancellation

**Rule: All long-running operations accept `context.Context`**
```go
func (s *Scanner) Run(ctx context.Context) error {
    for _, repo := range s.repos {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
            // continue scanning
        }
    }
}
```

### Enforcement Guidelines

**All AI Agents MUST:**
1. Follow Go naming conventions exactly as specified
2. Use snake_case for all JSON/YAML keys and log attributes
3. Return errors with context wrapping, never panic
4. Use slog with specified levels and attribute names
5. Name OTel metrics per the conventions above
6. Accept context.Context for cancellable operations

**Pattern Verification:**
- `go vet` and `staticcheck` in CI
- Review metric names against conventions
- Log output spot-checks for attribute naming

## Project Structure & Boundaries

### Complete Project Directory Structure

```
github-radar/
├── README.md
├── LICENSE
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile
├── .gitignore
├── .github/
│   └── workflows/
│       ├── ci.yml              # Build, test, lint
│       └── release.yml         # Cross-compile + Docker push
│
├── cmd/
│   └── github-radar/
│       └── main.go             # Single binary entry point
│
├── internal/
│   ├── cli/                    # CLI command implementations
│   │   ├── root.go             # Root command, flag parsing
│   │   ├── add.go              # `add <repo>` command
│   │   ├── remove.go           # `remove <repo>` command
│   │   ├── list.go             # `list` command
│   │   ├── config.go           # `config show/validate` commands
│   │   ├── status.go           # `status` command (queries daemon)
│   │   └── serve.go            # `serve` command (starts daemon)
│   │
│   ├── daemon/                 # Background scanner daemon
│   │   ├── daemon.go           # Daemon lifecycle management
│   │   ├── scanner.go          # Main scan loop orchestration
│   │   ├── discovery.go        # Topic-based repo discovery
│   │   ├── collector.go        # Data collection coordinator
│   │   ├── scheduler.go        # Scan scheduling logic
│   │   └── server.go           # HTTP status endpoints (/health, /status)
│   │
│   ├── github/                 # GitHub API client wrapper
│   │   ├── client.go           # Client initialization, rate limiting
│   │   ├── repos.go            # Repository data fetching
│   │   ├── search.go           # Search API for discovery
│   │   └── types.go            # GitHub response types
│   │
│   ├── scoring/                # Growth score calculation
│   │   ├── score.go            # Score computation logic
│   │   ├── velocity.go         # Velocity calculations
│   │   ├── weights.go          # Configurable weight handling
│   │   └── normalize.go        # Score normalization (0-100)
│   │
│   ├── state/                  # JSON state persistence
│   │   ├── store.go            # State read/write operations
│   │   ├── types.go            # State schema definitions
│   │   └── migrate.go          # State schema migrations
│   │
│   ├── metrics/                # OpenTelemetry metrics export
│   │   ├── exporter.go         # OTLP HTTP exporter setup
│   │   ├── repo_metrics.go     # Repository metric recording
│   │   ├── scan_metrics.go     # Operational metric recording
│   │   └── attributes.go       # Common attribute builders
│   │
│   └── config/                 # Configuration management
│       ├── loader.go           # YAML loading + env substitution
│       ├── types.go            # Config struct definitions
│       ├── validate.go         # Config validation rules
│       └── defaults.go         # Default values
│
├── pkg/
│   └── types/                  # Shared public types
│       ├── repo.go             # Repository types
│       ├── category.go         # Category types
│       └── score.go            # Score types
│
├── configs/
│   └── github-radar.example.yaml   # Example configuration
│
├── scripts/
│   ├── install.sh              # Installation script
│   └── uninstall.sh            # Uninstallation script
│
└── tests/
    ├── integration/            # Integration tests
    │   ├── github_test.go      # GitHub API integration tests
    │   └── metrics_test.go     # OTel export integration tests
    └── fixtures/               # Test data
        ├── config.yaml         # Test configuration
        └── state.json          # Test state file
```

### Architectural Boundaries

**CLI ↔ Daemon Boundary:**
```
CLI Commands                    Daemon
─────────────                   ──────
add/remove/config  ──writes──►  config.yaml
list               ◄──reads───  state.json
status             ◄──HTTP────  :8080/status
serve              ──starts──►  daemon process
```

**Internal Module Boundaries:**

| Module | Depends On | Provides |
|--------|------------|----------|
| `cli` | `config`, `state` | User commands |
| `daemon` | `github`, `scoring`, `state`, `metrics`, `config` | Background scanning |
| `github` | (external: go-github) | GitHub API access |
| `scoring` | `state` (for previous values) | Growth scores |
| `state` | (filesystem) | Persistence |
| `metrics` | (external: otel-go) | OTLP export |
| `config` | (filesystem, env) | Configuration |

**Data Flow:**
```
┌─────────┐    ┌─────────┐    ┌─────────┐    ┌─────────┐
│ GitHub  │───►│ Daemon  │───►│ Scoring │───►│ Metrics │───► OTLP Backend
│  API    │    │(scanner)│    │         │    │         │
└─────────┘    └────┬────┘    └─────────┘    └─────────┘
                    │
                    ▼
              ┌─────────┐
              │  State  │
              │ (JSON)  │
              └─────────┘
```

### Requirements to Structure Mapping

**Repository Management (FR1-FR6):**
- `internal/cli/add.go` — FR2: Add repos to category
- `internal/cli/remove.go` — FR3: Remove repos from tracking
- `internal/cli/list.go` — FR5: View tracked repos with scores
- `internal/config/types.go` — FR1, FR4: Category and exclusion definitions

**Topic Discovery (FR7-FR12):**
- `internal/daemon/discovery.go` — FR7-FR10: Search API, filtering, auto-track
- `internal/config/types.go` — FR11-FR12: Discovery configuration

**Data Collection (FR13-FR23):**
- `internal/github/repos.go` — FR13-FR21: All repo metrics collection
- `internal/github/client.go` — FR22-FR23: Rate limiting, conditional requests

**Growth Analysis (FR24-FR30):**
- `internal/scoring/velocity.go` — FR24-FR28: Velocity calculations
- `internal/scoring/score.go` — FR29-FR30: Composite score, normalization

**State Management (FR31-FR35):**
- `internal/state/store.go` — FR31-FR32: Persist and load state
- `internal/state/types.go` — FR33-FR34: State schema
- `internal/cli/root.go` — FR35: State file path flag

**Metrics Export (FR36-FR41):**
- `internal/metrics/exporter.go` — FR37-FR40: OTLP setup, flush
- `internal/metrics/repo_metrics.go` — FR36, FR41: Repo metrics with dimensions
- `internal/metrics/attributes.go` — FR38: Resource attributes

**Configuration (FR42-FR47):**
- `internal/config/loader.go` — FR42, FR46: Env substitution, path handling
- `internal/config/types.go` — FR43-FR45: All config fields
- `internal/config/validate.go` — FR47: Startup validation

**Observability (FR48-FR55):**
- Throughout with `slog` — FR48-FR51: Log levels
- `internal/daemon/scanner.go` — FR52-FR53: Skip-on-error, 404 warnings
- `internal/cli/root.go` — FR54: Exit codes
- `internal/cli/serve.go` — FR55: Dry-run flag

### Integration Points

**External Integrations:**

| Integration | Package | Boundary |
|-------------|---------|----------|
| GitHub REST API | `internal/github/` | `client.go` initializes authenticated client |
| OTLP HTTP | `internal/metrics/` | `exporter.go` configures endpoint + headers |
| Filesystem | `internal/state/`, `internal/config/` | Atomic writes, path validation |

**Internal Communication:**

| From | To | Mechanism |
|------|-----|-----------|
| CLI | Config | Direct function call (same process) |
| CLI | State | Direct file read (state.json) |
| CLI | Daemon | HTTP GET to localhost:8080 |
| Daemon | GitHub | go-github client |
| Daemon | State | Atomic file writes |
| Daemon | Metrics | OTel SDK |

### File Organization Patterns

**Configuration Files:**
- `configs/github-radar.example.yaml` — Checked into repo, documented
- `~/.config/github-radar/config.yaml` — Default user location
- `./github-radar.yaml` — Current directory override

**Test Organization:**
- Unit tests: `*_test.go` co-located with source
- Integration tests: `tests/integration/` (require external services)
- Fixtures: `tests/fixtures/` (static test data)

**Build Artifacts:**
- Binary: `./github-radar` (or `./dist/github-radar-{os}-{arch}`)
- Docker: `ghcr.io/{owner}/github-radar:latest`

### Development Workflow

**Local Development:**
```bash
go run ./cmd/github-radar serve --config ./configs/github-radar.example.yaml
```

**Build:**
```bash
make build        # Local binary
make docker       # Docker image
make release      # Cross-compile all platforms
```

**Test:**
```bash
go test ./...                    # Unit tests
go test ./tests/integration/...  # Integration tests (needs GITHUB_TOKEN)
```

## Architecture Validation Results

### Coherence Validation ✅

**Decision Compatibility:**
All technology choices are compatible:
- Go 1.22+ with go-github/v60 and go.opentelemetry.io/otel
- slog (stdlib) requires no external dependencies
- Client/daemon architecture with shared config/state files

**Pattern Consistency:**
- Go naming conventions (PascalCase exports, camelCase private) applied consistently
- snake_case for all external formats (JSON, YAML, metrics, logs)
- Error wrapping with context throughout

**Structure Alignment:**
- Standard Go project layout (cmd/, internal/, pkg/)
- Clear module boundaries with explicit dependencies
- Integration points well-defined

### Requirements Coverage Validation ✅

**Functional Requirements Coverage:**
All 55 FRs mapped to specific files in project structure.
See "Requirements to Structure Mapping" section for details.

**Non-Functional Requirements Coverage:**

| NFR | Status | Implementation |
|-----|--------|----------------|
| NFR1-NFR5 | ✅ | Integration decisions cover all |
| NFR6-NFR9 | ✅ | Security patterns address all |
| NFR10-NFR14 | ✅ | Reliability patterns cover all |

**Note:** NFR8 (config permission warning) and NFR14 (retry logic) rely on standard library and go-github built-in behavior; explicit implementation notes added to patterns.

### Implementation Readiness Validation ✅

**Decision Completeness:**
- All technology choices documented with versions
- Build and deployment patterns defined
- Development workflow documented

**Structure Completeness:**
- Complete directory tree with all files
- Every file mapped to specific requirements
- Integration boundaries clearly defined

**Pattern Completeness:**
- Naming conventions cover all code and data formats
- Error handling patterns with examples
- Logging patterns with level guidance
- Metric naming follows OTel conventions

### Gap Analysis Results

| Gap | Priority | Resolution |
|-----|----------|------------|
| PRD specified TypeScript | Important | User confirmed switch to Go; architecture supersedes PRD |
| Config permission check | Minor | Add file mode check in `config/loader.go` |
| Explicit OTLP retry | Minor | Add retry wrapper in `metrics/exporter.go` |

### Architecture Completeness Checklist

**✅ Requirements Analysis**
- [x] Project context thoroughly analyzed
- [x] Scale and complexity assessed (Medium)
- [x] Technical constraints identified (API rate limits, no UI/DB)
- [x] Cross-cutting concerns mapped

**✅ Architectural Decisions**
- [x] Language: Go 1.22+
- [x] Architecture: Client/daemon separation
- [x] Technology stack fully specified
- [x] Integration patterns defined (shared config, HTTP status)
- [x] Performance considerations addressed (rate limiting)

**✅ Implementation Patterns**
- [x] Naming conventions established (Go + snake_case external)
- [x] Structure patterns defined (cmd/internal/pkg)
- [x] Communication patterns specified (shared files + HTTP)
- [x] Process patterns documented (error handling, context)

**✅ Project Structure**
- [x] Complete directory structure defined
- [x] Component boundaries established
- [x] Integration points mapped
- [x] Requirements to structure mapping complete

### Architecture Readiness Assessment

**Overall Status:** ✅ READY FOR IMPLEMENTATION

**Confidence Level:** High

**Key Strengths:**
- Clean client/daemon separation enables non-blocking CLI
- Standard Go project layout familiar to Go developers
- All 55 FRs explicitly mapped to code locations
- Comprehensive patterns prevent AI agent conflicts
- Docker distribution simplifies deployment

**Areas for Future Enhancement:**
- Multi-profile configuration (post-MVP)
- Resume-from-interrupted-scan capability (post-MVP)
- Prometheus metrics endpoint alternative (post-MVP)

### Implementation Handoff

**AI Agent Guidelines:**
1. Follow all architectural decisions exactly as documented
2. Use implementation patterns consistently across all components
3. Respect project structure and module boundaries
4. Refer to this document for all architectural questions
5. Note: Architecture uses Go, superseding PRD's TypeScript specification

**First Implementation Steps:**
1. Initialize Go module: `go mod init github.com/{owner}/github-radar`
2. Create directory structure per "Project Structure" section
3. Implement `internal/config/` first (foundation for all components)
4. Implement `internal/state/` second (needed by CLI and daemon)
5. Build CLI commands before daemon (easier to test)

