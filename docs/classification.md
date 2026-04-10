# Classification Guide

GitHub Radar can automatically classify tracked repositories into CNCF and cloud-native categories using a local LLM via [Ollama](https://ollama.com). This helps organize large numbers of discovered repositories by technology domain.

## Overview

The classification pipeline:

1. Identifies repositories needing classification (new, or README changed)
2. Fetches the repository README from GitHub
3. Builds a prompt with repo metadata (name, description, language, topics, stars, README excerpt)
4. Sends the prompt to an Ollama LLM
5. Parses the JSON response for category, confidence, and reasoning
6. Stores the result — or marks as `needs_review` if confidence is below the threshold

## Prerequisites

### Ollama Setup

Classification requires a running [Ollama](https://ollama.com) instance:

```bash
# Install Ollama (Linux)
curl -fsSL https://ollama.com/install.sh | sh

# Pull the default model
ollama pull qwen3:1.7b

# Verify Ollama is running
curl http://localhost:11434/api/tags
```

!!! tip
    The default model `qwen3:1.7b` is lightweight (~1GB) and works well for classification. For higher accuracy on ambiguous repositories, consider larger models like `llama3:8b` or `qwen3:8b`.

### Configuration

Add the `classification` section to your config file:

```yaml
classification:
  ollama_endpoint: "http://localhost:11434"
  model: "qwen3:1.7b"
  timeout_ms: 30000
  max_readme_chars: 2000
  min_confidence: 0.6
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
    - other
```

See [Configuration Reference](configuration.md#classification-configuration) for all fields and prompt template variables.

## Categories

GitHub Radar ships with 19 CNCF-aligned categories plus an `other` catch-all:

| Category | Description |
|----------|-------------|
| `ai-agents` | Autonomous AI agent frameworks and tools |
| `llm-tooling` | LLM inference, fine-tuning, and tooling |
| `kubernetes` | Kubernetes core, operators, and extensions |
| `observability` | Monitoring, tracing, logging, and alerting |
| `cloud-native-security` | Container security, policy engines, supply chain |
| `networking` | CNI, load balancers, DNS, proxies |
| `service-mesh` | Service mesh implementations and control planes |
| `platform-engineering` | Internal developer platforms, IDPs, and portals |
| `gitops` | GitOps controllers and CD pipelines |
| `mlops` | ML pipelines, model serving, experiment tracking |
| `vector-database` | Vector stores and embedding databases |
| `rag` | Retrieval-augmented generation frameworks |
| `wasm` | WebAssembly runtimes and toolchains |
| `developer-tools` | CLI tools, SDKs, linters, and dev productivity |
| `infrastructure` | IaC, provisioning, cloud management |
| `data-engineering` | Data pipelines, streaming, and processing |
| `testing` | Testing frameworks, chaos engineering, load testing |
| `container-runtime` | Container runtimes and image builders |
| `other` | Catch-all for repos that don't fit above categories |

You can customize this list in your config file. The LLM is instructed to pick exactly one category from your configured list.

## Usage

### Batch Classification

Classify all repositories that need classification:

```bash
github-radar classify --config config.yaml
```

This processes repositories that are:

- Newly tracked and never classified
- Previously classified but whose README has changed (detected via SHA-256 hash)
- Queued for reclassification after a model change

Use `--dry-run` to preview without calling the LLM:

```bash
github-radar classify --config config.yaml --dry-run
```

### Test a Single Repository

Debug classification for a specific repo without saving results:

```bash
github-radar classify test kubernetes/kubernetes --config config.yaml
```

This prints the full prompt, LLM response, timing, and a warning if confidence is below the threshold. Use this to:

- Verify Ollama connectivity and model availability
- Debug prompt templates
- Evaluate how the model handles specific repositories

### Change the Model

View the current model:

```bash
github-radar classify model --config config.yaml
```

Switch to a different model:

```bash
github-radar classify model llama3:8b --config config.yaml
```

!!! warning
    Changing the model queues **all** previously classified repositories for reclassification. The next `classify` run will re-process them with the new model.

## How It Works

### README-Based Reclassification

GitHub Radar computes a SHA-256 hash of each repository's README content. When the hash changes between classification runs, the repository is automatically queued for reclassification. This ensures categories stay current as projects evolve.

### Confidence and Review

The LLM returns a confidence score (0.0 to 1.0) with each classification. If confidence falls below `min_confidence` (default: 0.6), the repository is marked as `needs_review` rather than auto-classified. This prevents low-confidence misclassifications from polluting your data.

### LLM Response Format

The LLM is instructed to respond with JSON:

```json
{
  "category": "kubernetes",
  "confidence": 0.92,
  "reasoning": "Core Kubernetes orchestration platform for container workloads"
}
```

The classification pipeline validates that the returned category is in the configured list and the confidence is a valid float between 0 and 1.

## Troubleshooting

### Ollama Connection Refused

```
Error from Ollama: connection refused
```

Ensure Ollama is running and the `ollama_endpoint` in your config matches. Default is `http://localhost:11434`.

### Model Not Found

```
Error from Ollama: model "qwen3:1.7b" not found
```

Pull the model first: `ollama pull qwen3:1.7b`

### Low Confidence on Many Repos

If many repositories are marked `needs_review`:

- Try a larger model (e.g., `llama3:8b`)
- Lower `min_confidence` if acceptable for your use case
- Customize the `system_prompt` to provide clearer classification instructions
- Increase `max_readme_chars` to give the LLM more context

### Timeout Errors

Increase `timeout_ms` in the classification config. Larger models or slower hardware may need 60000ms or more.
