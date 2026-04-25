# Feature Spec: Category Classification

**Version:** 1.0
**Date:** 2026-02-15
**Status:** Ready for Implementation

---

## Overview

Add automated category classification to the GitHub Trend Scanner to enable per-category alerting, filtered dashboards, and better signal-to-noise ratio for trending repository data.

### Primary Persona

**DevRel / Content Creator at Dynatrace**

- Tracks trending CNCF/cloud-native projects
- Needs category-based filtering to focus on relevant domains
- Shares news about recent projects with community team
- Uses Dynatrace as central consumption layer

### Goals

1. Classify repositories into CNCF/cloud-native categories automatically
2. Enable per-category Slack alerting via Dynatrace Workflows
3. Support multi-user team (DevRel, Community) with shared dashboards
4. Keep the scanner simple — no custom UI, Dynatrace handles visualization

### Rollout

- **Beta:** Henrik as single user
- **Production:** Dynatrace internal team (DevRel, Community)

---

## Architecture Decisions

| Component | Decision | Rationale |
|-----------|----------|-----------|
| Data store | SQLite | Single VM, no ops overhead, no external service |
| Metrics storage | None | Fire-and-forget to OTel → Dynatrace handles history |
| Category model | Primary only | No secondary categories needed |
| Classification history | None | Only current classification matters |
| Classification UI | None | Dynatrace dashboards + workflows |
| Review queue | None | Low-confidence = metric attribute, filter in Dynatrace |
| Manual overrides | CLI commands | `exclude` and `force_category` |
| Reclassification | Cron job | User-configured schedule via crontab |

---

## Category Taxonomy

### Fixed Categories (18 + other)

```yaml
categories:
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
  - other  # catch-all, always present
```

### Category Descriptions

| Category | Examples |
|----------|----------|
| ai-agents | AutoGPT, LangChain agents, autonomous AI |
| llm-tooling | Ollama, vLLM, text-generation-webui |
| kubernetes | K8s controllers, operators, tools |
| observability | OpenTelemetry, Prometheus, tracing, logging |
| cloud-native-security | Falco, Trivy, policy engines, scanning |
| networking | CNI plugins, load balancers, ingress |
| service-mesh | Istio, Linkerd, Envoy, mesh tools |
| platform-engineering | Backstage, Kratix, IDPs, portals |
| gitops | ArgoCD, Flux, config management |
| mlops | Kubeflow, MLflow, model serving |
| vector-database | Milvus, Qdrant, Weaviate, embeddings storage |
| rag | RAG frameworks, retrieval augmented generation |
| wasm | WebAssembly runtimes, WASI, wasm tools |
| developer-tools | CLIs, SDKs, dev utilities |
| infrastructure | Terraform, Pulumi, IaC tools |
| data-engineering | Streaming, pipelines, ETL, data processing |
| testing | Test frameworks, chaos engineering |
| container-runtime | containerd, CRI-O, container runtimes |
| other | Catch-all for unclassified repos |

### Behavior

- LLM must pick from fixed list or assign `other`
- `other` repos appear in "Uncategorized" dashboard widget
- User reviews `other` periodically, adds new categories as patterns emerge
- Uncategorized repos still trigger workflow alerts
- Categories managed via CLI/config to prevent duplicates

---

## Database Schema

### Schema Changes

```sql
-- Add columns to repos table
ALTER TABLE repos ADD COLUMN primary_category TEXT;
ALTER TABLE repos ADD COLUMN category_confidence REAL;
ALTER TABLE repos ADD COLUMN readme_hash TEXT;
ALTER TABLE repos ADD COLUMN classified_at TEXT;
ALTER TABLE repos ADD COLUMN model_used TEXT;
ALTER TABLE repos ADD COLUMN force_category TEXT;        -- manual override
ALTER TABLE repos ADD COLUMN excluded INTEGER DEFAULT 0;

-- Indexes for performance
CREATE INDEX idx_repos_category ON repos(primary_category);
CREATE INDEX idx_repos_status ON repos(status);
CREATE INDEX idx_repos_excluded ON repos(excluded);
```

