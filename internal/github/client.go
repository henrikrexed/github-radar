// Package github provides the GitHub API client wrapper for github-radar.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultBaseURL is the GitHub API base URL.
	DefaultBaseURL = "https://api.github.com"

	// UserAgent identifies this client to GitHub.
	UserAgent = "github-radar/1.0"

	// DefaultTimeout is the default HTTP request timeout.
	DefaultTimeout = 30 * time.Second
)

// RateLimit contains GitHub API rate limit information.
type RateLimit struct {
	Limit     int       // Maximum requests per hour
	Remaining int       // Remaining requests in current window
	Reset     time.Time // When the rate limit resets
}

// Client is a GitHub API client with rate limit tracking.
type Client struct {
	httpClient    *http.Client
	token         string
	baseURL       string
	rateLimit     RateLimit
	rateLimitOpts RateLimitOptions
	retryConfig   RetryConfig
	mu            sync.RWMutex
}

// NewClient creates a new GitHub API client.
// The token must not be empty.
func NewClient(token string) (*Client, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("github token cannot be empty")
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		token:   token,
		baseURL: DefaultBaseURL,
	}, nil
}

// RateLimitInfo returns the current rate limit information.
// This is safe for concurrent use.
func (c *Client) RateLimitInfo() RateLimit {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.rateLimit
}

// ValidateToken verifies the token works by calling the rate_limit endpoint.
// It also populates the initial rate limit information.
func (c *Client) ValidateToken(ctx context.Context) error {
	req, err := c.newRequest(ctx, http.MethodGet, "/rate_limit")
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	// Update rate limits from response headers
	c.updateRateLimitFromHeaders(resp.Header)

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid or expired token")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Do executes an HTTP request with authentication and rate limit tracking.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	// Check rate limit before making request
	if err := c.checkRateLimit(req.Context()); err != nil {
		return nil, err
	}

	// Add authentication header if not already present
	if req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	// Add standard headers
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/vnd.github.v3+json")
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", UserAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	// Update rate limits from response
	c.updateRateLimitFromHeaders(resp.Header)

	return resp, nil
}

// Get performs a GET request to the GitHub API.
func (c *Client) Get(ctx context.Context, path string) (*http.Response, error) {
	req, err := c.newRequest(ctx, http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

// GetJSON performs a GET request and decodes the response as JSON.
func (c *Client) GetJSON(ctx context.Context, path string, v interface{}) error {
	resp, err := c.Get(ctx, path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	return nil
}

// APIError represents a GitHub API error response.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("GitHub API error %d: %s", e.StatusCode, e.Message)
}

// IsAPINotFound checks if an error is a 404 API error.
func IsAPINotFound(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return false
}

// newRequest creates a new HTTP request with authentication headers.
func (c *Client) newRequest(ctx context.Context, method, path string) (*http.Request, error) {
	url := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", UserAgent)

	return req, nil
}

// updateRateLimitFromHeaders parses rate limit headers from the response.
func (c *Client) updateRateLimitFromHeaders(headers http.Header) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if limit := headers.Get("X-RateLimit-Limit"); limit != "" {
		if v, err := strconv.Atoi(limit); err == nil {
			c.rateLimit.Limit = v
		}
	}

	if remaining := headers.Get("X-RateLimit-Remaining"); remaining != "" {
		if v, err := strconv.Atoi(remaining); err == nil {
			c.rateLimit.Remaining = v
		}
	}

	if reset := headers.Get("X-RateLimit-Reset"); reset != "" {
		if v, err := strconv.ParseInt(reset, 10, 64); err == nil {
			c.rateLimit.Reset = time.Unix(v, 0)
		}
	}
}

// BaseURL returns the configured base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// SetBaseURL sets a custom base URL (useful for testing).
func (c *Client) SetBaseURL(url string) {
	c.baseURL = strings.TrimSuffix(url, "/")
}

// SetHTTPClient sets a custom HTTP client (useful for testing).
func (c *Client) SetHTTPClient(client *http.Client) {
	c.httpClient = client
}

// decodeJSON decodes a JSON response body into the target value.
func decodeJSON(body io.Reader, v interface{}) error {
	return json.NewDecoder(body).Decode(v)
}
