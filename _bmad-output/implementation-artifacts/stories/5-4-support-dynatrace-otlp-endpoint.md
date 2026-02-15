# Story 5.4: Support Dynatrace OTLP Endpoint

Status: done

## Story

As an **operator**,
I want to export metrics to Dynatrace via OTLP,
So that I can use Dynatrace for dashboards and alerting.

## Acceptance Criteria

1. **Given** Dynatrace OTLP endpoint URL
   **When** configured with `Authorization: Api-Token ${DT_API_TOKEN}`
   **Then** metrics are ingested by Dynatrace (NFR3)

2. **Given** Dynatrace configuration
   **When** setting endpoint URL
   **Then** format: `https://{env}.live.dynatrace.com/api/v2/otlp/v1/metrics`

3. **Given** connection errors
   **When** export fails
   **Then** clear diagnostics are shown

## Tasks / Subtasks

- [x] Task 1: Support custom headers for Api-Token
- [x] Task 2: Document Dynatrace endpoint format
- [x] Task 3: Test with headers configuration

## Completion Notes

- Headers config supports `Authorization: Api-Token ${DT_API_TOKEN}`
- Endpoint URL is configurable (any OTLP-compatible backend)
- Environment variable expansion supported in config
- OTLP/HTTP 1.0.0 protocol used

Example config:
```yaml
otel:
  endpoint: https://{env}.live.dynatrace.com/api/v2/otlp/v1/metrics
  headers:
    Authorization: "Api-Token ${DT_API_TOKEN}"
```

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/metrics/exporter.go (headers support)
- internal/config/expand.go (env var expansion)
