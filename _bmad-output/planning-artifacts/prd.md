---
stepsCompleted: ['step-01-init', 'step-02-discovery', 'step-03-success', 'step-04-journeys', 'step-05-domain', 'step-06-innovation', 'step-07-project-type', 'step-08-scoping', 'step-09-functional', 'step-10-nonfunctional', 'step-11-polish', 'step-12-complete', 'step-e-01-discovery', 'step-e-02-review', 'step-e-03-edit']
workflowStatus: complete
completedAt: 2026-02-14
lastEdited: 2026-02-16
editHistory:
  - date: 2026-02-16
    changes: "Added Documentation requirements (FR56-FR61): README with token guide, getting started, command examples, daemon setup; MkDocs site. Added CI/CD Pipeline requirements (FR62-FR67): GitHub Actions for build, test, quality checks, security scans, release artifacts. Added NFR21-NFR22. Updated MVP scope to R1-R11."
  - date: 2026-02-15
    changes: "Zero-config trend discovery (removed topic filtering from MVP); Quality Assurance integrated throughout (unit tests, integration tests, edge cases); Categorization moved to post-MVP with database/Redis storage note"
inputDocuments:
  - type: product-brief
    path: product-brief-github-trend-scanner-2026-02-14.md
    description: Initial product brief for GitHub Trend Scanner
  - type: user-provided-context
    source: conversation
    description: Comprehensive project requirements including problem statement, target users, MVP scope, tech stack, OTel metrics specification, and success criteria
workflowType: 'prd'
documentCounts:
  briefs: 1
  research: 0
  brainstorming: 0
  projectDocs: 0
projectType: greenfield
date: 2026-02-14
classification:
  projectType: cli_tool
  domain: devtools/observability
  complexity: medium
  projectContext: greenfield
  keyConstraints:
    - GitHub API rate limits (5,000 requests/hour authenticated)
    - Scan duration for large repo sets
    - Need for prioritized/incremental scanning
---

# Product Requirements Document - GitHub Trend Scanner

**Author:** Henrik.rexed
**Date:** 2026-02-14

## Executive Summary

**Product:** GitHub Radar — Open Source Growth Intelligence

**Vision:** Detect trending GitHub projects before they go viral, giving content creators a first-mover advantage.

**Target Users:**
- Primary: YouTube content creators (cloud-native, AI, observability)
- Secondary: DevRel teams, OSS investors, tech journalists

**Differentiator:** No UI, no alerting logic, no database. Export metrics to observability platforms (Dynatrace) and leverage their existing anomaly detection and dashboarding capabilities.

**Tech Stack:** TypeScript CLI, @octokit/rest, @opentelemetry/sdk-metrics, YAML config, JSON state

**Key Constraint:** GitHub API rate limits (5,000 requests/hour authenticated)

## Success Criteria

### User Success

- **Discovery of unknown projects**: User finds GitHub projects they've never heard of before they trend on social media
- **Justifiable investment**: Metrics provide concrete figures (stars, growth rate, contributor velocity) to justify time investment in content creation
- **Actionable signal volume**: 1-2 high-quality data points per week - enough to inform content decisions without noise overwhelm
- **Workflow integration**: Alerts feed directly into content backlog; growth rate enables prioritization; metrics support deep-dive research for technical angle

### Business Success

- **3-month milestone**: 2-3 published videos on projects discovered via GitHub Radar before other creators covered them
- **Content edge**: Consistent first-mover advantage on emerging cloud-native/AI projects
- **Secondary users (future)**: DevRel teams, investors, journalists benefit from same intelligence (validates broader market)

### Technical Success

- **Coverage**: Scan as many repositories as possible within GitHub API rate limits
- **Discovery**: Automatic trend-based discovery without manual topic configuration
- **Accuracy bias**: Minimize false negatives (missing trending projects) over false positives
- **Noise control**: Blacklist capability to exclude repos from future scans
- **Output quality**: Metrics correctness and completeness matter; execution time does not

### Quality Assurance

- **Unit test coverage**: ≥80% coverage for core modules (collection, scoring, export)
- **Integration tests**: Validate metrics accuracy using known GitHub repositories as fixtures
- **Edge case coverage**: Rate limiting scenarios tested (backoff, partial results, resume)
- **Test automation**: Test suite executes in CI pipeline on every pull request

### Measurable Outcomes

| Metric | Target |
|--------|--------|
| Projects discovered before social trend | 2-3 per quarter |
| Actionable alerts per week | 1-2 |
| False negative rate | Minimize (no hard target for MVP) |
| Scan coverage | Maximum repos within rate limits |

