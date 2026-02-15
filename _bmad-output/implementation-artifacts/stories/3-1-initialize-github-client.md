# Story 3.1: Initialize GitHub API Client

Status: done

## Story

As the **system**,
I want a configured GitHub API client with authentication,
So that all API operations use proper credentials and rate limiting.

## Acceptance Criteria

1. **Given** a GitHub token in configuration/environment
   **When** the client is initialized
   **Then** authentication is configured for REST API v3 (NFR1)

2. **Given** the client is created
   **When** making requests
   **Then** rate limit tracking is enabled

3. **Given** any API request
   **When** the request is made
   **Then** HTTPS is enforced for all requests (NFR9)

4. **Given** a token
   **When** validating the client
   **Then** a test API call verifies the token works

## Tasks / Subtasks

- [x] Task 1: Create GitHub client package (AC: #1, #3)
  - [x] 1.1: Create internal/github/client.go
  - [x] 1.2: Define Client struct with http.Client and token
  - [x] 1.3: Implement NewClient constructor with token validation

- [x] Task 2: Implement rate limit tracking (AC: #2)
  - [x] 2.1: Parse X-RateLimit headers from responses
  - [x] 2.2: Store remaining/reset time
  - [x] 2.3: Expose RateLimitInfo() method

- [x] Task 3: Implement token validation (AC: #4)
  - [x] 3.1: Call /rate_limit endpoint to verify token
  - [x] 3.2: Return clear error for invalid tokens
  - [x] 3.3: Rate limit info available via RateLimitInfo()

- [x] Task 4: Write tests
  - [x] 4.1: Test client creation
  - [x] 4.2: Test rate limit parsing
  - [x] 4.3: Test error handling

## Dev Notes

### Client Interface

```go
type Client struct {
    httpClient *http.Client
    token      string
    baseURL    string
    rateLimit  RateLimit
    mu         sync.RWMutex
}

type RateLimit struct {
    Limit     int
    Remaining int
    Reset     time.Time
}

func NewClient(token string) (*Client, error)
func (c *Client) ValidateToken(ctx context.Context) error
func (c *Client) RateLimit() RateLimit
```

### GitHub API Headers

```
Authorization: Bearer <token>
Accept: application/vnd.github.v3+json
User-Agent: github-radar/1.0
```

### Rate Limit Headers

```
X-RateLimit-Limit: 5000
X-RateLimit-Remaining: 4999
X-RateLimit-Reset: 1609459200 (Unix timestamp)
```

### References

- [GitHub REST API v3](https://docs.github.com/en/rest)
- [Source: _bmad-output/planning-artifacts/epics.md#Story 3.1]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### Debug Log References

### Completion Notes List

- Implemented Client struct with thread-safe rate limit tracking (sync.RWMutex)
- NewClient validates token is non-empty
- ValidateToken calls /rate_limit endpoint and returns clear error for invalid tokens
- Rate limit headers (X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset) parsed from all responses
- All requests use HTTPS (DefaultBaseURL = "https://api.github.com")
- Comprehensive test coverage using httptest for mocking

### File List

- internal/github/client.go (new)
- internal/github/client_test.go (new)

