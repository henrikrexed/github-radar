---
stepsCompleted: ['step-01-validate-prerequisites', 'step-02-design-epics', 'step-03-create-stories', 'step-04-final-validation']
workflowStatus: complete
completedAt: '2026-02-16'
lastEdited: '2026-02-16'
editHistory:
  - date: '2026-02-16'
    changes: "Added Epic 8 (Documentation & User Guide, 6 stories) and Epic 9 (CI/CD Pipeline, 7 stories) as MVP epics. Renumbered post-MVP epics from 8-12 to 10-14. Added FR56-FR67 to requirements inventory and FR coverage map."
inputDocuments:
  - type: prd
    path: prd.md
    description: Complete PRD with 55 FRs, 20 NFRs for GitHub Radar CLI tool
  - type: architecture
    path: architecture.md
    description: Architecture decisions - Go language, client/daemon separation
  - type: feature-spec
    path: feature-spec-category-classification.md
    description: Post-MVP category classification feature specification
workflowType: 'epics'
project_name: 'GitHub Radar'
user_name: 'Henrik.rexed'
date: '2026-02-15'
---

# GitHub Trend Scanner - Epic Breakdown

## Overview

This document provides the complete epic and story breakdown for GitHub Trend Scanner, decomposing the requirements from the PRD and Architecture into implementable stories.

## Requirements Inventory

### Functional Requirements

**Repository Management (FR1-FR6)**
- FR1: Operator can define categories of repositories to track in configuration
- FR2: Operator can add specific repositories to a category
- FR3: Operator can remove repositories from tracking via CLI
- FR4: Operator can add repositories to an exclusion list (blacklist)
- FR5: Operator can view all tracked repositories with their current scores
- FR6: System skips excluded repositories during collection runs

**Topic Discovery (FR7-FR12)**
- FR7: System can query GitHub Search API for repositories matching configured topics
- FR8: System can filter discovered repositories by minimum star count
- FR9: System can filter discovered repositories by maximum age (days since creation)
- FR10: System can automatically add discovered repositories to tracking when growth score exceeds threshold
- FR11: Operator can configure which topics to scan for discovery
- FR12: Operator can enable or disable automatic discovery

**Data Collection (FR13-FR23)**
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

**Growth Analysis (FR24-FR30)**
- FR24: System can calculate star velocity (stars gained per day)
- FR25: System can calculate star acceleration (velocity change from previous period)
- FR26: System can calculate PR velocity (PRs merged per day)
- FR27: System can calculate issue velocity (new issues per day)
- FR28: System can calculate contributor growth (new contributors per day)
- FR29: System can compute composite growth score using configurable weights
- FR30: System can normalize growth scores to 0-100 scale

**State Management (FR31-FR35)**
- FR31: System can persist collection state to JSON file
- FR32: System can load previous state for week-over-week comparison
- FR33: System tracks per-repository: last star count, last collection timestamp, previous velocities
- FR34: System tracks discovery state: last scan timestamp, known repositories
- FR35: Operator can configure state file location via CLI flag or environment variable

**Metrics Export (FR36-FR41)**
- FR36: System can export all collected metrics as OpenTelemetry metrics
- FR37: System can export metrics via OTLP HTTP to configurable endpoint
- FR38: System includes resource attributes (service.name, service.version) in metrics
- FR39: System can include custom headers in OTLP requests (for authentication)
- FR40: System flushes all metrics synchronously before process exit
- FR41: Metrics include repository dimensions (org, name, language, topics, category)

**Configuration (FR42-FR47)**
- FR42: Operator can configure GitHub token via environment variable substitution
- FR43: Operator can configure OTLP endpoint and authentication headers
- FR44: Operator can configure scoring weights for growth formula
- FR45: Operator can configure rate limit threshold to stay under GitHub limits
- FR46: Operator can specify configuration file path via CLI flag
- FR47: System validates configuration on startup and reports errors

