# Contributing

Contributions to GitHub Radar are welcome.

## Development Setup

### Prerequisites

- Go 1.22+
- Make
- Git

### Getting Started

```bash
git clone https://github.com/henrikrexed/github-radar.git
cd github-radar
make build
make test
```

### Development Commands

| Command | Description |
|---------|-------------|
| `make build` | Build the binary to `./bin/github-radar` |
| `make test` | Run all tests |
| `make test-v` | Run tests with verbose output |
| `make test-coverage` | Run tests and generate coverage report |
| `make lint` | Run `go vet` and `staticcheck` |
| `make fmt` | Format all Go files |
| `make fmt-check` | Check for formatting issues (CI uses this) |
| `make clean` | Remove build artifacts |

## Code Style

- Run `gofmt` before committing (or use `make fmt`)
- Follow standard Go conventions and naming
- Use `slog` for structured logging
- Handle errors explicitly (no ignored errors)

## Testing

### Running Tests

```bash
# All tests
make test

# Verbose
make test-v

# With race detector
go test -race ./...

# Specific package
go test ./internal/scoring/...

# With coverage
make test-coverage
open coverage.html
```

### Test Patterns

- Unit tests use `httptest.NewServer` to mock GitHub API responses
- Memory/resource tests use `runtime.MemStats` and `runtime.NumGoroutine`
- State tests use `t.TempDir()` for temporary files
- Integration tests use mock servers with realistic response shapes

## Pull Request Process

1. Fork the repository
2. Create a feature branch from `main`
3. Make your changes with tests
4. Ensure all checks pass: `make test && make lint && make fmt-check`
5. Submit a pull request

CI will automatically run build, test, quality, and security checks on your PR.

## Project Structure

See [Architecture](architecture.md) for the full project layout and component descriptions.