### Status Values

| Status | Description |
|--------|-------------|
| `pending` | New repo or README changed, awaiting classification |
| `classified` | Successfully classified with confidence >= 0.7 |
| `needs_reclassify` | Queued for reclassification (model change, manual trigger) |

---

## Override Mechanism

### Behaviors

| Action | Behavior |
|--------|----------|
| Exclude existing repo | Sets `excluded=1`, skipped in collect/classify/export, stays in DB |
| Exclude unknown repo | Creates placeholder with `excluded=1` |
| Force category on existing repo | Sets `force_category`, skips LLM classification |
| Force category on unknown repo | Creates placeholder, populates on next collect |
| Unset force category | Clears override, repo queued for reclassification |

### Data Integrity

- Excluding a repo does NOT delete historical data from SQLite
- Excluded repos are silently omitted from OTel exports
- Force category takes precedence over LLM classification

---

## Classification Service

### Configuration File

**Location:** `config/classification.yaml`

```yaml
ollama:
  endpoint: "http://localhost:11434"
  model: "qwen3:0.6b"
  timeout_ms: 30000

classification:
  max_readme_chars: 2000
  min_confidence: 0.7

  categories:
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

  system_prompt: |
    You are a GitHub repository classifier for CNCF and cloud-native projects.

    Your task:
    1. Read the repository README excerpt provided
    2. Classify into exactly ONE primary category from the allowed list
    3. Return a confidence score (0.0 to 1.0)

    Allowed categories:
    {{categories}}

    If the repository does not clearly fit any category, use "other".

    Respond ONLY with valid JSON:
    {
      "category": "<category-name>",
      "confidence": <0.0-1.0>,
      "reasoning": "<one sentence>"
    }

  user_prompt: |
    Repository: {{repo_name}}
    Description: {{description}}
    Language: {{language}}
    Topics: {{topics}}

    README excerpt:
    {{readme}}
```

### Prompt Variables

| Variable | Source |
|----------|--------|
| `{{categories}}` | Joined from `categories` list |
| `{{repo_name}}` | `full_name` from repos table |
| `{{description}}` | Repo description |
| `{{language}}` | Primary language |
| `{{topics}}` | GitHub topics (JSON array) |
| `{{readme}}` | Truncated README content |

---

## Model Configuration

### Upgrade Path

| Model | Use Case | Accuracy | RAM | Speed |
|-------|----------|----------|-----|-------|
| `qwen3:0.6b` | MVP / low-resource | ~80% | ~1GB | Fast |
| `qwen3:1.7b` | Production | ~90% | ~2GB | Moderate |
| `qwen3:4b` | High accuracy | ~95% | ~4GB | Slower |

### Model Switch Behavior

When model changes via CLI:

1. Updates config file
2. Pulls model via `ollama pull` if not local
3. Marks ALL classified repos as `status='needs_reclassify'`
4. Logs: "Model changed. X repos queued for reclassification."

User runs `scanner classify` to process the queue.

---

## Benchmark Testing

### Purpose

Measure model performance to guide model/prompt selection:
- Processing time per model
- Category accuracy
- Confidence distribution
- Prompt effectiveness

### Benchmark Output

```
Model: qwen3:0.6b
─────────────────────────────
Repos tested:       50
Avg duration:       1.2s
Avg tokens:         312
Category distribution:
  kubernetes:       12 (24%)
  observability:    8  (16%)
  ai-agents:        7  (14%)
  other:            6  (12%)  ← review these
  ...
Confidence:
  High (>0.8):      32 (64%)
  Medium (0.5-0.8): 14 (28%)
  Low (<0.5):       4  (8%)
─────────────────────────────
```

### Stored Results

**Location:** `_bmad-output/benchmarks/benchmark-{timestamp}.json`

