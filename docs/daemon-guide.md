# Daemon Guide

GitHub Radar can run as a background daemon that scans repositories on a schedule. The daemon exposes HTTP endpoints for health checks and status monitoring, making it suitable for container orchestration platforms.

## Starting the Daemon

```bash
github-radar serve --config config.yaml --interval 6h
```

The daemon runs in the foreground (not detached), which is the expected behavior for Docker, systemd, and Kubernetes.

## Configuration Options

| Flag | Description | Default |
|------|-------------|---------|
| `--interval` | Time between scan cycles (e.g., `1h`, `6h`, `24h`) | `24h` |
| `--http-addr` | HTTP server bind address | `:8080` |
| `--state` | State file path for persistence | `data/state.json` |
| `--dry-run` | Collect data but skip metrics export | `false` |

## HTTP Endpoints

### Health Check

```bash
curl http://localhost:8080/health
```

Response:
```json
{"healthy": true}
```

Returns HTTP 200 when healthy, HTTP 503 when unhealthy. Use this for container health probes.

### Status

```bash
curl http://localhost:8080/status
```

Response:
```json
{
  "status": "idle",
  "last_scan": "2026-02-16T06:00:00Z",
  "repos_tracked": 47,
  "next_scan": "2026-02-16T12:00:00Z",
  "rate_limit_remaining": 4500,
  "uptime": "18h32m"
}
```

Status values: `idle`, `scanning`, `starting`.

## Signal Handling

| Signal | Action |
|--------|--------|
| `SIGTERM` / `SIGINT` | Graceful shutdown — completes current scan, flushes metrics, saves state |
| `SIGHUP` | Reload configuration — picks up new repos and settings without restart |

```bash
# Reload config
kill -HUP $(pidof github-radar)

# Graceful stop
kill $(pidof github-radar)
```

## Deployment Options

### systemd Service

Create `/etc/systemd/system/github-radar.service`:

```ini
[Unit]
Description=GitHub Radar - Open Source Growth Intelligence
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=github-radar
ExecStart=/usr/local/bin/github-radar serve \
    --config /etc/github-radar/config.yaml \
    --state /var/lib/github-radar/state.json \
    --interval 6h
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=30
Environment=GITHUB_TOKEN=ghp_your_token_here
Environment=DT_API_TOKEN=dt0c01.your_token

[Install]
WantedBy=multi-user.target
```

```bash
# Setup
sudo useradd --system --no-create-home github-radar
sudo mkdir -p /etc/github-radar /var/lib/github-radar
sudo cp config.yaml /etc/github-radar/config.yaml
sudo chown -R github-radar: /var/lib/github-radar

# Enable and start
sudo systemctl daemon-reload
sudo systemctl enable github-radar
sudo systemctl start github-radar

# Check status
sudo systemctl status github-radar
journalctl -u github-radar -f

# Reload config
sudo systemctl reload github-radar
```

### Docker Compose

The project includes a `docker-compose.yml`:

```bash
# Set environment variables
export GITHUB_TOKEN="ghp_your_token_here"
export DT_API_TOKEN="dt0c01.your_token"

# Create config
cp configs/github-radar.example.yaml configs/config.yaml
# Edit configs/config.yaml

# Start
docker-compose up -d

# Check logs
docker-compose logs -f github-radar

# Check health
curl http://localhost:8080/health

# Check status
curl http://localhost:8080/status

# Stop
docker-compose down
```

The Docker Compose setup:

- Mounts config as read-only volume
- Persists state to a named Docker volume
- Passes secrets via environment variables
- Includes health check (30s interval)
- Limits log file size (10MB, 3 files)
- Restarts on failure

### Kubernetes

Example Kubernetes deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: github-radar
spec:
  replicas: 1
  selector:
    matchLabels:
      app: github-radar
  template:
    metadata:
      labels:
        app: github-radar
    spec:
      containers:
        - name: github-radar
          image: ghcr.io/hrexed/github-radar:latest
          args:
            - serve
            - --config
            - /etc/github-radar/config.yaml
            - --state
            - /data/state.json
            - --interval
            - "6h"
          env:
            - name: GITHUB_TOKEN
              valueFrom:
                secretKeyRef:
                  name: github-radar-secrets
                  key: github-token
          ports:
            - containerPort: 8080
          livenessProbe:
            httpGet:
              path: /health
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 30
          readinessProbe:
            httpGet:
              path: /health
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
          volumeMounts:
            - name: config
              mountPath: /etc/github-radar
            - name: data
              mountPath: /data
      volumes:
        - name: config
          configMap:
            name: github-radar-config
        - name: data
          persistentVolumeClaim:
            claimName: github-radar-data
```

## Cron Alternative

If you prefer one-shot scans over a long-running daemon:

```bash
# Weekly scan every Monday at 6 AM
0 6 * * 1 GITHUB_TOKEN=ghp_xxx /usr/local/bin/github-radar collect --config /etc/github-radar/config.yaml

# Daily scan at midnight
0 0 * * * GITHUB_TOKEN=ghp_xxx /usr/local/bin/github-radar collect --config /etc/github-radar/config.yaml
```

## Scan Overlap Prevention

The daemon prevents overlapping scans. If a scan is still running when the next interval triggers, the new scan is skipped with a log message. This ensures stability under slow network conditions or large repository sets.