## Product Scope

### MVP - Minimum Viable Product

1. **GitHub Data Collector** - Stars, forks, PRs, issues, contributors via Octokit
2. **Growth Scoring Algorithm** - Velocity (change per period) + acceleration (rate of change)
3. **Trend-Based Discovery** - Automatically detect trending/growing repositories without manual topic configuration
4. **OpenTelemetry Metrics Exporter** - OTLP HTTP to OTel Collector → Dynatrace
5. **CLI Runner** - Config-file driven, cron-schedulable
6. **JSON State File** - Week-over-week delta tracking (no database)
7. **Structured Logging** - Debug mode for scan details, ERROR for issues
8. **Quality Assurance** - Unit tests (≥80% coverage), integration tests with real repo fixtures, edge case coverage
9. **Documentation** - Comprehensive README (token setup, getting started, command examples, daemon guide) + MkDocs documentation site
10. **CI/CD Pipeline** - GitHub Actions for build, test, quality checks, security scans, and release artifact creation

### Growth Features (Post-MVP)

- **Category Classification** - LLM-based repo categorization (one-time per repo, cached)
- **Storage Layer** - Database or Redis cache for classification persistence at scale
- **Topic Filtering** - Filter repos by category (requires classification feature)
- Blacklist management for repos to skip
- Incremental scanning (prioritized rotation through large repo lists)
- Rate-limit resilience (backoff, resume capability)
- Multiple config profiles (different topic sets)

### Vision (Future)

- Trend prediction (ML-based growth forecasting)
- Multi-source signals (social media, Hacker News, Reddit)
- Content workflow integration (direct backlog creation)
- Competitor tracking (what projects are other creators covering)
- Historical trend analysis and reporting

## User Journeys

### Journey 1: Content Discovery (Primary User - Happy Path)

**Persona:** Henrik, YouTube creator running IsItObservable (cloud-native, observability, AI content)

**Opening Scene:**
It's Monday morning. A Slack notification pops up from Dynatrace: "GitHub Radar Alert: 3 projects with unusual growth detected this week."

**Rising Action:**
Henrik clicks through to see the details:
- **Project A**: 2K → 8K stars (+300%), 12 contributors, topic: "kubernetes"
- **Project B**: 500 → 3K stars (+500%), 1 contributor, topic: "llm"
- **Project C**: 1K → 4K stars (+300%), 8 contributors, topic: "observability"

He filters mentally:
- Project A: Kubernetes + healthy contributor base → **Add to backlog (high priority)**
- Project B: Massive growth but single contributor → **Skip for now, might be hype**
- Project C: Observability + solid team → **Add to backlog, investigate further**

**Climax:**
Henrik opens Project A on GitHub, does a 15-minute deep dive: README, architecture, use cases. It's a new service mesh observability tool. Perfect for his audience. He adds "Deep dive on [Project A]" to his content calendar for next week.

**Resolution:**
Two weeks later, his video goes live. The project hits 20K stars the following month. His video is the first English-language technical deep dive. Comments flood in: "How did you find this so early?"

---

### Journey 2: False Positive Handling (Primary User - Edge Case)

**Opening Scene:**
Henrik gets a Slack alert about a project with 400% growth. He clicks through to GitHub, excited.

**Rising Action:**
After 5 minutes of investigation, he realizes it's a cryptocurrency project that happens to use "cloud" in its description. Not his audience.

**Climax:**
He opens his `config.yaml`, adds the repo to the blacklist:
```yaml
blacklist:
  - org/crypto-cloud-thing
```

**Resolution:**
Next week's scan skips this repo entirely. His signal-to-noise ratio improves. Over time, his blacklist grows, and alerts become increasingly relevant.

---

### Journey 3: Initial Setup (Operator Journey)

**Opening Scene:**
Henrik just installed GitHub Radar. He needs minimal configuration before the first run.

**Rising Action:**
He creates `config.yaml`:
```yaml
github_token: ${GITHUB_TOKEN}
otel_endpoint: http://localhost:4318

blacklist:
  - some-org/known-irrelevant-repo
```

No topic configuration required — the tool automatically discovers trending repositories.

He runs the CLI for the first time:
```bash
github-radar scan --config ./config.yaml
```

**Climax:**
The CLI outputs:
- Discovered 47 trending repos
- Collected metrics for 47 repos
- Exported 564 data points to OTLP endpoint
- State saved to `./state.json`

