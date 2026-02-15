.PHONY: build test lint clean run docker docker-push release serve help

# Build variables
VERSION ?= dev
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"
DOCKER_IMAGE := github-radar

# Build the binary
build:
	go build $(LDFLAGS) -o bin/github-radar ./cmd/github-radar

# Run tests
test:
	go test ./...

# Run tests with verbose output
test-v:
	go test -v ./...

# Run tests with coverage
test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Run linters
lint:
	go vet ./...
	@which staticcheck > /dev/null 2>&1 && staticcheck ./... || echo "staticcheck not installed, skipping"

# Format code
fmt:
	go fmt ./...

# Check for formatting issues
fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "Files need formatting:" && gofmt -l . && exit 1)

# Clean build artifacts
clean:
	rm -rf bin/ dist/
	rm -f coverage.out coverage.html

# Run the binary
run: build
	./bin/github-radar

# Run the daemon locally
serve: build
	./bin/github-radar serve --config configs/config.yaml --interval 1h

# Build Docker image
docker:
	docker build -t $(DOCKER_IMAGE):$(VERSION) .
	docker tag $(DOCKER_IMAGE):$(VERSION) $(DOCKER_IMAGE):latest

# Push Docker image (requires DOCKER_REGISTRY env var)
docker-push: docker
	@test -n "$(DOCKER_REGISTRY)" || (echo "DOCKER_REGISTRY not set" && exit 1)
	docker tag $(DOCKER_IMAGE):$(VERSION) $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(VERSION)
	docker tag $(DOCKER_IMAGE):$(VERSION) $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest
	docker push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(VERSION)
	docker push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest

# Run with docker-compose
docker-up:
	docker-compose up -d

# Stop docker-compose
docker-down:
	docker-compose down

# Cross-compile for all platforms
release: clean
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/github-radar-linux-amd64 ./cmd/github-radar
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/github-radar-linux-arm64 ./cmd/github-radar
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/github-radar-darwin-amd64 ./cmd/github-radar
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/github-radar-darwin-arm64 ./cmd/github-radar
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/github-radar-windows-amd64.exe ./cmd/github-radar
	@echo "Built binaries in dist/"

# Show help
help:
	@echo "Available targets:"
	@echo "  build         - Build the binary"
	@echo "  test          - Run tests"
	@echo "  test-v        - Run tests with verbose output"
	@echo "  test-coverage - Run tests with coverage report"
	@echo "  lint          - Run linters (go vet, staticcheck)"
	@echo "  fmt           - Format code"
	@echo "  fmt-check     - Check for formatting issues"
	@echo "  clean         - Clean build artifacts"
	@echo "  run           - Build and run the binary"
	@echo "  serve         - Run the daemon locally"
	@echo "  docker        - Build Docker image"
	@echo "  docker-push   - Push Docker image to registry"
	@echo "  docker-up     - Start with docker-compose"
	@echo "  docker-down   - Stop docker-compose"
	@echo "  release       - Cross-compile for all platforms"
	@echo "  help          - Show this help"
