# Configuration Reference

GitHub Radar is configured via a YAML file. Create one from the example:

```bash
cp configs/github-radar.example.yaml config.yaml
```

## Config File Location

The config file is resolved in this order:

1. `--config <path>` CLI flag
2. `GITHUB_RADAR_CONFIG` environment variable
3. `./github-radar.yaml` (default)

## Full Schema

```yaml
# GitHub API settings
github:
  token: ${GITHUB_TOKEN}         # Required. GitHub Personal Access Token.
  rate_limit: 4000               # Max API requests per hour (default: 4000, max: 5000)

# OpenTelemetry metrics export
otel:
  endpoint: http://localhost:4318  # Required. OTLP HTTP endpoint URL.
  headers:                         # Optional. Custom HTTP headers for authentication.
    Authorization: "Api-Token ${DT_API_TOKEN}"
  service_name: github-radar       # OTel service.name resource attribute (default: github-radar)
  service_version: ""              # OTel service.version resource attribute
  flush_timeout: 10                # Seconds to wait for metrics flush on exit (default: 10)
  attributes: {}                   # Additional OTel resource attributes (key-value map)

# Repository discovery settings
discovery:
  enabled: true                    # Enable/disable auto-discovery (default: true)
  topics:                          # Topics to search on GitHub
    - kubernetes
    - opentelemetry
    - ebpf
  min_stars: 100                   # Minimum star count to include (default: 100)
  max_age_days: 90                 # Max repo age in days, 0 = no limit (default: 90)
  auto_track_threshold: 50.0       # Auto-track repos scoring above this (default: 50.0)

# Growth scoring formula weights
scoring:
  weights:
    star_velocity: 2.0             # Weight for stars gained per day (default: 2.0)
    star_acceleration: 3.0         # Weight for velocity change (default: 3.0)
    contributor_growth: 1.5        # Weight for new contributors per day (default: 1.5)
    pr_velocity: 1.0               # Weight for PRs merged per day (default: 1.0)
    issue_velocity: 0.5            # Weight for new issues per day (default: 0.5)

# LLM-based category classification (requires Ollama)
classification:
  ollama_endpoint: "http://localhost:11434"  # Ollama API endpoint URL
  model: "qwen3:1.7b"                       # Ollama model name for classification
  timeout_ms: 30000                          # Request timeout in milliseconds (default: 30000)
  max_readme_chars: 2000                     # Max README characters sent to LLM (default: 2000)
  min_confidence: 0.6                        # Confidence threshold (0.0–1.0). Below → needs_review
  categories:                                # CNCF/cloud-native categories (19 + "other")
    - ai-agents
    - llm-tooling
    - kubernetes
    - observability
    - cloud-native-security
    - networking
    - service-mesh
    - platform-engineering
    - gitops
    - mlops
    - vector-database
    - rag
    - wasm
    - developer-tools
    - infrastructure
    - data-engineering
    - testing
    - container-runtime
    - other
  system_prompt: |                           # System prompt template ({{.Categories}} is replaced)
    You are a GitHub repository classifier for CNCF and cloud-native projects.
    Classify into exactly ONE category from: {{.Categories}}
    If unclear, use "other".
    Respond ONLY with JSON: {"category": "<name>", "confidence": <0.0-1.0>, "reasoning": "<one sentence>"}
  user_prompt: |                             # User prompt template (see template variables below)
    Repository: {{.RepoName}}
    Description: {{.Description}}
    Language: {{.Language}}
    Topics: {{.Topics}}
    Stars: {{.Stars}} (trend: {{.StarTrend}})
    README excerpt:
    {{.Readme}}

# Collector configuration — gharchive.org rate-limit fallback (ISI-815)
# When enabled, the router switches from live GitHub API to gharchive.org
# hourly archive downloads when API budget headroom drops below the threshold.
collector:
  gharchive:
    enabled: false                              # default: false — zero behavior change when disabled
    base_url: https://data.gharchive.org        # gharchive.org hourly archive base URL
    http_timeout: 60s                           # per-hour-file download timeout
  fallback_threshold_pct: 0.25                  # trip fallback when remaining/limit < 0.25

# Repositories to exclude from scanning
exclusions:
  - example-org/spam-repo          # Exact match: owner/repo
  # - example-org/*                # Wildcard: entire organization

# Manually tracked repositories
repositories:
  - repo: kubernetes/kubernetes    # Required: owner/repo format
    categories:                    # Optional: defaults to ["default"]
      - cncf
      - container-orchestration
  - repo: opentelemetry/opentelemetry-go  # No categories = "default"
```

## Environment Variable Substitution

Config values support `${VAR}` syntax:

| Syntax | Behavior |
|--------|----------|
| `${VAR}` | Required — fails with error if `VAR` is not set |
| `${VAR:-default}` | Optional — uses `default` if `VAR` is not set |
| `$${VAR}` | Escaped — produces literal `${VAR}` |

Example:

```yaml
github:
  token: ${GITHUB_TOKEN}                              # Required
otel:
  endpoint: ${OTEL_ENDPOINT:-http://localhost:4318}    # Optional with default
```

## Common Environment Variables

| Variable | Purpose |
|----------|---------|
| `GITHUB_TOKEN` | GitHub API authentication token |
| `DT_API_TOKEN` | Dynatrace API token (for OTLP auth header) |
| `GITHUB_RADAR_CONFIG` | Default config file path |
| `GITHUB_RADAR_STATE` | Default state file path |
| `OTEL_ENDPOINT` | OTLP HTTP endpoint URL |
| `OLLAMA_ENDPOINT` | Ollama API endpoint for classification |

## Validation

Validate your configuration before running:

```bash
github-radar config validate --config config.yaml
```

This checks:

- Required fields (`github.token`, `otel.endpoint`) are present
- Environment variables referenced in `${VAR}` are set
- URL formats are valid (OTLP endpoint)
- Numeric values are in range (rate_limit > 0, weights >= 0)
- Repository identifiers are in `owner/repo` format

## Display Configuration

View the resolved configuration (secrets are masked):

```bash
github-radar config show --config config.yaml
```

## Growth Score Formula

The composite growth score is calculated as:

```
growth_score = (star_velocity     × weight.star_velocity)
             + (star_acceleration × weight.star_acceleration)
             + (contributor_growth × weight.contributor_growth)
             + (pr_velocity       × weight.pr_velocity)
             + (issue_velocity    × weight.issue_velocity)
```

Scores are then normalized to a 0-100 scale across all tracked repositories.

### Tuning Weights

- Increase `star_acceleration` to prioritize repos with **accelerating** growth
- Increase `contributor_growth` to prioritize repos attracting **new developers**
- Increase `pr_velocity` to prioritize repos with **active development**
- Set `issue_velocity` lower since high issues can indicate problems, not just popularity

## Classification Configuration

The `classification` section configures LLM-based category classification using [Ollama](https://ollama.com). See the [Classification Guide](classification.md) for full usage details.

### Prompt Template Variables

The `user_prompt` template supports Go template syntax with these variables:

| Variable | Description |
|----------|-------------|
| `{{.RepoName}}` | Full repository name (`owner/repo`) |
| `{{.Description}}` | Repository description from GitHub |
| `{{.Language}}` | Primary programming language |
| `{{.Topics}}` | Comma-separated GitHub topics |
| `{{.Stars}}` | Current star count |
| `{{.StarTrend}}` | Star growth trend (e.g., `rising`, `stable`, `unknown`) |
| `{{.Readme}}` | Truncated README content (up to `max_readme_chars`) |

The `system_prompt` template supports:

| Variable | Description |
|----------|-------------|
| `{{.Categories}}` | Comma-separated list of configured categories |

### Confidence Threshold

Repositories classified with a confidence below `min_confidence` are marked as `needs_review` instead of being auto-assigned a category. Adjust this value based on your tolerance for misclassification:

- **0.8+** — Strict: only high-confidence classifications are accepted
- **0.6** — Balanced (default): reasonable accuracy with fewer manual reviews
- **0.4** — Lenient: accepts most classifications, review only very uncertain ones

### Reclassification Triggers

Classification is automatically re-triggered when:

- A repository's README content changes (detected via SHA-256 hash comparison)
- The classification model is changed via `github-radar classify model <name>`
- A repository has never been classified

## Collector Configuration

The `collector` section configures the data collection backends. When gharchive.org fallback is enabled, GitHub Radar automatically switches from the live GitHub API to downloading hourly event archives when API budget headroom runs low.

### How It Works

1. Every scan cycle, the router checks `remaining / limit` from GitHub's `X-RateLimit-*` response headers
2. If headroom drops below `fallback_threshold_pct` (default **25%**), the router switches to gharchive.org for that cycle only
3. Next cycle, it re-evaluates — no sticky state. If budget recovered, it returns to the live API
4. gharchive.org downloads hourly `.json.gz` files, streams gzip + JSON decode, filters events for tracked repos only

### Configuration Keys

```yaml
collector:
  gharchive:
    enabled: false                              # Enable gharchive.org fallback (default: false)
    base_url: https://data.gharchive.org        # Archive base URL
    http_timeout: 60s                           # Download timeout per hourly file
  fallback_threshold_pct: 0.25                  # Trip when remaining < 25% of limit
```

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `collector.gharchive.enabled` | bool | `false` | Enable the gharchive.org fallback. When `false`, the router is nil and behavior is identical to versions without this feature. |
| `collector.gharchive.base_url` | string | `https://data.gharchive.org` | Base URL for hourly archive files. Files are fetched as `{base_url}/YYYY-MM-DD-HH.json.gz`. |
| `collector.gharchive.http_timeout` | duration | `60s` | HTTP timeout for each hourly archive download. Each hour of data is a separate HTTP request. |
| `collector.fallback_threshold_pct` | float64 | `0.25` | Fraction of API budget remaining below which the router trips to gharchive. Must be between 0 and 1. |

### When to Enable

Enable gharchive fallback when:

- Tracking **500+ repositories** — the GitHub API budget (5,000 requests/hour) becomes tight at scale
- Running **short scan intervals** (1-4 hours) — more frequent scans consume budget faster
- Experiencing **rate limit errors** in production — the fallback provides a zero-cost safety net

The fallback fires approximately 25% of cycles in the worst case. Since gharchive.org requires no authentication, no API keys, and has no rate limit, the only cost is network bandwidth.

### What gharchive.org Provides

[gharchive.org](https://www.gharchive.org/) publishes hourly archive files of all public GitHub events:

- One file per hour, named `YYYY-MM-DD-HH.json.gz`
- Available within minutes after each hour ends
- Covers all public repositories going back to 2011
- No authentication required

The `HourlyArchiveCollector` downloads each hourly file within the scan window, streams the gzip decompression and JSON decode (never loads entire files into memory), and filters for tracked repositories. Over 99% of events are discarded — only events matching tracked repos are kept.

### Signal Differences

gharchive.org provides event-based data (star events, fork events, PR events, issue events, release events) rather than exact API counts. Velocity signals stay within +/-5% of live API values across all scoring weights:

- `star_velocity`, `star_acceleration`, `fork_velocity`, `release_cadence`
- `contributor_growth`, `pr_velocity`, `issue_velocity`, `merged_prs_7d`

Absolute counts (total stars, total forks, contributor count) are not available from gharchive — only event-level deltas. The live API remains the source of truth for those metrics.
