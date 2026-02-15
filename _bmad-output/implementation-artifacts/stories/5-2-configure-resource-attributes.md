# Story 5.2: Configure Resource Attributes

Status: done

## Story

As the **system**,
I want to include OTel semantic resource attributes,
So that metrics are properly identified in the backend.

## Acceptance Criteria

1. **Given** OTel semantic conventions (NFR5)
   **When** metrics are exported
   **Then** `service.name` = configured service name (default: github-radar)

2. **Given** application version
   **When** metrics are exported
   **Then** `service.version` = application version

3. **Given** additional custom attributes in config
   **When** metrics are exported
   **Then** custom attributes are supported

## Tasks / Subtasks

- [x] Task 1: Configure resource with semantic conventions
- [x] Task 2: Set service.name from config
- [x] Task 3: Set service.version from build
- [x] Task 4: Support custom attributes
- [x] Task 5: Update config types

## Completion Notes

- Uses semconv v1.24.0 for semantic conventions
- ServiceName defaults to "github-radar"
- ServiceVersion set via Version variable (build-time)
- OtelConfig extended with ServiceVersion and Attributes fields

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/metrics/exporter.go (resource configuration)
- internal/config/types.go (OtelConfig updates)