```json
{
  "timestamp": "2026-02-15T10:30:00Z",
  "model": "qwen3:0.6b",
  "sample_size": 50,
  "results": [
    {
      "repo": "owner/repo",
      "category": "observability",
      "confidence": 0.85,
      "duration_ms": 1234,
      "prompt_tokens": 450,
      "completion_tokens": 28
    }
  ],
  "summary": {
    "avg_duration_ms": 1200,
    "avg_confidence": 0.78,
    "category_distribution": {},
    "low_confidence_repos": []
  }
}
```

### Benchmark Workflow

1. **Initial baseline** — Run benchmark with `qwen3:0.6b` on 50 repos
2. **Review `other` category** — Are repos correctly uncategorized, or is taxonomy missing something?
3. **Review low confidence** — Check if prompt needs refinement
4. **Compare models** — Run same sample through `1.7b` and `4b`
5. **Decision** — Trade off accuracy vs. speed vs. resource usage
6. **Adjust prompt** — If certain categories are over/under-represented, tweak system prompt
7. **Re-benchmark** — Validate changes

---

## Data Flow

```
[Cron: collect] (configurable, e.g., every 6h)
  → GitHub API: fetch repo data
  → Skip repos where excluded=1
  → Compute scores + deltas
  → Upsert into SQLite
  → Mark status='pending' if new or README hash changed
  → Export OTel metrics (with category if classified)

[Cron: classify] (configurable via crontab, runs after collect)
  → Query: SELECT * FROM repos
           WHERE excluded=0
           AND force_category IS NULL
           AND (status='pending' OR status='needs_reclassify')
  → For each repo:
      → Fetch README from GitHub
      → Hash README content (SHA256)
      → If hash unchanged AND already classified → skip
      → Truncate README to max_readme_chars
      → Call Ollama with system_prompt + user_prompt
      → Parse JSON response
      → Store: primary_category, confidence, readme_hash, model_used, classified_at
      → status = 'classified'
  → Export classification metrics

[Cron: export]
  → Read all repos WHERE excluded=0
  → Emit OTel metrics:
      github.repo.stars{repo, category, confidence}
      github.repo.growth_score{repo, category}
      github.repo.velocity{repo, category}
  → Flush to collector → Backend
```

---

## Self-Monitoring (Optional)

### Overview

Export scanner health metrics, logs, and LLM telemetry to observability backend.

### Configuration

**Location:** `config/telemetry.yaml`

```yaml
telemetry:
  enabled: true

  host_metrics:
    enabled: true
    collection_interval: 60s

  llm_metrics:
    enabled: true
```

### Host Metrics (hostmetrics receiver)

| Metric | Description |
|--------|-------------|
| `system.cpu.utilization` | CPU usage % |
| `system.memory.usage` | Memory consumption |
| `system.disk.io` | Disk read/write |
| `process.cpu.utilization` | Scanner process CPU |
| `process.memory.usage` | Scanner process memory |
| `process.threads` | Active threads |

### LLM Telemetry (OpenLLMetry)

**Metrics:**

| Metric | Description |
|--------|-------------|
| `llm.request.duration` | Time per Ollama call |
| `llm.tokens.prompt` | Input tokens per request |
| `llm.tokens.completion` | Output tokens per request |
| `llm.tokens.total` | Total tokens consumed |
| `llm.request.success` | Success/failure count |
| `llm.request.model` | Model used (dimension) |

**Traces:**
- Span per classification request
- Attributes: `repo`, `model`, `category`, `confidence`
- Link to parent collect/classify job span

**Use Cases:**
- Track token consumption over time (cost projection)
- Identify slow classifications
- Compare model performance in production
- Alert on LLM errors (Ollama down, timeout)

### Scanner Logs

**Structured logging** via OTel Logs API:

```json
{
  "timestamp": "2026-02-15T10:30:00Z",
  "severity": "INFO",
  "body": "Classification complete",
  "attributes": {
    "repo": "owner/repo",
    "category": "observability",
    "confidence": 0.85,
    "duration_ms": 1234,
    "model": "qwen3:0.6b"
  }
}
```

