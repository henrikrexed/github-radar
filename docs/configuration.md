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
