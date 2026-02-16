# CLI Reference

## Global Flags

These flags apply to all commands:

| Flag | Description | Default |
|------|-------------|---------|
| `--config <path>` | Path to configuration file | `./github-radar.yaml` |
| `--verbose` | Enable debug logging | `false` |
| `--dry-run` | Collect data but don't export metrics | `false` |

## Commands

### discover

Discover trending repositories from GitHub Search API.

```bash
github-radar discover --config config.yaml
```

| Flag | Description | Default |
|------|-------------|---------|
| `--topics <list>` | Comma-separated topics to search | From config |
| `--min-stars <int>` | Minimum star count | From config |
| `--max-age <int>` | Maximum repo age in days | From config |
| `--threshold <float>` | Auto-track growth score threshold | From config |
| `--auto-track` | Automatically add high-scoring repos to tracking | `false` |
| `--format <fmt>` | Output format: `table`, `json`, `csv` | `table` |

**Examples:**

```bash
# Search specific topics
github-radar discover --config config.yaml --topics kubernetes,ebpf,wasm

# Stricter filters
github-radar discover --config config.yaml --min-stars 500 --max-age 30

# Auto-track and output JSON
github-radar discover --config config.yaml --auto-track --format json
```

---

### add

Add a repository to tracking.

```bash
github-radar add <owner/repo> [flags]
```

| Flag | Description | Default |
|------|-------------|---------|
| `--category <name>` | Category for the repository | `default` |

**Examples:**

```bash
github-radar add kubernetes/kubernetes --config config.yaml
github-radar add grafana/grafana --category monitoring --config config.yaml
```

---

### remove

Remove a repository from tracking.

```bash
github-radar remove <owner/repo> [flags]
```

| Flag | Description | Default |
|------|-------------|---------|
| `--keep-state` | Preserve state data for the repository | `false` |

**Examples:**

```bash
github-radar remove example-org/old-repo --config config.yaml
github-radar remove example-org/old-repo --keep-state --config config.yaml
```

---

### list

List all tracked repositories with current metrics and scores.

```bash
github-radar list [flags]
```

| Flag | Description | Default |
|------|-------------|---------|
| `--category <name>` | Filter by category | All |
| `--format <fmt>` | Output format: `table`, `json`, `csv` | `table` |

**Examples:**

```bash
github-radar list --config config.yaml
github-radar list --config config.yaml --category cncf
github-radar list --config config.yaml --format json
```

---

### exclude

Manage the exclusion list. Excluded repos are never scanned or auto-tracked.

```bash
github-radar exclude <action> [pattern]
```

**Actions:**

| Action | Description |
|--------|-------------|
| `list` | Show all exclusion patterns |
| `add <pattern>` | Add an exclusion (exact `owner/repo` or wildcard `owner/*`) |
| `remove <pattern>` | Remove an exclusion |

**Examples:**

```bash
github-radar exclude list --config config.yaml
github-radar exclude add spam-org/crypto-thing --config config.yaml
github-radar exclude add spam-org/* --config config.yaml
github-radar exclude remove spam-org/crypto-thing --config config.yaml
```

---

### serve

Start the background daemon for scheduled scanning.

```bash
github-radar serve [flags]
```

| Flag | Description | Default |
|------|-------------|---------|
| `--interval <duration>` | Scan interval (e.g., `6h`, `24h`) | `24h` |
| `--http-addr <addr>` | HTTP server address for health/status | `:8080` |
| `--state <path>` | State file path | `data/state.json` |

**HTTP Endpoints:**

| Endpoint | Description |
|----------|-------------|
| `GET /health` | Health check: `{"healthy": true}` |
| `GET /status` | Status: scan state, repos tracked, next scan, rate limit |

**Signals:**

| Signal | Action |
|--------|--------|
| `SIGTERM` / `SIGINT` | Graceful shutdown |
| `SIGHUP` | Reload configuration |

**Examples:**

```bash
github-radar serve --config config.yaml --interval 6h
github-radar serve --config config.yaml --http-addr :9090
```

---

### status

Check the running daemon's status.

```bash
github-radar status [flags]
```

| Flag | Description | Default |
|------|-------------|---------|
| `--addr <url>` | Daemon HTTP address | `http://localhost:8080` |
| `--format <fmt>` | Output format: `text`, `json` | `text` |

**Examples:**

```bash
github-radar status
github-radar status --addr http://localhost:9090
github-radar status --format json
```

---

### config

Configuration management commands.

```bash
github-radar config <subcommand>
```

| Subcommand | Description |
|------------|-------------|
| `validate` | Validate the configuration file |
| `show` | Display resolved configuration (secrets masked) |

**Examples:**

```bash
github-radar config validate --config config.yaml
github-radar config show --config config.yaml
```
