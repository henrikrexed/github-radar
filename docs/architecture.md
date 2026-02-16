# Architecture Overview

## System Design

GitHub Radar follows a pipeline architecture:

```
GitHub API → Collector → Scorer → Exporter → OTel Backend
                ↕                     ↕
            State Store          OTLP HTTP
```

### Components

| Component | Package | Responsibility |
|-----------|---------|----------------|
| **CLI** | `internal/cli` | Command parsing, user interaction, output formatting |
| **Config** | `internal/config` | YAML loading, env var substitution, validation |
| **GitHub Client** | `internal/github` | HTTP client with auth, rate limiting, conditional requests |
| **Scanner** | `internal/github` | Orchestrates collection across all tracked repos |
| **Collector** | `internal/github` | Gathers metrics for a single repository (stars, PRs, issues, etc.) |
| **Discovery** | `internal/discovery` | Queries GitHub Search API, filters results, auto-tracks |
| **Scoring** | `internal/scoring` | Growth velocity/acceleration calculation, composite scoring |
| **State Store** | `internal/state` | JSON persistence with atomic writes, thread-safe access |
| **Metrics** | `internal/metrics` | OTel SDK setup, metric recording, OTLP export |
| **Daemon** | `internal/daemon` | Scheduling, HTTP endpoints, signal handling, config reload |
| **Repository** | `internal/repository` | Repository management (add, remove, list, exclude) |

## Data Flow

### Collection Cycle

1. **Load Config** — Parse YAML, expand env vars, validate
2. **Load State** — Read previous state from JSON file (if exists)
3. **Discovery** (if enabled) — Query GitHub Search API for trending repos
4. **Collection** — For each tracked repository:
     - Fetch repo metadata (stars, forks, language, topics)
     - Fetch activity data (PRs, issues, contributors, releases)
     - Use conditional requests (ETag/If-Modified-Since) to save API calls
5. **Scoring** — Calculate velocities and composite growth score
6. **Export** — Record all metrics via OTel SDK, flush to OTLP endpoint
7. **Save State** — Atomic write of updated state to JSON file

### API Calls per Repository

Each repository requires approximately 6-8 API calls:

| Call | Endpoint | Purpose |
|------|----------|---------|
| 1 | `GET /repos/{owner}/{repo}` | Stars, forks, issues, language, topics |
| 2 | `GET /repos/{owner}/{repo}/pulls?state=closed` | Merged PRs (7 days) |
| 3 | `GET /repos/{owner}/{repo}/issues?state=open` | Recent issues (7 days) |
| 4 | `GET /repos/{owner}/{repo}/contributors` | Contributor count |
| 5 | `GET /repos/{owner}/{repo}/releases/latest` | Latest release |
| 6 | `GET /search/repositories?q=...` | Discovery (shared across topics) |

## Technology Choices

| Area | Choice | Rationale |
|------|--------|-----------|
| Language | Go 1.22+ | Static binary, good concurrency, stdlib HTTP/JSON |
| CLI Framework | Go stdlib (`flag`) | No dependencies, sufficient for this scope |
| GitHub API | Custom HTTP client | Lightweight, full control over rate limiting |
| Metrics | OpenTelemetry Go SDK | Industry standard, supports any backend |
| Config | YAML with env expansion | Human-readable, familiar, supports secrets |
| State | JSON file | Simple, no database dependency, portable |
| Logging | `slog` (Go stdlib) | Structured logging, zero dependencies |
| Build | Makefile + Go toolchain | Standard, cross-platform compilation |
| Container | Multi-stage Docker | Minimal image (~15MB), non-root user |

## State Management

State is persisted as a JSON file using atomic write-rename:

1. Write to temporary file (`state.json.tmp`)
2. Rename to target path (`state.json`)

This prevents corruption if the process crashes mid-write.

The state store is thread-safe (uses `sync.RWMutex`) for concurrent access by the daemon's scan loop and HTTP status endpoint.

### State Schema

```json
{
  "repos": {
    "kubernetes/kubernetes": {
      "owner": "kubernetes",
      "name": "kubernetes",
      "stars": 108000,
      "stars_prev": 107500,
      "forks": 38000,
      "contributors": 3700,
      "star_velocity": 71.4,
      "star_acceleration": 2.1,
      "growth_score": 285.6,
      "etag": "\"abc123\"",
      "last_collected": "2026-02-16T06:00:00Z"
    }
  }
}
```

## Rate Limiting

GitHub API allows 5,000 requests/hour for authenticated users. GitHub Radar:

- Tracks remaining requests via `X-RateLimit-Remaining` headers
- Configurable rate cap (default: 4000, leaving buffer)
- Uses conditional requests (ETag) to reduce calls for unchanged repos
- Skips individual repos on error rather than failing the entire scan

## Daemon Architecture

The daemon runs as a single process with:

- **Scan loop** — Ticker-based scheduling with overlap prevention (`sync.Mutex.TryLock`)
- **HTTP server** — Lightweight health/status endpoints on configurable port
- **Signal handler** — `SIGTERM`/`SIGINT` for graceful shutdown, `SIGHUP` for config reload
- **Context propagation** — All operations use `context.Context` for cancellation

## Project Layout

```
github-radar/
├── cmd/github-radar/          # Entry point (main.go)
├── internal/                   # Private packages
│   ├── cli/                   # Command implementations
│   │   ├── add.go
│   │   ├── collect.go
│   │   ├── config_cmd.go
│   │   ├── discover.go
│   │   ├── exclude.go
│   │   ├── list.go
│   │   ├── remove.go
│   │   ├── root.go
│   │   ├── serve.go
│   │   └── status.go
│   ├── config/                # Configuration
│   ├── daemon/                # Background daemon
│   ├── discovery/             # Topic-based discovery
│   ├── github/                # GitHub API client
│   ├── logging/               # Structured logging
│   ├── metrics/               # OTel metrics
│   ├── repository/            # Repo management
│   ├── scoring/               # Growth scoring
│   └── state/                 # State persistence
├── configs/                   # Example configs
├── docs/                      # MkDocs documentation
├── Dockerfile
├── docker-compose.yml
├── Makefile
└── mkdocs.yml
```
