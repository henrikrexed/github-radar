---
stepsCompleted: [1, 2, 3]
inputDocuments:
  - prd.md
  - architecture.md
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

