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

### Discovery Metrics (gharchive event-stream)

> Status: spec-only. Live emission lands with [ISI-951](/ISI/issues/ISI-951) (collector implementation). Until then these names exist in `schemas/github-radar.yaml` and `dashboards/dt-radar-discovery.json` as the authoritative contract for the implementer.

When the gharchive **discovery** source is enabled (`discovery.sources.gharchive.enabled: true`, see [ISI-953](/ISI/issues/ISI-953)), the following metrics are exported to monitor pipeline lag, throughput, dedup, and backpressure during the [ISI-956](/ISI/issues/ISI-956) Stage C soak. Distinct namespace from the metrics-fallback collector below: `github_radar.discovery.*` describes our pipeline health, the data-export metrics above describe GitHub data.

| Metric | Type | Unit | Description |
|--------|------|------|-------------|
| `github_radar.discovery.gharchive.lag_seconds` | Gauge | `s` | Age of the most-recently-processed gharchive hourly archive (`now - archive_published_at`). |
| `github_radar.discovery.gharchive.candidates_total` | Counter | `1` | Repo candidates surfaced from gharchive top-N + activity floor selection. Carries `event_type`. |
| `github_radar.discovery.classifier.queue_depth` | Gauge | `1` | Pending candidates waiting on classifier. Co-owned with [ISI-954](/ISI/issues/ISI-954) backpressure. |
| `github_radar.discovery.gharchive.dedup_ratio` | Gauge | `1` | Fraction (0.0 – 1.0) of gharchive candidates dropped because the repo is already tracked. |
| `github_radar.discovery.gharchive.events_processed_total` | Counter | `1` | Raw gharchive events processed after event-type filter. Carries `event_type`. |

#### Discovery Metric Dimensions

| Attribute | Description | Allowed values |
|-----------|-------------|----------------|
| `event_type` | gharchive event type (carries on `candidates_total` and `events_processed_total` only — not on gauges) | `WatchEvent`, `ForkEvent`, `PushEvent`, `PullRequestEvent` |

Per-repo attributes (`repo_owner`, `repo_name`, `repo_full_name`) are intentionally **not** carried on these metrics — they would explode cardinality. Per-repo telemetry stays on the existing `github.repo.*` metrics.

See `docs/observability/gharchive-instrumentation-spec.md` for the implementer-facing instrumentation contract.

### Collector Metrics (gharchive.org Fallback)

When the gharchive.org fallback is enabled (`collector.gharchive.enabled: true`), the following metrics are exported to monitor the collector router and archive processing:

| Metric | Type | Description |
|--------|------|-------------|
| `collector_active` | Gauge | Active collector backend (1=active, 0=inactive). Attribute: `backend` = `live` or `gharchive`. |
| `fallback_trip_count_total` | Counter | Number of times the router switched from live API to gharchive fallback. |
| `gharchive_archive_bytes_downloaded_total` | Counter | Total bytes downloaded from gharchive.org hourly archives. |
| `gharchive_decode_duration_ms` | Histogram | Duration to decode and filter a single hourly archive file (gzip decompress + JSON stream). |
| `gharchive_events_filtered_total` | Counter | Events processed from gharchive archives. Attribute: `kept` = `true` (tracked repo) or `false` (discarded). |

#### Collector Metric Dimensions

| Attribute | Description | Values |
|-----------|-------------|--------|
| `backend` | Active collector backend | `live`, `gharchive` |
| `kept` | Whether the event matched a tracked repo | `true`, `false` |

#### Dashboard Recommendations

- Alert on `fallback_trip_count_total` increasing — sustained fallback usage indicates the tracked repo set may be too large for the current scan interval
- Monitor `gharchive_events_filtered_total{kept="false"}` to understand the discard ratio (typically >99%)
- Use `gharchive_decode_duration_ms` to track archive processing performance

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

### Local OTel Collector → Dynatrace (Recommended)

The default deployment uses a local OTel Collector as a gateway:

```
github-radar → OTel Collector (localhost:4318) → Dynatrace
```

GitHub Radar config (when using Docker Compose):

```yaml
otel:
  endpoint: http://otel-collector:4318
```

Set these environment variables in your `.env` file:

```bash
DT_OTLP_ENDPOINT=https://{your-environment-id}.live.dynatrace.com/api/v2/otlp
DT_API_TOKEN=dt0c01.xxxxx
```

The collector config (`configs/otel-collector-config.yaml`) is included in the repo and wired into `docker-compose.yml`. It receives OTLP/HTTP on port 4318, batches metrics, and forwards them to Dynatrace via OTLP/HTTP.

### Local OTel Collector (Generic)

For non-Dynatrace backends, customize the collector config:

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
github-radar serve --config config.yaml --dry-run
```

In dry-run mode, all data is collected and scored, but metrics are not sent to the OTLP endpoint. State is still saved.

## Metric Flush on Exit

GitHub Radar synchronously flushes all pending metrics before exiting. The `flush_timeout` setting controls how long to wait (default: 10 seconds). If the flush times out, a warning is logged but the exit code is not affected.
