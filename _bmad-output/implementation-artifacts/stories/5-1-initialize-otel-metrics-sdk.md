# Story 5.1: Initialize OTel Metrics SDK

Status: done

## Story

As the **system**,
I want to configure the OpenTelemetry metrics SDK,
So that metrics can be exported via OTLP HTTP.

## Acceptance Criteria

1. **Given** OTLP endpoint and headers in config
   **When** the SDK is initialized
   **Then** OTLP/HTTP 1.0.0 exporter is configured (NFR2)

2. **Given** custom headers (e.g., Api-Token)
   **When** the SDK is initialized
   **Then** headers are included in export requests (FR39)

3. **Given** resource attributes
   **When** metrics are exported
   **Then** service.name and service.version are set (FR38)

## Tasks / Subtasks

- [x] Task 1: Add OpenTelemetry dependencies
- [x] Task 2: Create Exporter struct with configuration
- [x] Task 3: Implement OTLP HTTP exporter initialization
- [x] Task 4: Support custom headers for authentication
- [x] Task 5: Write tests

## Completion Notes

- Uses go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp
- ExporterConfig holds endpoint, headers, service name/version
- MeterProvider configured with PeriodicReader (60s interval)
- Supports dry-run mode with ManualReader

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/metrics/exporter.go (NewExporter, init)
- internal/metrics/exporter_test.go
- go.mod (OTel dependencies)
