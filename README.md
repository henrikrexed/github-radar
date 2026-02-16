# GitHub Radar

[![CI](https://github.com/henrikrexed/github-radar/actions/workflows/ci.yml/badge.svg)](https://github.com/henrikrexed/github-radar/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/henrikrexed/github-radar)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**Open Source Growth Intelligence** — Detect trending GitHub projects before they go viral.

GitHub Radar scans GitHub for repositories showing unusual growth patterns (star velocity, contributor acceleration, PR activity) and exports metrics via OpenTelemetry to observability platforms like Dynatrace. Content creators, DevRel teams, and OSS investors use it to discover emerging projects before they hit social media.

## Key Features

- **Trend-Based Discovery** — Automatically find repositories with unusual growth across configurable topics
- **Growth Scoring** — Weighted composite score from star velocity, star acceleration, contributor growth, PR velocity, and issue velocity
- **OpenTelemetry Export** — OTLP HTTP metrics to any compatible backend (Dynatrace, Grafana, Prometheus)
- **Background Daemon** — Scheduled scanning with health/status HTTP endpoints
- **CLI Management** — Add, remove, list, discover, and exclude repositories
- **Cross-Platform** — Native binaries for Linux, macOS, and Windows

## Prerequisites

- **GitHub Personal Access Token** (see [Token Setup](#github-token-setup) below)
- **OTel Collector or OTLP endpoint** for metrics export (optional for dry-run mode)

## Installation

### Download Binary

Download the latest release for your platform from [GitHub Releases](https://github.com/henrikrexed/github-radar/releases):

```bash
# Linux (amd64)
curl -Lo github-radar https://github.com/henrikrexed/github-radar/releases/latest/download/github-radar-linux-amd64
chmod +x github-radar
sudo mv github-radar /usr/local/bin/

# macOS (Apple Silicon)
curl -Lo github-radar https://github.com/henrikrexed/github-radar/releases/latest/download/github-radar-darwin-arm64
chmod +x github-radar
sudo mv github-radar /usr/local/bin/

# macOS (Intel)
curl -Lo github-radar https://github.com/henrikrexed/github-radar/releases/latest/download/github-radar-darwin-amd64
chmod +x github-radar
sudo mv github-radar /usr/local/bin/
```

### Build from Source

Requires Go 1.22+:

```bash
git clone https://github.com/henrikrexed/github-radar.git
cd github-radar
make build
# Binary is at ./bin/github-radar
```

### Docker

```bash
docker pull ghcr.io/henrikrexed/github-radar:latest

# Or build locally
make docker
```

## GitHub Token Setup

GitHub Radar requires a Personal Access Token to access the GitHub API (5,000 requests/hour authenticated vs 60/hour without).

### Creating a Classic Token

1. Go to [github.com/settings/tokens](https://github.com/settings/tokens)
2. Click **"Generate new token"** → **"Generate new token (classic)"**
3. Set a descriptive name (e.g., "GitHub Radar")
4. Select scopes:
   - **`public_repo`** — Required: access public repository data
   - **`read:org`** — Optional: access organization repository data
5. Click **"Generate token"**
6. Copy the token immediately (it won't be shown again)

### Creating a Fine-Grained Token

1. Go to [github.com/settings/tokens?type=beta](https://github.com/settings/tokens?type=beta)
2. Click **"Generate new token"**
3. Set token name and expiration
4. Under **Repository access**, select **"Public Repositories (read-only)"**
5. Under **Permissions**, grant:
   - **Repository permissions → Metadata** — Read-only
   - **Repository permissions → Issues** — Read-only
   - **Repository permissions → Pull requests** — Read-only
6. Click **"Generate token"**

### Setting the Token

```bash
# Set as environment variable (recommended)
export GITHUB_TOKEN="ghp_your_token_here"

# Or add to your shell profile (~/.bashrc, ~/.zshrc)
echo 'export GITHUB_TOKEN="ghp_your_token_here"' >> ~/.zshrc
```

> **Security**: Never commit tokens to source control. Always use environment variables or a secrets manager.

## Quick Start

```bash
# 1. Set your GitHub token
export GITHUB_TOKEN="ghp_your_token_here"

# 2. Create a config file
cp configs/github-radar.example.yaml config.yaml

# 3. Discover trending repositories (dry-run, no OTel needed)
github-radar discover --config config.yaml --topics kubernetes,ebpf --dry-run

# 4. Run a full scan with metrics export
github-radar collect --config config.yaml

# 5. Or start the background daemon
github-radar serve --config config.yaml --interval 6h
```

## CLI Commands

### `github-radar collect`

Run a full collection cycle: scan all tracked repositories, calculate growth scores, and export metrics.

```bash
github-radar collect --config config.yaml
github-radar collect --config config.yaml --dry-run      # Collect without exporting metrics
github-radar collect --config config.yaml --verbose       # Enable debug logging
```

### `github-radar discover`

Discover trending repositories by topic from GitHub Search API.

```bash
# Discover using topics from config
github-radar discover --config config.yaml

# Discover specific topics
github-radar discover --config config.yaml --topics kubernetes,ebpf,wasm

# Discover and automatically track high-scoring repos
github-radar discover --config config.yaml --auto-track

# Customize filters
github-radar discover --config config.yaml --min-stars 500 --max-age 30

# Output as JSON for scripting
github-radar discover --config config.yaml --format json
```

### `github-radar add`

Add a repository to tracking.

```bash
github-radar add kubernetes/kubernetes --config config.yaml
github-radar add kubernetes/kubernetes --category cncf --config config.yaml
```

### `github-radar remove`

Remove a repository from tracking.

```bash
github-radar remove kubernetes/kubernetes --config config.yaml
github-radar remove kubernetes/kubernetes --keep-state --config config.yaml
```

### `github-radar list`

List all tracked repositories with their current scores.

```bash
github-radar list --config config.yaml
github-radar list --config config.yaml --category cncf
github-radar list --config config.yaml --format json
github-radar list --config config.yaml --format csv
```

### `github-radar exclude`

Manage the exclusion list (excluded repos are never scanned or auto-tracked).

```bash
github-radar exclude list --config config.yaml
github-radar exclude add example-org/spam-repo --config config.yaml
github-radar exclude add example-org/* --config config.yaml     # Wildcard: entire org
github-radar exclude remove example-org/spam-repo --config config.yaml
```

### `github-radar serve`

Start the background daemon for scheduled scanning.

```bash
github-radar serve --config config.yaml
github-radar serve --config config.yaml --interval 6h           # Scan every 6 hours
github-radar serve --config config.yaml --http-addr :9090       # Custom health endpoint port
github-radar serve --config config.yaml --state ./data/state.json
```

The daemon exposes HTTP endpoints:
- `GET /health` — Health check (`{"healthy": true}`)
- `GET /status` — Daemon status (scan state, repos tracked, rate limit remaining)

Send `SIGHUP` to reload configuration without restarting.

### `github-radar status`

Check the daemon's status (requires a running daemon).

```bash
github-radar status
github-radar status --addr http://localhost:9090
github-radar status --format json
```

### `github-radar config`

Validate or display configuration.

```bash
github-radar config validate --config config.yaml
github-radar config show --config config.yaml
```

### Global Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--config <path>` | Path to configuration file | `./github-radar.yaml` |
| `--verbose` | Enable debug logging | `false` |
| `--dry-run` | Collect data but don't export metrics | `false` |

Environment variables: `GITHUB_RADAR_CONFIG` (config path), `GITHUB_RADAR_STATE` (state path).

## Configuration

Create a configuration file from the example:

```bash
cp configs/github-radar.example.yaml config.yaml
```

### Full Configuration Reference

```yaml
# GitHub API settings
github:
  token: ${GITHUB_TOKEN}         # Required. Use env var substitution.
  rate_limit: 4000               # Max API requests per hour (default: 4000, GitHub max: 5000)

# OpenTelemetry metrics export
otel:
  endpoint: http://localhost:4318  # Required. OTLP HTTP endpoint.
  headers:                         # Optional. Custom headers for authentication.
    Authorization: "Api-Token ${DT_API_TOKEN}"
  service_name: github-radar       # OTel service.name attribute (default: github-radar)
  service_version: ""              # OTel service.version attribute
  flush_timeout: 10                # Seconds to wait for metric flush on exit (default: 10)
  attributes: {}                   # Additional OTel resource attributes

# Repository discovery settings
discovery:
  enabled: true                    # Enable/disable auto-discovery (default: true)
  topics:                          # Topics to search. Empty = use GitHub trending.
    - kubernetes
    - opentelemetry
    - ebpf
  min_stars: 100                   # Minimum star count filter (default: 100)
  max_age_days: 90                 # Max repo age in days, 0 = no limit (default: 90)
  auto_track_threshold: 50.0       # Auto-track repos above this growth score (default: 50)

# Growth scoring weights
scoring:
  weights:
    star_velocity: 2.0             # Stars gained per day (default: 2.0)
    star_acceleration: 3.0         # Velocity change from previous period (default: 3.0)
    contributor_growth: 1.5        # New contributors per day (default: 1.5)
    pr_velocity: 1.0               # PRs merged per day (default: 1.0)
    issue_velocity: 0.5            # New issues per day (default: 0.5)

# Repositories to exclude from scanning
exclusions:
  - example-org/spam-repo          # Exact match
  # - example-org/*                # Wildcard: entire org

# Manually tracked repositories
repositories:
  - repo: kubernetes/kubernetes
    categories:
      - cncf
      - container-orchestration
  - repo: opentelemetry/opentelemetry-go   # No categories = "default"
```

### Environment Variable Substitution

Config values support `${VAR}` syntax for environment variable expansion:

```yaml
github:
  token: ${GITHUB_TOKEN}                    # Required - fails if not set
otel:
  endpoint: ${OTEL_ENDPOINT:-http://localhost:4318}  # Optional with default
```

## Daemon Setup

### Running as a systemd Service

Create `/etc/systemd/system/github-radar.service`:

```ini
[Unit]
Description=GitHub Radar - Open Source Growth Intelligence
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=github-radar
ExecStart=/usr/local/bin/github-radar serve \
    --config /etc/github-radar/config.yaml \
    --state /var/lib/github-radar/state.json \
    --interval 6h
Restart=on-failure
RestartSec=30
Environment=GITHUB_TOKEN=ghp_your_token_here

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable github-radar
sudo systemctl start github-radar
sudo systemctl status github-radar

# Reload config without restart
sudo systemctl reload github-radar   # Sends SIGHUP
```

### Running with Docker Compose

```bash
# Set required environment variables
export GITHUB_TOKEN="ghp_your_token_here"
export DT_API_TOKEN="dt0c01.your_token"      # If using Dynatrace

# Create config
cp configs/github-radar.example.yaml configs/config.yaml
# Edit configs/config.yaml with your settings

# Start
docker-compose up -d

# Check health
curl http://localhost:8080/health

# Check status
curl http://localhost:8080/status

# View logs
docker-compose logs -f github-radar

# Stop
docker-compose down
```

### Cron Alternative

If you prefer one-shot scans over a daemon:

```bash
# Weekly scan every Monday at 6 AM
0 6 * * 1 GITHUB_TOKEN=ghp_xxx /usr/local/bin/github-radar collect --config /etc/github-radar/config.yaml
```

## OpenTelemetry Integration

GitHub Radar exports metrics via OTLP HTTP. Metrics use the `github.repo.*` namespace with dimensions for `repo_owner`, `repo_name`, `language`, and `category`.

### Exported Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `github.repo.stars` | Gauge | Current star count |
| `github.repo.forks` | Gauge | Current fork count |
| `github.repo.open_issues` | Gauge | Open issue count |
| `github.repo.open_prs` | Gauge | Open PR count |
| `github.repo.contributors` | Gauge | Contributor count |
| `github.repo.star_velocity` | Gauge | Stars gained per day |
| `github.repo.star_acceleration` | Gauge | Velocity change from previous period |
| `github.repo.pr_velocity` | Gauge | PRs merged per day |
| `github.repo.issue_velocity` | Gauge | New issues per day |
| `github.repo.contributor_growth` | Gauge | New contributors per day |
| `github.repo.growth_score` | Gauge | Composite growth score |
| `github.repo.normalized_growth_score` | Gauge | Normalized score (0-100) |

### Dynatrace Setup

```yaml
otel:
  endpoint: https://{your-env}.live.dynatrace.com/api/v2/otlp/v1/metrics
  headers:
    Authorization: "Api-Token ${DT_API_TOKEN}"
```

The `DT_API_TOKEN` needs the **Ingest metrics** (`metrics.ingest`) scope.

## Development

```bash
make build          # Build binary
make test           # Run tests
make test-v         # Run tests (verbose)
make test-coverage  # Run tests with coverage report
make lint           # Run go vet + staticcheck
make fmt            # Format code
make fmt-check      # Check formatting
make release        # Cross-compile for all platforms
make docker         # Build Docker image
make help           # Show all available targets
```

### Project Structure

```
github-radar/
├── cmd/github-radar/     # CLI entry point
├── internal/
│   ├── cli/              # Command implementations
│   ├── config/           # Configuration loading & validation
│   ├── daemon/           # Background daemon
│   ├── discovery/        # Topic-based repository discovery
│   ├── github/           # GitHub API client & scanner
│   ├── logging/          # Structured logging
│   ├── metrics/          # OTel metrics export
│   ├── repository/       # Repository management
│   ├── scoring/          # Growth scoring algorithm
│   └── state/            # JSON state persistence
├── configs/              # Example configuration files
├── docs/                 # MkDocs documentation source
├── Dockerfile
├── docker-compose.yml
├── Makefile
└── mkdocs.yml
```

## License

MIT