**Resolution:**
He sets up a cron job to run weekly. Metrics start flowing into Dynatrace. He configures a dashboard and alert rule. Filtering by topic/category is handled in Dynatrace dashboards using repo metadata.

---

### Journey 4: Scan Failure (Failure Scenario)

**Opening Scene:**
It's Monday. Henrik expects his weekly Slack alert, but nothing arrives.

**Rising Action:**
He checks Dynatrace - no new metrics from GitHub Radar since last week. He checks the CLI logs on his server:

```
ERROR: GitHub API rate limit exceeded. Scanned 312/500 repos before failure.
ERROR: Partial state saved. Resume with --resume flag.
```

**Climax:**
He sees the error metric in Dynatrace: `github.radar.scan.errors = 1`. He waits an hour for rate limit reset, then runs:
```bash
github-radar scan --config ./config.yaml --resume
```

**Resolution:**
The scan completes from where it left off. Metrics flow. He considers reducing scan frequency or prioritizing repos.

---

### Journey 5: Secondary Users

| User | Journey Summary |
|------|-----------------|
| **DevRel Team** | Uses same setup, tracks repos in their ecosystem. Gets alerts on competitor project growth. Informs content/community strategy. |
| **OSS Investor** | Tracks emerging projects by category. Growth acceleration signals investment timing. |
| **Tech Journalist** | Monitors for story leads. Sudden growth = potential article. |

*These users have identical journeys to Henrik, just different content decisions at the end.*

---

### Journey Requirements Summary

| Capability | Source Journeys |
|------------|-----------------|
| Trend-based discovery (zero-config) | J1, J3 |
| Growth rate calculation | J1, J2 |
| Contributor metrics | J1 |
| Blacklist management | J2, J3 |
| Config file driven (minimal config) | J3 |
| Structured logging (DEBUG/ERROR) | J3, J4 |
| Self-monitoring metrics | J4 |
| Resume on failure | J4 |
| OTLP export | J1, J3 |
| State persistence | J3, J4 |
| Quality assurance (tests) | All journeys |

## CLI Tool Specific Requirements

### Command Structure

| Command | Purpose | Example |
|---------|---------|---------|
| `github-radar collect` | Run full collection + export | `github-radar collect --config ./config.yaml` |
| `github-radar discover` | Run topic discovery only | `github-radar discover --dry-run` |
| `github-radar add <repo>` | Add repo to tracking | `github-radar add kubernetes/kubernetes` |
| `github-radar remove <repo>` | Remove repo from tracking | `github-radar remove org/repo` |
| `github-radar list` | Show tracked repos with scores | `github-radar list --category ai-agents` |
| `github-radar config` | Show/edit configuration | `github-radar config --show` |

**Global Flags:**
- `--config <path>` — Config file path (default: `./github-radar.yaml`)
- `--state <path>` — State file path (default: `./github-radar-state.json`)
- `--dry-run` — Simulate without exporting metrics
- `--verbose` — Enable debug logging

### Output Formats

| Output Type | Format | Destination |
|-------------|--------|-------------|
| Metrics | OTLP HTTP | OTel Collector or Dynatrace endpoint |
| State | JSON | Local file (configurable path) |
| Logs | Structured text | stdout/stderr |
| List output | Table/JSON | stdout |

**Log Levels:**
- `DEBUG` — Scan details, API calls, metric values
- `INFO` — Summary statistics, progress
- `WARN` — Rate limit approaching, repo 404s
- `ERROR` — Fatal errors, API failures

### Configuration Schema

```yaml
# github-radar.yaml
github:
  token: ${GITHUB_TOKEN}
  rateLimit: 4000

otel:
  endpoint: http://localhost:4318
  headers:
    Authorization: "Api-Token ${DT_API_TOKEN}"
  serviceName: github-radar

discovery:
  enabled: true
  minStars: 100
  maxAgeDays: 90
  autoTrackThreshold: 50
  # topics: [kubernetes, opentelemetry, ebpf, llm]  # Optional (post-MVP)

tracking:
  repos: []  # Optional: manually tracked repos
  # categories:  # Optional (post-MVP, requires classification feature)
  #   ai-agents: [openclaw/openclaw, langchain-ai/langchain]

exclusions:
  - dynatrace/dynatrace-operator

scoring:
  weights:
    starVelocity: 2.0
    starAcceleration: 3.0
    contributorGrowth: 1.5
    prVelocity: 1.0
    issueVelocity: 0.5
```

### Scripting Support

