# OpenTelemetry Integration

GitHub Radar exports all metrics via OTLP HTTP to any OpenTelemetry-compatible backend.

## Configuration

```yaml
otel:
  endpoint: http://localhost:4318
  headers:
    Authorization: "Api-Token ${DT_API_TOKEN}"
  service_name: github-radar
  service_version: "1.0.0"
  flush_timeout: 10
  attributes:
    deployment.environment: production
```

## Exported Metrics

All metrics use the `github.repo.*` namespace.

### Repository Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `github.repo.stars` | Gauge | Current star count |
| `github.repo.forks` | Gauge | Current fork count |
| `github.repo.open_issues` | Gauge | Open issue count |
| `github.repo.open_prs` | Gauge | Open PR count |
| `github.repo.contributors` | Gauge | Total contributor count |
| `github.repo.merged_prs_7d` | Gauge | PRs merged in the last 7 days |
| `github.repo.new_issues_7d` | Gauge | Issues opened in the last 7 days |

### Growth Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `github.repo.star_velocity` | Gauge | Stars gained per day |
| `github.repo.star_acceleration` | Gauge | Velocity change from previous period |
| `github.repo.pr_velocity` | Gauge | PRs merged per day |
| `github.repo.issue_velocity` | Gauge | New issues per day |
| `github.repo.contributor_growth` | Gauge | New contributors per day |
| `github.repo.growth_score` | Gauge | Composite growth score (raw) |
| `github.repo.normalized_growth_score` | Gauge | Growth score normalized to 0-100 |

### Metric Dimensions

Each metric includes the following attributes (dimensions):

| Attribute | Description | Example |
|-----------|-------------|---------|
| `repo_owner` | Repository owner/organization | `kubernetes` |
| `repo_name` | Repository name | `kubernetes` |
| `repo_full_name` | Full `owner/repo` identifier | `kubernetes/kubernetes` |
| `language` | Primary programming language | `Go` |
| `category` | Assigned category | `cncf` |

### Resource Attributes

| Attribute | Description |
|-----------|-------------|
| `service.name` | Configured service name (default: `github-radar`) |
| `service.version` | Application version |

## Backend Setup

### Dynatrace

```yaml
otel:
  endpoint: https://{your-environment-id}.live.dynatrace.com/api/v2/otlp/v1/metrics
  headers:
    Authorization: "Api-Token ${DT_API_TOKEN}"
```

The `DT_API_TOKEN` requires the **Ingest metrics** (`metrics.ingest`) scope:

1. Go to **Settings** > **Access tokens** in your Dynatrace environment
2. Create a token with the `metrics.ingest` scope
3. Set as environment variable: `export DT_API_TOKEN="dt0c01.xxxxx"`

### Grafana Cloud (OTLP)

```yaml
otel:
  endpoint: https://otlp-gateway-{region}.grafana.net/otlp/v1/metrics
  headers:
    Authorization: "Basic ${GRAFANA_OTLP_TOKEN}"
```

### Local OTel Collector

Run a local OpenTelemetry Collector and configure GitHub Radar to send to it:

```yaml
otel:
  endpoint: http://localhost:4318
```

Example OTel Collector config (`otel-collector-config.yaml`):

```yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318

processors:
  batch:
    timeout: 10s

exporters:
  prometheus:
    endpoint: 0.0.0.0:8889
  debug:
    verbosity: detailed

service:
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [prometheus, debug]
```

### Prometheus (via OTel Collector)

Use the OTel Collector with a Prometheus exporter to scrape metrics:

```yaml
exporters:
  prometheus:
    endpoint: 0.0.0.0:8889
    namespace: github_radar
```

Then add to your Prometheus scrape config:

```yaml
scrape_configs:
  - job_name: github-radar
    static_configs:
      - targets: ['otel-collector:8889']
```

## Dry-Run Mode

Test your configuration without actually exporting metrics:

```bash
github-radar collect --config config.yaml --dry-run
```

In dry-run mode, all data is collected and scored, but metrics are not sent to the OTLP endpoint. State is still saved.

## Metric Flush on Exit

GitHub Radar synchronously flushes all pending metrics before exiting. The `flush_timeout` setting controls how long to wait (default: 10 seconds). If the flush times out, a warning is logged but the exit code is not affected.
