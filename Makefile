.PHONY: build test lint clean run docker

# Build variables
VERSION ?= dev
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

# Build the binary
build:
	go build $(LDFLAGS) -o bin/github-radar ./cmd/github-radar

# Run tests
test:
	go test ./...

# Run tests with coverage
test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Run linters
lint:
	go vet ./...

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

# Run the binary
run: build
	./bin/github-radar

# Build Docker image
docker:
	docker build -t github-radar:$(VERSION) .

# Cross-compile for all platforms
release:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/github-radar-linux-amd64 ./cmd/github-radar
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/github-radar-linux-arm64 ./cmd/github-radar
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/github-radar-darwin-amd64 ./cmd/github-radar
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/github-radar-darwin-arm64 ./cmd/github-radar
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/github-radar-windows-amd64.exe ./cmd/github-radar