**Observability (FR48-FR55)**
- FR48: System logs scan progress and summary statistics at INFO level
- FR49: System logs detailed API calls and metric values at DEBUG level
- FR50: System logs rate limit warnings at WARN level
- FR51: System logs fatal errors at ERROR level
- FR52: System skips individual repositories on API errors without failing the entire run
- FR53: System logs warnings for repositories that return 404 (deleted/renamed)
- FR54: System exits with code 0 on success, code 1 on fatal errors
- FR55: System can run in dry-run mode (collect but don't export)

**Documentation (FR56-FR61)**
- FR56: Project includes comprehensive README with getting started guide
- FR57: README documents GitHub token generation process
- FR58: README includes CLI command examples for all commands
- FR59: README documents daemon setup and configuration
- FR60: Project provides MkDocs documentation site
- FR61: MkDocs site is deployable to GitHub Pages

**CI/CD Pipeline (FR62-FR67)**
- FR62: GitHub Actions workflow builds on every push/PR
- FR63: GitHub Actions workflow runs full test suite
- FR64: GitHub Actions workflow runs quality checks (go vet, staticcheck, gofmt)
- FR65: GitHub Actions workflow runs security scans (govulncheck)
- FR66: GitHub Actions workflow creates cross-platform release binaries on tag
- FR67: GitHub Actions workflow publishes release artifacts as GitHub Release assets

### NonFunctional Requirements

**Integration (NFR1-NFR5)**
- NFR1: System must use GitHub REST API v3 with authenticated requests
- NFR2: System must comply with OTLP/HTTP 1.0.0 specification for metrics export
- NFR3: System must support Dynatrace OTLP endpoint with Api-Token header authentication
- NFR4: System must handle GitHub API pagination for large result sets
- NFR5: System must support OTel resource attributes per OpenTelemetry semantic conventions

**Security (NFR6-NFR9)**
- NFR6: System must read credentials exclusively from environment variables
- NFR7: System must never log credentials or tokens at any log level
- NFR8: System should warn if configuration file has overly permissive permissions
- NFR9: System must use HTTPS for all external API communications

**Reliability (NFR10-NFR14)**
- NFR10: System must continue processing remaining repositories after individual repo failures
- NFR11: System must implement graceful backoff when GitHub API rate limit is approached
- NFR12: System must write state file atomically to prevent corruption on crash
- NFR13: System must be idempotent — re-running collection produces consistent results
- NFR14: System must recover gracefully from transient network failures with retry logic

### Additional Requirements

**From Architecture Document:**

- **Language:** Go 1.22+ (supersedes PRD's TypeScript specification)
- **Architecture Pattern:** Client/daemon separation - CLI is non-blocking, daemon runs in background
- **Project Initialization:** Standard Go project layout with `go mod init`
- **CLI Framework:** Commander.js-style approach using Go standard library
- **GitHub Client:** go-github/v60 with built-in rate limiting
- **OTel SDK:** go.opentelemetry.io/otel for metrics export
- **Logging:** slog (Go stdlib) for structured logging
- **Build Tooling:** Makefile for build, Docker for distribution
- **Distribution:** Docker image (primary), native binaries (optional)
- **Daemon Communication:** Shared config file + HTTP status endpoint on localhost:8080
- **State Persistence:** Write-rename pattern for atomic file writes
- **Config Reload:** SIGHUP signal or file watch for daemon config refresh

**Implementation Sequence (from Architecture):**
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

### FR Coverage Map

| FR | Epic | Description |
|----|------|-------------|
| FR1 | Epic 2 | Define categories in configuration |
| FR2 | Epic 2 | Add repositories to category |
| FR3 | Epic 2 | Remove repositories via CLI |
| FR4 | Epic 2 | Add to exclusion list |
| FR5 | Epic 2 | View tracked repos with scores |
| FR6 | Epic 2 | Skip excluded repos |
| FR7 | Epic 6 | Query GitHub Search API |
| FR8 | Epic 6 | Filter by minimum stars |
| FR9 | Epic 6 | Filter by maximum age |
| FR10 | Epic 6 | Auto-add high-growth repos |
| FR11 | Epic 6 | Configure discovery topics |
| FR12 | Epic 6 | Enable/disable discovery |
| FR13 | Epic 3 | Collect star count |
| FR14 | Epic 3 | Collect fork count |
| FR15 | Epic 3 | Collect open issues |
| FR16 | Epic 3 | Collect open PRs |
| FR17 | Epic 3 | Collect merged PRs (7 days) |
| FR18 | Epic 3 | Collect new issues (7 days) |
| FR19 | Epic 3 | Collect contributor count |
| FR20 | Epic 3 | Collect latest release |
| FR21 | Epic 3 | Collect language and topics |
| FR22 | Epic 3 | Respect rate limits |
| FR23 | Epic 3 | Use conditional requests |
| FR24 | Epic 4 | Calculate star velocity |
| FR25 | Epic 4 | Calculate star acceleration |
| FR26 | Epic 4 | Calculate PR velocity |
| FR27 | Epic 4 | Calculate issue velocity |
| FR28 | Epic 4 | Calculate contributor growth |
| FR29 | Epic 4 | Compute composite score |
| FR30 | Epic 4 | Normalize scores 0-100 |
| FR31 | Epic 3 | Persist state to JSON |
| FR32 | Epic 3 | Load previous state |
| FR33 | Epic 3 | Track per-repo metrics |
| FR34 | Epic 3 | Track discovery state |
| FR35 | Epic 3 | Configure state file path |
| FR36 | Epic 5 | Export as OTel metrics |
| FR37 | Epic 5 | Export via OTLP HTTP |
| FR38 | Epic 5 | Include resource attributes |
| FR39 | Epic 5 | Include custom headers |
| FR40 | Epic 5 | Flush metrics on exit |
| FR41 | Epic 5 | Include repo dimensions |
| FR42 | Epic 1 | Configure GitHub token |
| FR43 | Epic 1 | Configure OTLP endpoint |
| FR44 | Epic 1 | Configure scoring weights |
| FR45 | Epic 1 | Configure rate limit threshold |
| FR46 | Epic 1 | Specify config file path |
| FR47 | Epic 1 | Validate config on startup |
| FR48 | Epic 5 | Log progress at INFO |
| FR49 | Epic 5 | Log details at DEBUG |
| FR50 | Epic 5 | Log rate warnings at WARN |
| FR51 | Epic 5 | Log errors at ERROR |
| FR52 | Epic 5 | Skip failed repos |
| FR53 | Epic 5 | Warn on 404 repos |
| FR54 | Epic 5 | Exit codes 0/1 |
| FR55 | Epic 5 | Dry-run mode |
| FR56 | Epic 8 | Comprehensive README |
| FR57 | Epic 8 | Token generation guide |
| FR58 | Epic 8 | CLI command examples |
| FR59 | Epic 8 | Daemon setup documentation |
| FR60 | Epic 8 | MkDocs documentation site |
| FR61 | Epic 8 | GitHub Pages deployment |
| FR62 | Epic 9 | Build on push/PR |
| FR63 | Epic 9 | Test on push/PR |
| FR64 | Epic 9 | Quality checks |
| FR65 | Epic 9 | Security scans |
| FR66 | Epic 9 | Cross-platform release binaries |
| FR67 | Epic 9 | Publish release assets |

## Epic List

### Epic 1: Project Foundation & Configuration
Operator can configure the system with credentials, endpoints, and preferences. This establishes the foundation for all other functionality.

**FRs Covered:** FR42, FR43, FR44, FR45, FR46, FR47
**NFRs Addressed:** NFR6, NFR7, NFR8, NFR9

---

### Epic 2: Repository Tracking Management
Operator can define, add, remove, and view repositories to monitor. Provides CLI commands for managing the tracking list.

**FRs Covered:** FR1, FR2, FR3, FR4, FR5, FR6

---

### Epic 3: GitHub Data Collection
System collects comprehensive metrics from GitHub for all tracked repositories, with state persistence for week-over-week comparison.

**FRs Covered:** FR13, FR14, FR15, FR16, FR17, FR18, FR19, FR20, FR21, FR22, FR23, FR31, FR32, FR33, FR34, FR35
**NFRs Addressed:** NFR1, NFR4, NFR10, NFR11, NFR12, NFR13, NFR14

---

### Epic 4: Growth Scoring & Analysis
System calculates growth velocities and composite scores to identify trending projects. Enables data-driven content prioritization.

**FRs Covered:** FR24, FR25, FR26, FR27, FR28, FR29, FR30

---

### Epic 5: OpenTelemetry Metrics Export
System exports all metrics via OTLP HTTP to any compatible backend (e.g., Dynatrace). Includes structured logging and observability.

**FRs Covered:** FR36, FR37, FR38, FR39, FR40, FR41, FR48, FR49, FR50, FR51, FR52, FR53, FR54, FR55
**NFRs Addressed:** NFR2, NFR3, NFR5

---

### Epic 6: Topic-Based Discovery
System automatically discovers new trending repositories matching configured topics, enabling early detection of viral projects.

**FRs Covered:** FR7, FR8, FR9, FR10, FR11, FR12

---

### Epic 7: Background Daemon & Distribution
Operator can run continuous scheduled monitoring via background daemon. Includes Docker packaging for easy deployment.

**Architecture Requirements:** serve command, HTTP status endpoint, Docker image, native binaries

---

### Epic 8: Documentation & User Guide
Project provides comprehensive documentation for new users, including README with getting started guide, GitHub token setup, CLI examples, daemon configuration, and a MkDocs documentation site deployable to GitHub Pages.

**FRs Covered:** FR56, FR57, FR58, FR59, FR60, FR61
**NFRs Addressed:** NFR21

---

### Epic 9: CI/CD Pipeline
GitHub Actions workflows automate build, test, quality checks, security scans, and cross-platform release artifact creation. Ensures every PR is validated and releases are published automatically.

**FRs Covered:** FR62, FR63, FR64, FR65, FR66, FR67
**NFRs Addressed:** NFR19, NFR22

---

## Epic 1: Project Foundation & Configuration

Operator can configure the system with credentials, endpoints, and preferences. This establishes the foundation for all other functionality.

### Story 1.1: Initialize Go Project Structure

As an **operator**,
I want the project scaffolded with proper Go structure,
So that development can begin with correct conventions.

**Acceptance Criteria:**

**Given** a new project directory
**When** `go mod init github.com/{owner}/github-radar` is run
**Then** go.mod is created with Go 1.22+ requirement
**And** directory structure matches Architecture specification (cmd/, internal/, pkg/, configs/)

---

### Story 1.2: Load YAML Configuration File

As an **operator**,
I want to specify my configuration in a YAML file,
So that I can easily manage settings without recompiling.

**Acceptance Criteria:**

**Given** a valid YAML config file exists at the specified path
**When** the application starts with `--config ./config.yaml`
**Then** all configuration values are loaded into memory
**And** the config file path can be specified via `GITHUB_RADAR_CONFIG` env var

*(Addresses: FR46)*

---

### Story 1.3: Environment Variable Substitution in Config

As an **operator**,
I want to use `${ENV_VAR}` syntax in my config file,
So that I can inject secrets without hardcoding them.

**Acceptance Criteria:**

**Given** a config with `token: ${GITHUB_TOKEN}`
**When** the config is loaded and `GITHUB_TOKEN=abc123` is set
**Then** the token value resolves to `abc123`
**And** missing env vars cause a clear error message

*(Addresses: FR42, NFR6)*

---

### Story 1.4: Validate Configuration on Startup

As an **operator**,
I want the system to validate my config before running,
So that I catch errors early rather than mid-execution.

**Acceptance Criteria:**

**Given** a config file is loaded
**When** the application starts
**Then** required fields are validated (github.token, otel.endpoint)
**And** URL formats are validated (OTLP endpoint must be valid URL)
**And** scoring weights are validated (must be positive numbers)
**And** rate limit threshold is validated (must be 1-5000)
**And** credentials are never logged in error messages (NFR7)

*(Addresses: FR43, FR44, FR45, FR47, NFR7)*

---

### Story 1.5: Warn on Insecure Config File Permissions

As an **operator**,
I want a warning if my config file is world-readable,
So that I'm alerted to potential credential exposure.

**Acceptance Criteria:**

**Given** a config file with permissions more permissive than 0600
**When** the config is loaded
**Then** a WARN log is emitted about insecure permissions
**And** the application continues (non-blocking warning)

*(Addresses: NFR8)*

---

### Story 1.6: Validate GitHub Token Permissions

As an **operator**,
I want the system to verify my GitHub token works,
So that I know authentication is correct before scanning.

**Acceptance Criteria:**

**Given** a GitHub token is configured
**When** the application starts (or `config validate` is run)
**Then** a test API call is made to GitHub (e.g., `/user` or `/rate_limit`)
**And** if the token is invalid/expired, a clear error is shown
**And** if the token lacks required scopes, a warning is shown
**And** the rate limit remaining is logged at INFO level

*(Addresses: NFR1, NFR9)*

---

### Story 1.7: Validate OTLP Endpoint Connectivity

As an **operator**,
I want the system to verify the OTLP endpoint is reachable,
So that I know metrics export will work before scanning.

**Acceptance Criteria:**

**Given** an OTLP endpoint is configured
**When** `config validate` is run (or `--validate` flag on startup)
**Then** a health check or test request is made to the endpoint
**And** if unreachable, a clear error with connection details is shown
**And** if auth headers are rejected (401/403), the error indicates auth failure
**And** successful validation logs the endpoint at INFO level

*(Addresses: FR43, NFR2, NFR3, NFR9)*

---

## Epic 2: Repository Tracking Management

Operator can define, add, remove, and view repositories to monitor. Provides CLI commands for managing the tracking list.

### Story 2.1: Parse Repository Identifiers Flexibly

As an **operator**,
I want the system to accept repositories in multiple formats,
So that I can add repos from various sources without format constraints.

**Acceptance Criteria:**

**Given** a repository identifier is provided
**When** the system parses it
**Then** it accepts: `owner/repo`, `https://github.com/owner/repo`, `github.com/owner/repo`
**And** it extracts owner and repo name correctly
**And** invalid formats return a clear error with expected format examples

---

### Story 2.2: Define Repository Categories in Config

As an **operator**,
I want to organize tracked repositories into categories,
So that I can group repos by topic for better dashboard organization.

**Acceptance Criteria:**

**Given** a config with category definitions
**When** repos are added to categories
**Then** categories are dynamic (created on-demand)
**And** repos can belong to multiple categories
**And** uncategorized repos go to a "default" category

*(Addresses: FR1)*

---

### Story 2.3: Add Repository via CLI

As an **operator**,
I want to add any valid GitHub repository via CLI,
So that I can track any public repo I discover.

**Acceptance Criteria:**

**Given** a valid GitHub repository exists
**When** `github-radar add <repo> [--category <name>]`
**Then** the repo is validated against GitHub API (exists, is public)
**And** the repo is added to tracking
**And** if no category specified, uses "default"

*(Addresses: FR2)*

---

### Story 2.4: Remove Repository via CLI

As an **operator**,
I want to remove any repository from tracking,
So that I can stop monitoring repos I no longer need.

**Acceptance Criteria:**

**Given** any tracked repository
**When** `github-radar remove <repo>`
**Then** the repo is removed from all categories
**And** the repo's state data is optionally preserved (`--keep-state`)
**And** works with any valid repo format

*(Addresses: FR3)*

---

### Story 2.5: Manage Exclusion List

As an **operator**,
I want to maintain a flexible exclusion list,
So that specific repos are never tracked regardless of discovery.

**Acceptance Criteria:**

**Given** an exclusion pattern (repo or org/*)
**When** `github-radar exclude <pattern>`
**Then** exact repos (`owner/repo`) are excluded
**And** wildcard patterns (`owner/*`) exclude entire orgs
**And** exclusions override any discovery or manual adds

*(Addresses: FR4)*

---

### Story 2.6: List All Tracked Repositories

As an **operator**,
I want to view all tracked repositories dynamically,
So that I can see the current state regardless of how repos were added.

**Acceptance Criteria:**

**Given** repos from config, CLI adds, and discovery
**When** `github-radar list [--category <name>] [--format table|json|csv]`
**Then** all tracked repos are shown with current metrics
**And** filtering by category is supported
**And** output format is configurable for scripting

*(Addresses: FR5)*

---

### Story 2.7: Skip Excluded Repositories

As the **system**,
I want to automatically skip excluded repos and patterns,
So that blacklisted items never consume resources.

**Acceptance Criteria:**

**Given** exclusion patterns in config
**When** any operation processes repos
**Then** excluded repos are filtered out before processing
**And** pattern matching supports exact and wildcard
**And** exclusion happens at the earliest possible point

*(Addresses: FR6)*

---

## Epic 3: GitHub Data Collection

System collects comprehensive metrics from GitHub for all tracked repositories, with state persistence for week-over-week comparison.

### Story 3.1: Initialize GitHub API Client

As the **system**,
I want a configured GitHub API client with authentication,
So that all API operations use proper credentials and rate limiting.

**Acceptance Criteria:**

**Given** a GitHub token in environment
**When** the client is initialized
**Then** authentication is configured for REST API v3 (NFR1)
**And** rate limit tracking is enabled
**And** HTTPS is enforced for all requests (NFR9)

---

### Story 3.2: Collect Core Repository Metrics

As the **system**,
I want to collect basic metrics for each tracked repository,
So that I have foundational data for growth analysis.

**Acceptance Criteria:**

**Given** a list of tracked repositories
**When** collection runs
**Then** star count is collected (FR13)
**And** fork count is collected (FR14)
**And** open issues count is collected (FR15)
**And** open PRs count is collected (FR16)
**And** primary language and topics are collected (FR21)

---

### Story 3.3: Collect Activity Metrics (7-Day Window)

As the **system**,
I want to collect recent activity metrics,
So that I can calculate velocity and detect sudden growth.

**Acceptance Criteria:**

**Given** a tracked repository
**When** activity metrics are collected
**Then** PRs merged in last 7 days is counted (FR17)
**And** issues opened in last 7 days is counted (FR18)
**And** contributor count is retrieved (FR19)
**And** latest release tag and date are retrieved (FR20)

---

### Story 3.4: Handle GitHub API Pagination

As the **system**,
I want to automatically handle paginated API responses,
So that large result sets are fully retrieved.

**Acceptance Criteria:**

**Given** an API response with pagination links
**When** results exceed page size
**Then** all pages are automatically fetched (NFR4)
**And** results are combined correctly
**And** pagination respects rate limits

---

### Story 3.5: Track and Respect Rate Limits

As the **system**,
I want to monitor GitHub API rate limits,
So that I never exceed quota and get blocked.

**Acceptance Criteria:**

**Given** a configurable rate limit threshold (e.g., 4000 of 5000)
**When** remaining requests approach threshold
**Then** collection pauses or slows down (NFR11)
**And** rate limit status is logged at WARN level
**And** X-RateLimit headers are parsed and tracked (FR22)

---

### Story 3.6: Use Conditional Requests

As the **system**,
I want to use If-Modified-Since headers,
So that unchanged repos don't consume rate limit quota.

**Acceptance Criteria:**

**Given** a previously collected repo with ETag/Last-Modified
**When** re-collecting the same repo
**Then** conditional request headers are sent (FR23)
**And** 304 Not Modified responses skip re-processing
**And** rate limit is preserved for unchanged repos

---

### Story 3.7: Initialize State Store

As the **system**,
I want a JSON-based state persistence layer,
So that data survives between runs for delta calculation.

**Acceptance Criteria:**

**Given** a state file path (default or configured via FR35)
**When** the system starts
**Then** existing state is loaded if present (FR32)
**And** missing state file initializes empty state
**And** state file path can be set via `--state` flag or env var

---

### Story 3.8: Persist Collection State

As the **system**,
I want to save collection state after each run,
So that week-over-week comparisons are possible.

**Acceptance Criteria:**

**Given** collection completes (fully or partially)
**When** state is persisted
**Then** per-repo data is saved: stars, timestamp, velocities (FR33)
**And** discovery state is saved: last scan, known repos (FR34)
**And** writes use atomic rename pattern (NFR12)

---

### Story 3.9: Handle Individual Repository Failures

As the **system**,
I want to continue processing when individual repos fail,
So that one bad repo doesn't block the entire scan.

**Acceptance Criteria:**

**Given** an API error for a specific repository
**When** the error occurs
**Then** the repo is skipped with a warning log (NFR10)
**And** 404 errors log "repo may be deleted/renamed" (FR53)
**And** other repos continue processing
**And** partial results are still saved

---

### Story 3.10: Implement Retry Logic for Transient Failures

As the **system**,
I want to retry failed requests with backoff,
So that temporary network issues don't cause failures.

**Acceptance Criteria:**

**Given** a transient error (timeout, 5xx, network error)
**When** the request fails
**Then** retry with exponential backoff (NFR14)
**And** max 3 retries before marking as failed
**And** permanent errors (4xx except 429) don't retry

---

### Story 3.11: Ensure Idempotent Collection

As the **system**,
I want collection runs to be idempotent,
So that re-running produces consistent results.

**Acceptance Criteria:**

**Given** the same repos and time window
**When** collection runs multiple times
**Then** results are consistent (NFR13)
**And** state is not corrupted by partial runs
**And** metrics reflect current GitHub state, not accumulated

---

## Epic 4: Growth Scoring & Analysis

System calculates growth velocities and composite scores to identify trending projects.

### Story 4.1: Calculate Star Velocity

As the **system**,
I want to calculate stars gained per day,
So that I can identify repos with growing popularity.

**Acceptance Criteria:**

**Given** current star count and previous star count from state
**When** velocity is calculated
**Then** star_velocity = (current_stars - previous_stars) / days_elapsed
**And** first-time repos use 0 as baseline
**And** negative velocity (star loss) is captured accurately

*(Addresses: FR24)*

---

### Story 4.2: Calculate Star Acceleration

As the **system**,
I want to calculate velocity change from previous period,
So that I can detect repos with accelerating growth.

**Acceptance Criteria:**

**Given** current velocity and previous velocity from state
**When** acceleration is calculated
**Then** star_acceleration = current_velocity - previous_velocity
**And** positive acceleration indicates speeding up
**And** requires at least 2 data points (else 0)

*(Addresses: FR25)*

---

### Story 4.3: Calculate PR Velocity

As the **system**,
I want to calculate PRs merged per day,
So that I can measure development activity.

**Acceptance Criteria:**

**Given** PRs merged in last 7 days
**When** velocity is calculated
**Then** pr_velocity = merged_prs / 7
**And** higher velocity indicates active development
**And** value is normalized to daily rate

*(Addresses: FR26)*

---

### Story 4.4: Calculate Issue Velocity

As the **system**,
I want to calculate new issues per day,
So that I can measure community engagement.

**Acceptance Criteria:**

**Given** issues opened in last 7 days
**When** velocity is calculated
**Then** issue_velocity = new_issues / 7
**And** high issue velocity can indicate popularity OR problems
**And** value is normalized to daily rate

*(Addresses: FR27)*

---

### Story 4.5: Calculate Contributor Growth

As the **system**,
I want to track contributor count changes,
So that I can identify repos attracting new developers.

**Acceptance Criteria:**

**Given** current and previous contributor counts
**When** growth is calculated
**Then** contributor_growth = (current - previous) / days_elapsed
**And** new repos use current count as baseline
**And** contributor data comes from GitHub API

*(Addresses: FR28)*

---

### Story 4.6: Compute Composite Growth Score

As the **system**,
I want to calculate a weighted composite score,
So that I can rank repos by overall growth potential.

**Acceptance Criteria:**

**Given** all velocity metrics and configurable weights
**When** composite score is calculated
**Then** formula applies:
```
growth_score = (star_velocity × weight_star) +
               (star_acceleration × weight_accel) +
               (contributor_growth × weight_contrib) +
               (pr_velocity × weight_pr) +
               (issue_velocity × weight_issue)
```
**And** default weights: star=2.0, accel=3.0, contrib=1.5, pr=1.0, issue=0.5
**And** weights are configurable in config (FR44)

*(Addresses: FR29)*

---

### Story 4.7: Normalize Growth Scores

As the **system**,
I want to normalize scores to a 0-100 scale,
So that scores are comparable and dashboard-friendly.

**Acceptance Criteria:**

**Given** raw composite growth scores for all repos
**When** normalization is applied
**Then** scores are scaled to 0-100 range
**And** normalization uses min-max or percentile scaling
**And** a score of 100 represents highest growth in the set
**And** negative raw scores map to low end (0-20 range)

*(Addresses: FR30)*

---

## Epic 5: OpenTelemetry Metrics Export

System exports all metrics via OTLP HTTP to any compatible backend (e.g., Dynatrace).

### Story 5.1: Initialize OTel Metrics SDK

As the **system**,
I want to configure the OpenTelemetry metrics SDK,
So that metrics can be exported via OTLP HTTP.

**Acceptance Criteria:**

**Given** OTLP endpoint and headers in config
**When** the SDK is initialized
**Then** OTLP/HTTP 1.0.0 exporter is configured (NFR2)
**And** custom headers (e.g., Api-Token) are supported (FR39)
**And** resource attributes are set (service.name, service.version) (FR38)

---

### Story 5.2: Configure Resource Attributes

As the **system**,
I want to include OTel semantic resource attributes,
So that metrics are properly identified in the backend.

**Acceptance Criteria:**

**Given** OTel semantic conventions (NFR5)
**When** metrics are exported
**Then** `service.name` = configured service name (default: github-radar)
**And** `service.version` = application version
**And** additional custom attributes from config are supported

*(Addresses: FR38, NFR5)*

---

### Story 5.3: Export Repository Metrics

As the **system**,
I want to export all collected repo metrics as OTel gauges,
So that they appear in the observability backend.

**Acceptance Criteria:**

**Given** collected metrics for each repository
**When** metrics are recorded
**Then** all metrics use `github.repo.*` namespace
**And** each metric includes dimensions: repo_owner, repo_name, category, language (FR41)
**And** metrics include: stars, forks, issues, prs, growth_score, velocities

*(Addresses: FR36, FR41)*

---

### Story 5.4: Support Dynatrace OTLP Endpoint

As an **operator**,
I want to export metrics to Dynatrace via OTLP,
So that I can use Dynatrace for dashboards and alerting.

**Acceptance Criteria:**

**Given** Dynatrace OTLP endpoint URL
**When** configured with `Authorization: Api-Token ${DT_API_TOKEN}`
**Then** metrics are ingested by Dynatrace (NFR3)
**And** endpoint URL format: `https://{env}.live.dynatrace.com/api/v2/otlp/v1/metrics`
**And** connection errors show clear diagnostics

*(Addresses: NFR3)*

---

### Story 5.5: Flush Metrics on Exit

As the **system**,
I want to synchronously flush all metrics before process exit,
So that no data is lost on shutdown.

**Acceptance Criteria:**

**Given** metrics have been recorded
**When** the process exits (normal or error)
**Then** all pending metrics are flushed synchronously (FR40)
**And** flush timeout is configurable (default: 10s)
**And** flush errors are logged but don't change exit code

*(Addresses: FR40)*

---

### Story 5.6: Configure Structured Logging with slog

As the **system**,
I want structured JSON logging via slog,
So that logs are parseable and filterable.

**Acceptance Criteria:**

**Given** slog is the logging framework
**When** logs are emitted
**Then** output is structured JSON
**And** log level is configurable via `--log-level` flag
**And** attributes use snake_case per Architecture patterns

---

### Story 5.7: Log at Appropriate Levels

As the **system**,
I want to log events at correct severity levels,
So that operators can filter and alert appropriately.

**Acceptance Criteria:**

**Given** logging is configured
**When** events occur
**Then** scan progress/summary logs at INFO (FR48)
**And** API calls/metric values log at DEBUG (FR49)
**And** rate limit warnings log at WARN (FR50)
**And** fatal errors log at ERROR (FR51)

*(Addresses: FR48-FR51)*

---

### Story 5.8: Handle Repository Errors Gracefully

As the **system**,
I want to skip failed repos and continue,
So that partial results are still exported.

**Acceptance Criteria:**

**Given** an error occurs for a specific repo
**When** processing continues
**Then** the repo is skipped with a warning (FR52)
**And** 404 errors note "repo may be deleted/renamed" (FR53)
**And** successfully collected repos are still exported

*(Addresses: FR52, FR53)*

---

### Story 5.9: Implement Exit Codes

As the **system**,
I want to return appropriate exit codes,
So that scripts and CI can detect success/failure.

**Acceptance Criteria:**

**Given** a collection run completes
**When** the process exits
**Then** exit code 0 on success (FR54)
**And** exit code 1 on fatal errors (FR54)
**And** partial success (some repos failed) still exits 0

*(Addresses: FR54)*

---

### Story 5.10: Implement Dry-Run Mode

As an **operator**,
I want to run collection without exporting metrics,
So that I can test configuration without sending data.

**Acceptance Criteria:**

**Given** `--dry-run` flag is set
**When** collection runs
**Then** all data is collected and scored normally
**And** metrics are NOT exported to OTLP endpoint (FR55)
**And** logs indicate dry-run mode is active
**And** state IS saved (for testing state persistence)

*(Addresses: FR55)*

---

## Epic 6: Topic-Based Discovery

System automatically discovers new trending repositories matching configured topics.

### Story 6.1: Query GitHub Search API by Topic

As the **system**,
I want to search GitHub for repositories matching configured topics,
So that I can find trending projects in specific domains.

**Acceptance Criteria:**

**Given** topics configured (e.g., kubernetes, opentelemetry, ebpf)
**When** discovery runs
**Then** GitHub Search API is queried for each topic (FR7)
**And** search uses `topic:{topic}` qualifier
**And** results are sorted by stars or recent activity
**And** pagination retrieves all matching results

*(Addresses: FR7)*

---

### Story 6.2: Filter by Minimum Star Count

As an **operator**,
I want to filter discovered repos by minimum stars,
So that I only see established projects, not noise.

**Acceptance Criteria:**

**Given** `discovery.minStars: 100` in config
**When** repos are discovered
**Then** repos with fewer stars are excluded (FR8)
**And** filter is applied before processing
**And** default minimum is configurable (suggest: 100)

*(Addresses: FR8)*

---

### Story 6.3: Filter by Repository Age

As an **operator**,
I want to filter discovered repos by maximum age,
So that I focus on newer projects with growth potential.

**Acceptance Criteria:**

**Given** `discovery.maxAgeDays: 90` in config
**When** repos are discovered
**Then** repos older than threshold are excluded (FR9)
**And** age is calculated from repo creation date
**And** default can be disabled (0 = no age limit)

*(Addresses: FR9)*

---

### Story 6.4: Configure Discovery Topics

As an **operator**,
I want to specify which topics to scan,
So that discovery focuses on my areas of interest.

**Acceptance Criteria:**

**Given** config section `discovery.topics: [kubernetes, ebpf, llm]`
**When** configuration is loaded
**Then** topics list is validated (non-empty strings)
**And** topics can be added/removed via config edit
**And** empty topics list disables discovery

*(Addresses: FR11)*

---

### Story 6.5: Enable/Disable Discovery

As an **operator**,
I want to toggle automatic discovery on/off,
So that I can control when new repos are found.

**Acceptance Criteria:**

**Given** `discovery.enabled: true|false` in config
**When** daemon runs
**Then** discovery only runs if enabled (FR12)
**And** `github-radar discover` CLI works regardless of setting
**And** disabled discovery logs INFO message explaining why skipped

*(Addresses: FR12)*

---

### Story 6.6: Auto-Track High-Growth Discoveries

As the **system**,
I want to automatically add discovered repos that exceed a growth threshold,
So that trending projects are tracked without manual intervention.

**Acceptance Criteria:**

**Given** `discovery.autoTrackThreshold: 50` in config
**When** a discovered repo's growth score exceeds threshold
**Then** the repo is automatically added to tracking (FR10)
**And** auto-added repos go to a "discovered" category
**And** exclusion list is respected (excluded repos never auto-added)
**And** INFO log announces each auto-tracked repo

*(Addresses: FR10)*

---

### Story 6.7: Discover Command (Manual Trigger)

As an **operator**,
I want to manually trigger discovery via CLI,
So that I can find new repos on-demand.

**Acceptance Criteria:**

**Given** discovery is configured
**When** `github-radar discover [--topic <name>] [--dry-run]`
**Then** discovery runs for specified topic (or all if not specified)
**And** results are displayed with scores
**And** `--dry-run` shows what would be tracked without adding
**And** without `--dry-run`, eligible repos are auto-tracked

---

## Epic 7: Background Daemon & Distribution

Operator can run continuous scheduled monitoring via background daemon.

### Story 7.1: Implement Serve Command

As an **operator**,
I want to start the scanner as a foreground daemon,
So that Docker or systemd can manage it as a service.

**Acceptance Criteria:**

**Given** a valid configuration
**When** `github-radar serve` is executed
**Then** the daemon starts in foreground (not detached)
**And** logs are written to stdout/stderr
**And** graceful shutdown on SIGTERM/SIGINT
**And** daemon runs until explicitly stopped

---

### Story 7.2: Implement Scan Scheduling

As the **system**,
I want to run collection on a configurable schedule,
So that metrics are updated automatically.

**Acceptance Criteria:**

**Given** `daemon.schedule: "0 6 * * 1"` (cron syntax) or `daemon.interval: 24h`
**When** the daemon is running
**Then** collection runs at scheduled times
**And** next scheduled run is logged at INFO
**And** manual trigger via SIGHUP is supported
**And** overlapping runs are prevented (skip if previous still running)

---

### Story 7.3: Implement Health Endpoint

As an **operator**,
I want a `/health` endpoint for container probes,
So that Kubernetes/Docker can check if the daemon is alive.

**Acceptance Criteria:**

**Given** daemon is running
**When** `GET http://localhost:8080/health`
**Then** returns `{"healthy": true}` with HTTP 200
**And** returns `{"healthy": false}` with HTTP 503 if unhealthy
**And** health check is lightweight (no external calls)

---

### Story 7.4: Implement Status Endpoint

As an **operator**,
I want a `/status` endpoint showing daemon state,
So that I can check scan progress and metrics.

**Acceptance Criteria:**

**Given** daemon is running
**When** `GET http://localhost:8080/status`
**Then** returns JSON with:
```json
{
  "status": "running|idle|scanning",
  "last_scan": "2026-02-14T06:00:00Z",
  "repos_tracked": 47,
  "next_scan": "2026-02-21T06:00:00Z",
  "rate_limit_remaining": 4500
}
```
**And** CLI `github-radar status` queries this endpoint

---

### Story 7.5: Support Config Reload

As an **operator**,
I want to reload config without restarting the daemon,
So that I can add repos without downtime.

**Acceptance Criteria:**

**Given** daemon is running
**When** SIGHUP is received OR config file changes (if watching enabled)
**Then** config is reloaded
**And** new repos are picked up on next scan
**And** invalid config reload is rejected with error log (keeps old config)

---

### Story 7.6: Create Dockerfile

As an **operator**,
I want a Docker image for easy deployment,
So that I can run the scanner in any container environment.

**Acceptance Criteria:**

**Given** Go source code
**When** `docker build` is run
**Then** multi-stage build produces minimal image
**And** base image is distroless/static or scratch (~15MB)
**And** binary runs as non-root user
**And** config can be mounted at `/etc/github-radar/config.yaml`

---

### Story 7.7: Create Docker Compose Example

As an **operator**,
I want a docker-compose.yml example,
So that I can quickly deploy with proper configuration.

**Acceptance Criteria:**

**Given** Docker image is available
**When** `docker-compose up -d`
**Then** daemon starts with mounted config
**And** environment variables are passed for secrets
**And** health check is configured
**And** state file is persisted via volume

---

### Story 7.8: Implement Cross-Platform Build

As a **developer**,
I want to build binaries for Mac/Windows/Linux,
So that users can run natively without Docker.

**Acceptance Criteria:**

**Given** Go source code
**When** `make release` is run
**Then** binaries are built for: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
**And** binaries are named `github-radar-{os}-{arch}`
**And** GitHub Actions workflow automates releases

---

### Story 7.9: Implement Makefile

As a **developer**,
I want a Makefile for common tasks,
So that build commands are standardized.

**Acceptance Criteria:**

**Given** project source
**When** make targets are run
**Then** `make build` builds local binary
**And** `make test` runs all tests
**And** `make docker` builds Docker image
**And** `make release` cross-compiles all platforms
**And** `make lint` runs go vet + staticcheck

---

## Epic 8: Documentation & User Guide

Project provides comprehensive documentation for new users, including README with getting started guide, GitHub token setup, CLI command examples, daemon configuration, and a MkDocs documentation site.

### Story 8.1: Create Comprehensive README

As an **operator**,
I want a clear and complete README in the project root,
So that I can quickly understand what GitHub Radar does and how to get started.

**Acceptance Criteria:**

**Given** the project repository
**When** a new user visits the GitHub page
**Then** README includes: project description, key features, prerequisites (Go 1.22+, GitHub token)
**And** README includes badges for build status, Go version, and license
**And** README includes a quick-start section with install + first scan in under 5 commands
**And** README includes links to full MkDocs documentation

*(Addresses: FR56)*

---

### Story 8.2: Document GitHub Token Generation

As an **operator**,
I want step-by-step instructions for creating a GitHub token,
So that I can authenticate without guessing the required scopes.

**Acceptance Criteria:**

**Given** a new user needs a GitHub token
**When** they follow the documentation
**Then** instructions cover: navigating to GitHub Settings → Developer Settings → Personal Access Tokens
**And** required scopes are listed: `public_repo` (minimum), `read:org` (optional for org repos)
**And** instructions explain how to set the token as `GITHUB_TOKEN` environment variable
**And** instructions cover both classic tokens and fine-grained tokens
**And** security best practices are noted (never commit tokens, use env vars)

*(Addresses: FR57)*

---

### Story 8.3: Document CLI Command Examples

As an **operator**,
I want examples for every CLI command,
So that I can learn by example without reading full documentation.

**Acceptance Criteria:**

**Given** each CLI command (collect, discover, add, remove, list, serve, config)
**When** the README documents it
**Then** each command has: description, syntax, common flags, and 2-3 practical examples
**And** examples show real-world usage (e.g., discovering Kubernetes projects, adding a specific repo)
**And** output samples are included so users know what to expect
**And** global flags (--config, --state, --dry-run, --verbose) are documented

*(Addresses: FR58)*

---

### Story 8.4: Document Daemon Setup and Configuration

As an **operator**,
I want documentation on running GitHub Radar as a background daemon,
So that I can set up continuous automated scanning.

**Acceptance Criteria:**

**Given** the `serve` command
**When** documentation covers daemon usage
**Then** daemon start/stop commands are documented
**And** configuration options (interval, HTTP address, dry-run) are explained
**And** systemd service file example is provided
**And** Docker Compose deployment is documented
**And** health and status endpoint usage is explained
**And** SIGHUP config reload is documented

*(Addresses: FR59)*

---

### Story 8.5: Create MkDocs Documentation Site

As an **operator**,
I want a structured documentation website,
So that I can find detailed reference material beyond the README.

**Acceptance Criteria:**

**Given** the project documentation needs
**When** MkDocs is configured
**Then** `mkdocs.yml` is present in project root with Material theme
**And** documentation structure includes:
  - Home (overview + quick start)
  - Installation (binary, Docker, from source)
  - Configuration Reference (full YAML schema with descriptions)
  - CLI Commands (detailed per-command reference)
  - Architecture Overview (how the scanner works)
  - Daemon Guide (scheduling, health checks, deployment)
  - OpenTelemetry Integration (endpoints, Dynatrace setup, dashboards)
  - Contributing Guide
**And** `mkdocs serve` runs locally for preview
**And** docs source lives in `docs/` directory

*(Addresses: FR60)*

---

### Story 8.6: Configure MkDocs GitHub Pages Deployment

As a **developer**,
I want the MkDocs site to deploy to GitHub Pages,
So that documentation is publicly accessible.

**Acceptance Criteria:**

**Given** MkDocs site is configured
**When** deployment is triggered
**Then** `mkdocs gh-deploy` builds and pushes to `gh-pages` branch
**And** GitHub Actions workflow automates deployment on merge to main
**And** documentation URL is linked in README and GitHub repo description

*(Addresses: FR61)*

---

## Epic 9: CI/CD Pipeline

GitHub Actions workflows automate build, test, quality checks, security scans, and cross-platform release artifact creation.

### Story 9.1: Create CI Workflow for Build Validation

As a **developer**,
I want the project to build automatically on every push and PR,
So that broken builds are caught before merge.

**Acceptance Criteria:**

**Given** a push or pull request to main/master
**When** the CI workflow triggers
**Then** Go project compiles successfully (`go build ./...`)
**And** build runs on ubuntu-latest with Go 1.22+
**And** build failures block PR merge
**And** workflow file is `.github/workflows/ci.yml`

*(Addresses: FR62)*

---

### Story 9.2: Add Test Execution to CI

As a **developer**,
I want all tests to run automatically in CI,
So that regressions are caught before merge.

**Acceptance Criteria:**

**Given** CI workflow triggers
**When** test step executes
**Then** `go test ./...` runs with `-race` flag for race detection
**And** test results are reported in PR checks
**And** test failures block PR merge
**And** test coverage report is generated and available as artifact

*(Addresses: FR63, NFR19)*

---

### Story 9.3: Add Code Quality Checks to CI

As a **developer**,
I want automated quality checks on every PR,
So that code style and correctness issues are caught early.

**Acceptance Criteria:**

**Given** CI workflow triggers
**When** quality step executes
**Then** `go vet ./...` runs and reports issues
**And** `staticcheck ./...` runs for static analysis
**And** `gofmt` validation checks formatting (diff against gofmt output)
**And** any quality violation blocks PR merge
**And** clear error messages indicate which check failed and how to fix

*(Addresses: FR64)*

---

### Story 9.4: Add Security Scanning to CI

As a **developer**,
I want automated security vulnerability scanning,
So that known vulnerabilities in dependencies are caught.

**Acceptance Criteria:**

**Given** CI workflow triggers
**When** security step executes
**Then** `govulncheck ./...` scans for known Go vulnerabilities
**And** vulnerability findings are reported in PR checks
**And** critical/high vulnerabilities block PR merge
**And** `go mod verify` validates module checksums

*(Addresses: FR65, NFR22)*

---

### Story 9.5: Create Release Workflow for Binary Artifacts

As a **developer**,
I want cross-platform binaries built automatically on tag push,
So that releases are consistent and reproducible.

**Acceptance Criteria:**

**Given** a git tag matching `v*` is pushed (e.g., `v1.0.0`)
**When** the release workflow triggers
**Then** binaries are cross-compiled for: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
**And** binaries are named `github-radar-{os}-{arch}` (with `.exe` for windows)
**And** `CGO_ENABLED=0` is used for static binaries
**And** version is embedded via `-ldflags` from git tag
**And** workflow file is `.github/workflows/release.yml`

*(Addresses: FR66)*

---

### Story 9.6: Publish Release Artifacts to GitHub Releases

As an **operator**,
I want release binaries available as GitHub Release assets,
So that I can download the correct binary for my platform.

**Acceptance Criteria:**

**Given** cross-platform binaries are built
**When** release workflow completes
**Then** a GitHub Release is created with the tag name
**And** all platform binaries are attached as release assets
**And** SHA256 checksums file is generated and attached
**And** release notes are auto-generated from commits since last tag
**And** release is marked as latest

*(Addresses: FR67)*

---

### Story 9.7: Add MkDocs Deployment to CI

As a **developer**,
I want documentation automatically deployed on merge to main,
So that docs stay in sync with the code.

**Acceptance Criteria:**

**Given** a merge to main branch
**When** documentation has changed (docs/ or mkdocs.yml)
**Then** MkDocs site is built and deployed to GitHub Pages
**And** deployment only triggers when docs files change (path filter)
**And** build failure does not block the main branch (docs deployment is non-blocking)

*(Addresses: FR61, NFR21)*

---

# Post-MVP: Category Classification Feature

The following epics implement the Category Classification feature as specified in feature-spec-category-classification.md.

---

## Epic 10: SQLite Database Foundation

**User Outcome:** Operator has reliable, queryable storage that supports classification features while maintaining compatibility with existing CLI commands.

**FRs Covered:** FR-C16, FR-C17, FR-C18

---

## Epic 11: LLM Category Classification

**User Outcome:** System automatically classifies repositories into CNCF/cloud-native categories using local LLM, enabling operators to filter and route alerts by domain.

**FRs Covered:** FR-C1, FR-C2, FR-C3, FR-C4, FR-C5, FR-C6

---

## Epic 12: Classification Overrides & Taxonomy Management

**User Outcome:** Operator can exclude repos, force category overrides, and manage the category taxonomy via CLI, giving full control over classification behavior.

**FRs Covered:** FR-C7, FR-C8, FR-C9, CLI-C1 through CLI-C9

---

## Epic 13: Self-Monitoring & Telemetry

**User Outcome:** Operator can observe the scanner's health, LLM token usage, and resource consumption via their chosen observability backend.

**FRs Covered:** FR-C13, FR-C14, FR-C15, CLI-C16 through CLI-C19

---

## Epic 14: Model Management & Benchmarking

**User Outcome:** Operator can switch between LLM models (accuracy vs speed tradeoff) and benchmark classification performance to make informed decisions.

**FRs Covered:** FR-C10, FR-C11, FR-C12, CLI-C12 through CLI-C15

---

## Post-MVP FR Coverage Map

| FR | Epic | Description |
|----|------|-------------|
| FR-C1 | Epic 11 | Classify repos into 18 categories + other |
| FR-C2 | Epic 11 | Use Ollama with qwen3 models |
| FR-C3 | Epic 11 | Store category, confidence, readme_hash |
| FR-C4 | Epic 11 | Reclassify when README hash changes |
| FR-C5 | Epic 11 | Reclassify all when model changes |
| FR-C6 | Epic 11 | Customizable prompts via config |
| FR-C7 | Epic 12 | Exclude repos from classification |
| FR-C8 | Epic 12 | Force category override |
| FR-C9 | Epic 12 | Manage category taxonomy via CLI |
| FR-C10 | Epic 14 | Support model switching |
| FR-C11 | Epic 14 | Benchmark classification across models |
| FR-C12 | Epic 14 | Benchmark outputs performance metrics |
| FR-C13 | Epic 13 | Export host metrics via OTel |
| FR-C14 | Epic 13 | Export LLM telemetry via OpenLLMetry |
| FR-C15 | Epic 13 | User-configurable collector exporters |
| FR-C16 | Epic 10 | Use SQLite for repo state |
| FR-C17 | Epic 10 | Schema with classification columns |
| FR-C18 | Epic 10 | Excluded repos remain in DB |
| CLI-C1 | Epic 12 | scanner exclude list |
| CLI-C2 | Epic 12 | scanner exclude add |
| CLI-C3 | Epic 12 | scanner exclude remove |
| CLI-C4 | Epic 12 | scanner category list-overrides |
| CLI-C5 | Epic 12 | scanner category set |
| CLI-C6 | Epic 12 | scanner category unset |
| CLI-C7 | Epic 12 | scanner category list |
| CLI-C8 | Epic 12 | scanner category add |
| CLI-C9 | Epic 12 | scanner category remove |
| CLI-C10 | Epic 11 | scanner classify |
| CLI-C11 | Epic 11 | scanner classify test |
| CLI-C12 | Epic 14 | scanner classify model |
| CLI-C13 | Epic 14 | scanner classify model <name> |
| CLI-C14 | Epic 14 | scanner benchmark --sample |
| CLI-C15 | Epic 14 | scanner benchmark --compare |
| CLI-C16 | Epic 13 | scanner telemetry status |
| CLI-C17 | Epic 13 | scanner telemetry enable/disable |
| CLI-C18 | Epic 13 | scanner telemetry exporters |
| CLI-C19 | Epic 13 | scanner telemetry test |

---

## Epic 10: SQLite Database Foundation - Stories

Operator has reliable, queryable storage that supports classification features while maintaining compatibility with existing CLI commands.

### Story 10.1: Initialize SQLite Database Schema

As an **operator**,
I want the scanner to use SQLite for persistent storage,
So that I have reliable, queryable data without external database services.

**Acceptance Criteria:**

**Given** the scanner is run for the first time
**When** no database file exists
**Then** SQLite database is created at configured path (default: `data/scanner.db`)
**And** `repos` table is created with columns: id, full_name, owner, name, language, description, stars, forks, open_issues, open_prs, contributors, stars_prev, growth_score, star_velocity, star_acceleration, latest_release, latest_release_date, created_at, first_seen_at, last_collected_at, topics, status

---

### Story 10.2: Add Classification Columns to Schema

As the **system**,
I want classification-related columns in the repos table,
So that category data can be stored alongside repo metrics.

**Acceptance Criteria:**

**Given** SQLite database exists
**When** classification feature is enabled
**Then** schema includes: primary_category, category_confidence, readme_hash, classified_at, model_used, force_category, excluded (INTEGER)
**And** indexes exist on: primary_category, status, excluded

---

### Story 10.3: Migrate JSON State to SQLite

As an **operator**,
I want existing JSON state data migrated to SQLite,
So that I don't lose historical data when upgrading.

**Acceptance Criteria:**

**Given** existing `state.json` file with repo data
**When** scanner starts and detects JSON state but no SQLite
**Then** data is migrated to SQLite repos table
**And** JSON file is renamed to `state.json.migrated`
**And** migration is logged at INFO level

---

### Story 10.4: Handle Excluded Repos in Database

As the **system**,
I want excluded repos to remain in database but be skipped,
So that I can track exclusion history and re-enable repos later.

**Acceptance Criteria:**

**Given** a repo with `excluded=1` in database
**When** collection or export runs
**Then** repo is skipped in collection
**And** repo is skipped in metrics export
**And** repo remains in database with all historical data
**And** `scanner list` shows excluded repos with `[excluded]` marker

---

## Epic 11: LLM Category Classification - Stories

System automatically classifies repositories into CNCF/cloud-native categories using local LLM, enabling operators to filter and route alerts by domain.

### Story 11.1: Load Classification Configuration

As an **operator**,
I want to configure classification settings via YAML,
So that I can customize the LLM behavior without code changes.

**Acceptance Criteria:**

**Given** `config/classification.yaml` exists
**When** classification service starts
**Then** Ollama endpoint is loaded (default: `http://localhost:11434`)
**And** model name is loaded (default: `qwen3:0.6b`)
**And** category list is loaded (18 categories + `other`)
**And** system_prompt and user_prompt templates are loaded
**And** validation errors are logged clearly

---

### Story 11.2: Connect to Ollama LLM Service

As the **system**,
I want to communicate with Ollama for classification,
So that repos can be categorized using local LLM.

**Acceptance Criteria:**

**Given** Ollama is running at configured endpoint
**When** classification is triggered
**Then** system connects to `/api/chat` endpoint
**And** timeout is respected (default: 30s)
**And** connection errors are logged with clear diagnostics
**And** Ollama not running produces actionable error message

---

### Story 11.3: Fetch and Hash README Content

As the **system**,
I want to fetch README and compute hash,
So that I can detect changes and avoid redundant classifications.

**Acceptance Criteria:**

**Given** a repo needs classification
**When** README is fetched
**Then** README content is retrieved from GitHub API
**And** SHA256 hash is computed
**And** content is truncated to 2000 characters
**And** missing README is handled gracefully (use description only)

---

### Story 11.4: Classify Repository with LLM

As the **system**,
I want to send repo data to LLM for classification,
So that repos are assigned to appropriate categories.

**Acceptance Criteria:**

**Given** repo data and README content
**When** classification prompt is sent to Ollama
**Then** prompt includes: repo_name, description, language, topics, README excerpt
**And** LLM response is parsed as JSON: `{category, confidence, reasoning}`
**And** category must be from allowed list or `other`
**And** confidence is 0.0-1.0 float
**And** malformed responses are logged and marked as `other` with low confidence

---

### Story 11.5: Store Classification Results

As the **system**,
I want to persist classification in database,
So that category data is available for queries and export.

**Acceptance Criteria:**

**Given** LLM returns classification
**When** result is stored
**Then** primary_category is saved to repos table
**And** category_confidence is saved
**And** readme_hash is saved
**And** classified_at timestamp is saved
**And** model_used is saved (e.g., "qwen3:0.6b")
**And** status is updated to 'classified'

---

### Story 11.6: Skip Classification When README Unchanged

As the **system**,
I want to skip LLM calls when README hasn't changed,
So that I save resources and avoid redundant API calls.

**Acceptance Criteria:**

**Given** repo has existing readme_hash in database
**When** current README hash matches stored hash
**Then** classification is skipped
**And** existing category is retained
**And** log message indicates "skipped - no README change"

---

### Story 11.7: Trigger Reclassification on README Change

As the **system**,
I want to reclassify when README changes,
So that category stays accurate as projects evolve.

**Acceptance Criteria:**

**Given** repo has existing classification
**When** new README hash differs from stored hash
**Then** status is set to 'needs_reclassify'
**And** repo is included in next classification run
**And** log indicates "README changed - queued for reclassification"

---

### Story 11.8: Implement Classify Command

As an **operator**,
I want to run classification via CLI,
So that I can trigger classification on demand.

**Acceptance Criteria:**

**Given** repos in database with status='pending' or 'needs_reclassify'
**When** `scanner classify` is run
**Then** all pending repos are classified sequentially
**And** progress is logged (X of Y repos)
**And** summary shows: classified, skipped, failed counts
**And** `--dry-run` shows what would be classified without calling LLM

---

### Story 11.9: Implement Classify Test Command

As an **operator**,
I want to test classification on a single repo,
So that I can verify prompts and configuration work correctly.

**Acceptance Criteria:**

**Given** a repo identifier
**When** `scanner classify test <owner/repo>` is run
**Then** repo is fetched (from DB or GitHub if new)
**And** classification is performed and displayed
**And** result shows: category, confidence, reasoning, duration
**And** result is NOT saved to database (test only)

---

## Epic 12: Classification Overrides & Taxonomy Management - Stories

Operator can exclude repos, force category overrides, and manage the category taxonomy via CLI, giving full control over classification behavior.

### Story 12.1: Implement Exclude List Command

As an **operator**,
I want to view all excluded repositories,
So that I can see what's being skipped.

**Acceptance Criteria:**

**Given** repos with `excluded=1` in database
**When** `scanner exclude list` is run
**Then** all excluded repos are displayed in table format
**And** output shows: repo name, excluded date, reason (if stored)
**And** empty list shows "No excluded repositories"
**And** `--format json` outputs JSON for scripting

---

### Story 12.2: Implement Exclude Add Command

As an **operator**,
I want to exclude a repository from tracking,
So that irrelevant repos don't appear in my data.

**Acceptance Criteria:**

**Given** a repo identifier (owner/repo)
**When** `scanner exclude add <owner/repo>` is run
**Then** repo is marked `excluded=1` in database
**And** if repo doesn't exist, placeholder row is created with `excluded=1`
**And** success message confirms exclusion
**And** already-excluded repo shows "already excluded" message

---

### Story 12.3: Implement Exclude Remove Command

As an **operator**,
I want to remove a repository from exclusion list,
So that I can start tracking it again.

**Acceptance Criteria:**

**Given** an excluded repo
**When** `scanner exclude remove <owner/repo>` is run
**Then** repo is marked `excluded=0` in database
**And** repo will be included in next collection
**And** success message confirms removal
**And** non-excluded repo shows "not in exclusion list" message

---

### Story 12.4: Implement Category List-Overrides Command

As an **operator**,
I want to view all manual category overrides,
So that I can see which repos have forced categories.

**Acceptance Criteria:**

**Given** repos with `force_category` set in database
**When** `scanner category list-overrides` is run
**Then** all overridden repos are displayed
**And** output shows: repo name, forced category, override date
**And** empty list shows "No category overrides"

---

### Story 12.5: Implement Category Set Command

As an **operator**,
I want to force a category on a repository,
So that I can correct LLM mistakes or pre-classify repos.

**Acceptance Criteria:**

**Given** a repo and category name
**When** `scanner category set <owner/repo> <category>` is run
**Then** `force_category` is set in database
**And** category must be in allowed taxonomy
**And** invalid category shows error with allowed list
**And** repo is skipped by LLM classification (uses forced category)
**And** if repo doesn't exist, placeholder row is created

---

### Story 12.6: Implement Category Unset Command

As an **operator**,
I want to remove a forced category override,
So that the repo can be classified by LLM again.

**Acceptance Criteria:**

**Given** a repo with `force_category` set
**When** `scanner category unset <owner/repo>` is run
**Then** `force_category` is cleared (set to NULL)
**And** repo status is set to 'needs_reclassify'
**And** success message confirms override removed
**And** repo without override shows "no override exists" message

---

### Story 12.7: Implement Category List Command

As an **operator**,
I want to view the allowed category taxonomy,
So that I know what categories are available.

**Acceptance Criteria:**

**Given** categories defined in configuration
**When** `scanner category list` is run
**Then** all allowed categories are displayed
**And** `other` is always shown (built-in)
**And** count of repos per category is shown if `--counts` flag used

---

### Story 12.8: Implement Category Add Command

As an **operator**,
I want to add a new category to the taxonomy,
So that I can expand classification for new domains.

**Acceptance Criteria:**

**Given** a new category name
**When** `scanner category add <name>` is run
**Then** category is added to configuration file
**And** category name is validated (lowercase, alphanumeric, hyphens)
**And** duplicate category shows error
**And** success message confirms addition
**And** config is reloaded

---

### Story 12.9: Implement Category Remove Command

As an **operator**,
I want to remove a category from the taxonomy,
So that I can clean up unused categories.

**Acceptance Criteria:**

**Given** an existing category name
**When** `scanner category remove <name>` is run
**Then** category is removed from configuration file
**And** `other` cannot be removed (built-in)
**And** repos with that category are set to 'needs_reclassify'
**And** `--force` required if repos exist with that category
**And** success message confirms removal

---

## Epic 13: Self-Monitoring & Telemetry - Stories

Operator can observe the scanner's health, LLM token usage, and resource consumption via their chosen observability backend.

### Story 13.1: Configure Telemetry Settings

As an **operator**,
I want to configure self-monitoring via YAML,
So that I can enable/disable telemetry and set my backend.

**Acceptance Criteria:**

**Given** `config/telemetry.yaml` exists
**When** scanner starts
**Then** telemetry enabled/disabled flag is loaded
**And** host_metrics collection interval is configurable
**And** llm_metrics enabled flag is loaded
**And** disabled telemetry skips all monitoring setup

---

### Story 13.2: Deploy OTel Collector Sidecar Config

As an **operator**,
I want a pre-configured OTel Collector config,
So that I can collect scanner metrics without manual setup.

**Acceptance Criteria:**

**Given** telemetry is enabled
**When** scanner package is deployed
**Then** `otel-collector-config.yaml` is provided
**And** config includes hostmetrics receiver
**And** config includes OTLP receiver for app metrics
**And** config includes batch processor and resourcedetection
**And** exporters section references user's `collector-exporters.yaml`

---

### Story 13.3: Collect Host Metrics

As the **system**,
I want to collect VM resource metrics,
So that operators can monitor scanner infrastructure health.

**Acceptance Criteria:**

**Given** OTel Collector is running with hostmetrics receiver
**When** collection interval elapses (default: 60s)
**Then** `system.cpu.utilization` is collected
**And** `system.memory.usage` is collected
**And** `system.disk.io` is collected
**And** `process.cpu.utilization` for scanner process is collected
**And** `process.memory.usage` for scanner process is collected

---

### Story 13.4: Instrument LLM Calls with OpenLLMetry

As the **system**,
I want to capture LLM telemetry,
So that operators can track classification performance and costs.

**Acceptance Criteria:**

**Given** classification calls Ollama
**When** LLM request completes
**Then** `llm.request.duration` histogram is recorded
**And** `llm.tokens.prompt` counter is recorded
**And** `llm.tokens.completion` counter is recorded
**And** `llm.request.success` counter tracks success/failure
**And** attributes include: model, repo, category

---

### Story 13.5: Create LLM Request Spans

As the **system**,
I want distributed traces for classification,
So that operators can debug slow or failed classifications.

**Acceptance Criteria:**

**Given** classification is running
**When** each repo is classified
**Then** a span is created for the classification
**And** span includes attributes: repo, model, category, confidence
**And** span records duration
**And** errors are recorded on span
**And** spans link to parent scan span

---

### Story 13.6: Configure User Exporters

As an **operator**,
I want to define my own OTel exporter backend,
So that metrics go to my preferred observability platform.

**Acceptance Criteria:**

**Given** `config/collector-exporters.yaml` exists
**When** OTel Collector starts
**Then** user-defined exporters are loaded
**And** pipeline routing respects user config
**And** multiple exporters are supported (primary + backup)
**And** example configs provided for: Dynatrace, Grafana, Jaeger, Prometheus

---

### Story 13.7: Implement Telemetry Status Command

As an **operator**,
I want to check telemetry status,
So that I know if monitoring is working.

**Acceptance Criteria:**

**Given** scanner is configured
**When** `scanner telemetry status` is run
**Then** shows: enabled/disabled status
**And** shows: configured exporters
**And** shows: last successful export timestamp
**And** shows: collector health if running

---

### Story 13.8: Implement Telemetry Enable/Disable Commands

As an **operator**,
I want to toggle telemetry via CLI,
So that I can quickly enable/disable monitoring.

**Acceptance Criteria:**

**Given** telemetry config exists
**When** `scanner telemetry enable` is run
**Then** telemetry.enabled is set to true in config
**And** config is reloaded
**When** `scanner telemetry disable` is run
**Then** telemetry.enabled is set to false in config
**And** config is reloaded

---

### Story 13.9: Implement Telemetry Exporters Command

As an **operator**,
I want to view and edit exporter configuration,
So that I can manage where metrics are sent.

**Acceptance Criteria:**

**Given** exporter config exists
**When** `scanner telemetry exporters` is run
**Then** current exporter config is displayed
**When** `scanner telemetry exporters --edit` is run
**Then** config is opened in $EDITOR
**And** config is validated after save
**And** invalid config shows error and is not applied

---

### Story 13.10: Implement Telemetry Test Command

As an **operator**,
I want to test backend connectivity,
So that I can verify my exporter config works.

**Acceptance Criteria:**

**Given** exporter config exists
**When** `scanner telemetry test` is run
**Then** test metrics are sent to each configured exporter
**And** success/failure is reported per exporter
**And** connection errors show clear diagnostics
**And** authentication errors are clearly identified

---

## Epic 14: Model Management & Benchmarking - Stories

Operator can switch between LLM models (accuracy vs speed tradeoff) and benchmark classification performance to make informed decisions.

### Story 14.1: Implement Model Show Command

As an **operator**,
I want to see the current classification model,
So that I know what's being used.

**Acceptance Criteria:**

**Given** classification is configured
**When** `scanner classify model` is run
**Then** current model name is displayed (e.g., "qwen3:0.6b")
**And** model tier is shown (MVP/Production/High Accuracy)
**And** estimated accuracy and resource requirements are shown

---

### Story 14.2: Implement Model Switch Command

As an **operator**,
I want to switch to a different LLM model,
So that I can balance accuracy vs speed.

**Acceptance Criteria:**

**Given** a model name
**When** `scanner classify model <name>` is run
**Then** model is validated against allowed list (qwen3:0.6b, qwen3:1.7b, qwen3:4b)
**And** if model not local, `ollama pull` is triggered
**And** config file is updated with new model
**And** all classified repos are marked `status='needs_reclassify'`
**And** count of repos queued for reclassification is shown

---

### Story 14.3: Implement Model List Command

As an **operator**,
I want to see available models,
So that I know my options.

**Acceptance Criteria:**

**Given** scanner is configured
**When** `scanner classify model --list` is run
**Then** all supported models are displayed with:
**And** model name, estimated accuracy, RAM requirement, speed rating
**And** currently active model is marked
**And** locally available models are indicated

---

### Story 14.4: Implement Benchmark Sample Command

As an **operator**,
I want to benchmark classification on a sample,
So that I can measure model performance.

**Acceptance Criteria:**

**Given** repos exist in database
**When** `scanner benchmark --sample 50` is run
**Then** random sample of N repos is selected
**And** each repo is classified (or re-classified)
**And** timing, token usage, and confidence are recorded
**And** summary statistics are displayed:
  - Average duration
  - Average confidence
  - Category distribution
  - Low confidence count

---

### Story 14.5: Implement Benchmark Repos Command

As an **operator**,
I want to benchmark specific repos,
So that I can test classification on known repos.

**Acceptance Criteria:**

**Given** specific repo identifiers
**When** `scanner benchmark --repos owner/repo1,owner/repo2` is run
**Then** specified repos are classified
**And** results are displayed per repo
**And** summary includes same metrics as sample benchmark

---

### Story 14.6: Implement Benchmark Compare Command

As an **operator**,
I want to compare models side-by-side,
So that I can make informed model choices.

**Acceptance Criteria:**

**Given** multiple model names and sample size
**When** `scanner benchmark --compare qwen3:0.6b,qwen3:1.7b --sample 30` is run
**Then** same repos are classified with each model
**And** results are displayed in comparison table:
  - Model | Avg Duration | Avg Confidence | Accuracy Match
**And** category agreement between models is shown
**And** recommendation is provided based on results

---

### Story 14.7: Store Benchmark Results

As the **system**,
I want to persist benchmark results,
So that operators can review historical performance.

**Acceptance Criteria:**

**Given** benchmark completes
**When** results are generated
**Then** JSON file is saved to `_bmad-output/benchmarks/benchmark-{timestamp}.json`
**And** file includes: timestamp, model, sample_size, per-repo results, summary
**And** `--no-save` flag skips file creation

---

### Story 14.8: Display Benchmark Report

As an **operator**,
I want a clear benchmark report,
So that I can quickly understand results.

**Acceptance Criteria:**

**Given** benchmark completes
**When** results are displayed
**Then** report shows:
```
Model: qwen3:0.6b
─────────────────────────────
Repos tested:       50
Avg duration:       1.2s
Avg tokens:         312
Category distribution:
  kubernetes:       12 (24%)
  observability:    8  (16%)
  other:            6  (12%)  ← review these
Confidence:
  High (>0.8):      32 (64%)
  Medium (0.5-0.8): 14 (28%)
  Low (<0.5):       4  (8%)
```
**And** low-confidence repos are listed for review

---