**Cron Integration:**
```bash
# Weekly scan every Monday at 6 AM
0 6 * * 1 /usr/local/bin/github-radar collect --config /etc/github-radar/config.yaml
```

**Exit Codes:**
- `0` — Success
- `1` — Fatal error (config missing, auth failure)
- `0` with warnings — Partial success (some repos failed)

**Environment Variables:**
- `GITHUB_TOKEN` — GitHub API authentication
- `DT_API_TOKEN` — Dynatrace API token (if using direct export)
- `GITHUB_RADAR_CONFIG` — Default config path
- `GITHUB_RADAR_STATE` — Default state path

## Project Scoping & Phased Development

### MVP Strategy & Philosophy

**MVP Approach:** Problem-Solving MVP
- Solve one problem completely: detect trending GitHub projects before they go viral
- No UI (Dynatrace is the UI)
- No alerting logic (Dynatrace handles alerts)
- No database (JSON state file)

**Resource Requirements:** Solo developer, ~2-4 weeks for MVP

### MVP Feature Set (Phase 1)

**Core User Journeys Supported:**
- J1: Content Discovery (happy path)
- J2: False Positive Handling (blacklist)
- J3: Initial Setup (config-driven)
- J4: Scan Failure (error handling)

**Must-Have Capabilities (R1-R11):**
- Repository tracking with categories (R1)
- Topic-based discovery with filters (R2)
- GitHub data collection with rate limiting (R3)
- Growth scoring algorithm (R4)
- JSON state persistence (R5)
- OpenTelemetry metrics export (R6)
- CLI interface with core commands (R7)
- YAML configuration with env vars (R8)
- Graceful error handling (R9)
- Documentation: README + MkDocs site (R10)
- CI/CD: GitHub Actions pipeline (R11)

### Post-MVP Features

**Phase 2 (Growth):**
- `--resume` flag for interrupted scans
- Multiple config profiles
- CLI blacklist management commands
- Automatic rate-limit backoff and retry
- JSON output mode for scripting

**Phase 3 (Expansion):**
- Trend prediction (ML-based growth forecasting)
- Multi-source signals (social media, Hacker News)
- Optional web dashboard (for non-Dynatrace users)
- Historical trend analysis and export

### Risk Mitigation Strategy

**Technical Risks:**
- GitHub API rate limits → Conditional requests (If-Modified-Since), configurable rate cap
- Large repo sets → Skip failed repos, don't fail entire run

**Market Risks:**
- Primary user is the developer → Dogfooding ensures product-market fit

**Resource Risks:**
- Solo developer → No UI, leverage existing observability platform

## Functional Requirements

### Repository Management

- FR1: Operator can define categories of repositories to track in configuration
- FR2: Operator can add specific repositories to a category
- FR3: Operator can remove repositories from tracking via CLI
- FR4: Operator can add repositories to an exclusion list (blacklist)
- FR5: Operator can view all tracked repositories with their current scores
- FR6: System skips excluded repositories during collection runs

### Trend-Based Discovery

- FR7: System can query GitHub Search API for repositories showing unusual growth patterns
- FR8: System can filter discovered repositories by minimum star count
- FR9: System can filter discovered repositories by maximum age (days since creation)
- FR10: System can automatically add discovered repositories to tracking when growth score exceeds threshold
- FR11: System discovers trending repositories without requiring manual topic configuration
- FR12: Operator can enable or disable automatic discovery

### Data Collection

- FR13: System can collect star count for each tracked repository
- FR14: System can collect fork count for each tracked repository
- FR15: System can collect open issues count for each tracked repository
- FR16: System can collect open pull requests count for each tracked repository
- FR17: System can collect PRs merged in the last 7 days for each tracked repository
- FR18: System can collect new issues opened in the last 7 days for each tracked repository
- FR19: System can collect contributor count for each tracked repository
- FR20: System can collect latest release tag and date for each tracked repository
- FR21: System can collect primary language and topics for each tracked repository
- FR22: System respects GitHub API rate limits by tracking request count
- FR23: System uses conditional requests (If-Modified-Since) to minimize API calls

### Growth Analysis

- FR24: System can calculate star velocity (stars gained per day)
- FR25: System can calculate star acceleration (velocity change from previous period)
- FR26: System can calculate PR velocity (PRs merged per day)
- FR27: System can calculate issue velocity (new issues per day)
- FR28: System can calculate contributor growth (new contributors per day)
- FR29: System can compute composite growth score using configurable weights
- FR30: System can normalize growth scores to 0-100 scale

### State Management

