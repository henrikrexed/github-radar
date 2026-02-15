# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /github-radar ./cmd/github-radar

# Runtime stage
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /github-radar /github-radar

# Default config location
VOLUME ["/etc/github-radar"]

USER nonroot:nonroot

ENTRYPOINT ["/github-radar"]
CMD ["serve", "--config", "/etc/github-radar/config.yaml"]
