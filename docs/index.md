# GitHub Radar

**Open Source Growth Intelligence** — Detect trending GitHub projects before they go viral.

GitHub Radar scans GitHub for repositories showing unusual growth patterns and exports metrics via OpenTelemetry to observability platforms like Dynatrace. Content creators, DevRel teams, and OSS investors use it to discover emerging projects before they hit social media.

## How It Works

1. **Discover** — GitHub Radar queries the GitHub Search API for repositories matching configured topics (e.g., `kubernetes`, `ebpf`, `wasm`)
2. **Collect** — For each tracked repository, it collects stars, forks, PRs, issues, contributors, and release data
3. **Score** — A weighted growth score is computed from star velocity, acceleration, contributor growth, and activity metrics
4. **Export** — All metrics are exported via OTLP HTTP to your observability backend
5. **Alert** — Your observability platform (Dynatrace, Grafana, etc.) handles dashboards and alerting

## Quick Start

```bash
# 1. Set your GitHub token
export GITHUB_TOKEN="ghp_your_token_here"

# 2. Create a config file
cp configs/github-radar.example.yaml config.yaml

# 3. Discover trending repos (no OTel endpoint needed)
github-radar discover --config config.yaml --topics kubernetes,ebpf --dry-run

# 4. Start the background daemon
github-radar serve --config config.yaml --interval 6h
```

See [Installation](installation.md) for download and setup instructions.

## Features

| Feature | Description |
|---------|-------------|
| **Trend-Based Discovery** | Automatically find repos with unusual growth across configurable topics |
| **Growth Scoring** | Weighted composite score from star velocity, acceleration, contributor growth, PR and issue velocity |
| **OpenTelemetry Export** | OTLP HTTP metrics to Dynatrace, Grafana, Prometheus, or any OTel-compatible backend |
| **Background Daemon** | Scheduled scanning with HTTP health/status endpoints |
| **CLI Management** | Add, remove, list, discover, and exclude repositories |
| **Cross-Platform** | Native binaries for Linux (amd64/arm64), macOS (Intel/Apple Silicon), and Windows |
| **Docker Support** | Multi-stage Docker build with health checks, runs as non-root user |

## Use Cases

- **Content Creators** — Find emerging projects to cover before other creators
- **DevRel Teams** — Track competitor project growth and ecosystem trends
- **OSS Investors** — Identify projects with accelerating growth for investment timing
- **Tech Journalists** — Monitor for story leads based on sudden growth signals