- FR31: System can persist collection state to JSON file
- FR32: System can load previous state for week-over-week comparison
- FR33: System tracks per-repository: last star count, last collection timestamp, previous velocities
- FR34: System tracks discovery state: last scan timestamp, known repositories
- FR35: Operator can configure state file location via CLI flag or environment variable

### Metrics Export

- FR36: System can export all collected metrics as OpenTelemetry metrics
- FR37: System can export metrics via OTLP HTTP to configurable endpoint
- FR38: System includes resource attributes (service.name, service.version) in metrics
- FR39: System can include custom headers in OTLP requests (for authentication)
- FR40: System flushes all metrics synchronously before process exit
- FR41: Metrics include repository dimensions (org, name, language, topics, category)

### Configuration

- FR42: Operator can configure GitHub token via environment variable substitution
- FR43: Operator can configure OTLP endpoint and authentication headers
- FR44: Operator can configure scoring weights for growth formula
- FR45: Operator can configure rate limit threshold to stay under GitHub limits
- FR46: Operator can specify configuration file path via CLI flag
- FR47: System validates configuration on startup and reports errors

### Observability

- FR48: System logs scan progress and summary statistics at INFO level
- FR49: System logs detailed API calls and metric values at DEBUG level
- FR50: System logs rate limit warnings at WARN level
- FR51: System logs fatal errors at ERROR level
- FR52: System skips individual repositories on API errors without failing the entire run
- FR53: System logs warnings for repositories that return 404 (deleted/renamed)
- FR54: System exits with code 0 on success, code 1 on fatal errors
- FR55: System can run in dry-run mode (collect but don't export)

### Documentation

- FR56: Project includes a comprehensive README with getting started guide, prerequisites, and installation instructions
- FR57: README documents GitHub token generation process with step-by-step instructions
- FR58: README includes CLI command examples for all available commands (collect, discover, add, remove, list, serve)
- FR59: README documents daemon setup, configuration options, and scheduling
- FR60: Project provides a MkDocs documentation site with structured user guide, architecture overview, and configuration reference
- FR61: MkDocs site is deployable to GitHub Pages for public access

### CI/CD Pipeline

- FR62: GitHub Actions workflow builds the project on every push and pull request
- FR63: GitHub Actions workflow runs the full test suite on every push and pull request
- FR64: GitHub Actions workflow runs code quality checks (go vet, staticcheck, gofmt validation)
- FR65: GitHub Actions workflow runs security vulnerability scanning (govulncheck)
- FR66: GitHub Actions workflow creates cross-platform release binaries on tag push (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64)
- FR67: GitHub Actions workflow publishes release artifacts as GitHub Release assets

## Non-Functional Requirements

### Integration

- NFR1: System must use GitHub REST API v3 with authenticated requests
- NFR2: System must comply with OTLP/HTTP 1.0.0 specification for metrics export
- NFR3: System must support Dynatrace OTLP endpoint with Api-Token header authentication
- NFR4: System must handle GitHub API pagination for large result sets
- NFR5: System must support OTel resource attributes per OpenTelemetry semantic conventions

### Security

- NFR6: System must read credentials exclusively from environment variables
- NFR7: System must never log credentials or tokens at any log level
- NFR8: System should warn if configuration file has overly permissive permissions
- NFR9: System must use HTTPS for all external API communications

### Reliability

- NFR10: System must continue processing remaining repositories after individual repo failures
- NFR11: System must implement graceful backoff when GitHub API rate limit is approached (trigger at 80% of limit)
- NFR12: System must write state file atomically to prevent corruption on crash
- NFR13: System must be idempotent — re-running collection produces consistent results
- NFR14: System must recover gracefully from transient network failures with retry logic (3 retries, exponential backoff)
- NFR15: Rate limiting edge cases must be tested: backoff triggers, partial result handling, resume from interruption

### Quality Assurance

- NFR16: Unit test coverage must be ≥80% for core modules (collection, scoring, export)
- NFR17: Integration tests must validate metrics accuracy using known GitHub repositories as fixtures
- NFR18: Edge case scenarios must have dedicated test coverage: rate limiting, API failures, malformed responses
- NFR19: Test suite must execute automatically in CI pipeline on every pull request
- NFR20: Integration tests must use real GitHub API calls against stable reference repositories

### Documentation & Developer Experience

- NFR21: All user-facing documentation must be kept in sync with the current CLI behavior and configuration schema
- NFR22: CI/CD pipeline must block merges on test failures, quality violations, or known security vulnerabilities
