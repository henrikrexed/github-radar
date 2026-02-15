# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build binary with version info
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s -X main.Version=${VERSION}" \
    -o /github-radar ./cmd/github-radar

# Runtime stage - using alpine for wget/curl availability
FROM alpine:3.19

# Install ca-certificates for HTTPS and wget for health checks
RUN apk --no-cache add ca-certificates wget

# Create non-root user
RUN addgroup -S github-radar && adduser -S github-radar -G github-radar

COPY --from=builder /github-radar /github-radar

# Create data directory
RUN mkdir -p /data /etc/github-radar && chown -R github-radar:github-radar /data

# Default config and data locations
VOLUME ["/etc/github-radar", "/data"]

USER github-radar

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD wget -q --spider http://localhost:8080/health || exit 1

# Expose status endpoint port
EXPOSE 8080

ENTRYPOINT ["/github-radar"]
CMD ["serve", "--config", "/etc/github-radar/config.yaml", "--state", "/data/state.json"]
