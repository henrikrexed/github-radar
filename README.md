# GitHub Radar

Open Source Growth Intelligence - Detect trending GitHub projects before they go viral.

## Overview

GitHub Radar is a CLI tool that tracks and analyzes GitHub repositories to identify projects with unusual growth patterns. It exports metrics via OpenTelemetry to observability platforms like Dynatrace, enabling dashboards and alerting on emerging trends.

## Features

- **Trend-based Discovery**: Automatically detect repositories showing unusual growth
- **Growth Scoring**: Calculate velocity and acceleration metrics for stars, contributors, and activity
- **OpenTelemetry Export**: Export all metrics via OTLP HTTP to any compatible backend
- **Background Daemon**: Run continuous scheduled monitoring
- **CLI Management**: Add, remove, and list tracked repositories

## Quick Start

```bash
# Build
make build

# Run with config
./bin/github-radar --config ./configs/github-radar.example.yaml

# Or with Docker
docker run -v /path/to/config.yaml:/etc/github-radar/config.yaml github-radar
```

## Configuration

Copy `configs/github-radar.example.yaml` to `config.yaml` and configure:

- `GITHUB_TOKEN`: GitHub API token (set via environment variable)
- `otel.endpoint`: OTLP HTTP endpoint for metrics export
- `discovery`: Auto-discovery settings (min stars, max age, etc.)

## Development

```bash
# Run tests
make test

# Run linter
make lint

# Build binary
make build

# Build Docker image
make docker
```

## License

MIT