**Log Levels:**
- `INFO` — Collection/classification events
- `WARN` — Low confidence, retries, rate limits
- `ERROR` — API failures, Ollama errors

---

## Collector Exporter Configuration

### User-Configurable Exporters

**Location:** `config/collector-exporters.yaml`

```yaml
# Define your backend(s) - scanner merges into collector config
exporters:
  # Option A: Dynatrace
  otlphttp/dynatrace:
    endpoint: "https://{env-id}.live.dynatrace.com/api/v2/otlp"
    headers:
      Authorization: "Api-Token ${DT_API_TOKEN}"

  # Option B: Grafana Cloud
  # otlphttp/grafana:
  #   endpoint: "https://otlp-gateway-prod-us-central-0.grafana.net/otlp"
  #   headers:
  #     Authorization: "Basic ${GRAFANA_CLOUD_TOKEN}"

  # Option C: Self-hosted (Jaeger, Tempo, etc.)
  # otlp/local:
  #   endpoint: "localhost:4317"
  #   tls:
  #     insecure: true

  # Option D: Multiple backends
  # otlphttp/primary:
  #   endpoint: "https://primary-backend.example.com"
  # otlphttp/secondary:
  #   endpoint: "https://backup-backend.example.com"

# Pipeline routing - which exporters receive which signals
pipelines:
  metrics:
    exporters: [otlphttp/dynatrace]
  traces:
    exporters: [otlphttp/dynatrace]
  logs:
    exporters: [otlphttp/dynatrace]
```

### How It Works

1. Scanner ships with `collector-exporters.yaml.example`
2. User copies to `collector-exporters.yaml` and customizes
3. Scanner merges user exporters into base collector config at startup
4. Base config handles receivers/processors — user only touches exporters

### Supported Backends

| Backend | Exporter Type | Notes |
|---------|---------------|-------|
| Dynatrace | `otlphttp` | API token required |
| Grafana Cloud | `otlphttp` | Basic auth |
| New Relic | `otlphttp` | License key header |
| Honeycomb | `otlphttp` | API key header |
| Jaeger | `otlp` (gRPC) | Self-hosted |
| Prometheus | `prometheusremotewrite` | Metrics only |
| Loki | `loki` | Logs only |
| Local file | `file` | Debug/testing |

### Base Collector Config

```yaml
# otel-collector-config.yaml (base - managed by scanner)
receivers:
  hostmetrics:
    collection_interval: 60s
    scrapers:
      cpu:
      memory:
      disk:
      process:
        include:
          match_type: strict
          names: ["node", "ollama"]

  otlp:
    protocols:
      grpc:
        endpoint: "0.0.0.0:4317"
      http:
        endpoint: "0.0.0.0:4318"

processors:
  batch:
    timeout: 10s
  resourcedetection:
    detectors: [env, system]

# Exporters and pipelines merged from user config
```

---

## Deployment

### Target Environment

| Spec | Value |
|------|-------|
| Target | Ubuntu VM on Proxmox |
| Resources | 4 vCPU, 4GB RAM, 20GB disk |
| GPU | None (CPU-only) |
| LLM | Ollama + qwen3:0.6b (`OLLAMA_NUM_GPU=0`) |
| Database | SQLite (no external service) |
| Runtime | Node.js 22 |
| Scheduling | Cron (user-configured) |

### Cron Configuration

```cron
# Example crontab
# Collect every 6 hours
0 */6 * * * /path/to/scanner collect

# Classify 30 minutes after each collect
30 */6 * * * /path/to/scanner classify

# Export metrics hourly
0 * * * * /path/to/scanner export
```

---

## CLI Command Reference

### Collection

```bash
scanner collect              # Run collection now
scanner collect --dry-run    # Preview what would be collected
```

### Classification

```bash
scanner classify             # Run classification now
scanner classify test <repo> # Dry-run single repo
scanner classify show-config # View prompt config
scanner classify edit-config # Edit in $EDITOR
scanner classify reload      # Reload config without restart
```

### Model Management

```bash
scanner classify model           # Show current model
scanner classify model <name>    # Switch model (triggers reclassification)
scanner classify model --list    # List available models
```

### Benchmark

```bash
scanner benchmark --sample 50                              # Run on sample
scanner benchmark --repos owner/repo1,owner/repo2          # Run on specific repos
scanner benchmark --compare qwen3:0.6b,qwen3:1.7b --sample 30  # Compare models
```

### Exclusions

```bash
scanner exclude list             # List excluded repos
scanner exclude add <repo>       # Add exclusion
scanner exclude remove <repo>    # Remove exclusion
```

### Category Overrides

```bash
scanner category list-overrides  # List manual overrides
scanner category set <repo> <category>  # Force category
scanner category unset <repo>    # Remove override
```

### Category Taxonomy

```bash
scanner category list            # List allowed categories
scanner category add <name>      # Add new category
scanner category remove <name>   # Remove category
```

### Export

```bash
scanner export                   # Run OTel export now
scanner export --dry-run         # Preview metrics
```

### Telemetry

```bash
scanner telemetry status         # Check telemetry status
scanner telemetry enable         # Enable self-monitoring
scanner telemetry disable        # Disable self-monitoring
scanner telemetry exporters      # Show exporter config
scanner telemetry exporters --edit  # Edit exporter config
scanner telemetry validate       # Validate config syntax
scanner telemetry test           # Test backend connectivity
```

---

## Dynatrace Integration

### Dashboards

| Widget | Purpose |
|--------|---------|
| Repos by category | Filterable view of all classified repos |
| Low confidence | Classifications with confidence < 0.7 |
| Uncategorized | Repos in `other` category |
| Top trending per category | Category-specific leaderboards |
| Scanner CPU/Memory | Self-monitoring: resource health |
| LLM requests/min | Self-monitoring: classification throughput |
| Avg LLM latency | Self-monitoring: model performance |
| Tokens consumed | Self-monitoring: usage tracking |
| Classification queue | Self-monitoring: backlog depth |

### Workflows

| Trigger | Action |
|---------|--------|
| New high-growth repo in [category] | Slack → #[category-channel] |
| Weekly digest of `other` repos | Slack → triage channel |
| LLM error rate spike | Alert → on-call |
| Classification queue backlog | Alert → on-call |

### SLOs (Optional)

- Classification coverage: % of repos with category != `other`
- Classification latency: p95 < 5s
- Scanner uptime: 99.5%

---

## Out of Scope (Future)

| Item | Rationale |
|------|-----------|
| Redis | Multi-instance shared state — not needed for single VM |
| Secondary categories | Primary is sufficient for current use cases |
| Classification history | Only current category matters |
| Custom review UI | Dynatrace handles visualization |
| `never_classify` flag | Can use `exclude` + manual tracking |
| `force_reclassify` flag | Model switch triggers bulk reclassify |
| GPU acceleration | CPU-only MVP, add if needed |

---

## Implementation Notes

### File Structure

```
config/
├── classification.yaml      # LLM prompts, categories, model
├── telemetry.yaml           # Self-monitoring settings
├── collector-exporters.yaml # User's exporter config
└── collector-exporters.yaml.example

data/
└── scanner.db               # SQLite database

_bmad-output/
└── benchmarks/
    └── benchmark-{timestamp}.json
```

### Environment Variables

| Variable | Description |
|----------|-------------|
| `GITHUB_TOKEN` | GitHub API access |
| `OLLAMA_HOST` | Ollama endpoint (default: localhost:11434) |
| `OLLAMA_NUM_GPU` | GPU layers (0 for CPU-only) |
| `DT_API_TOKEN` | Dynatrace API token (if using DT backend) |
| `DT_OTLP_ENDPOINT` | Dynatrace OTLP endpoint |

---

**End of Specification**
