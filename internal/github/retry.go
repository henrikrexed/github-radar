package github

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"time"
)

// DefaultMaxRetries is the default number of retry attempts.
const DefaultMaxRetries = 3

// DefaultBaseDelay is the base delay for exponential backoff.
const DefaultBaseDelay = 1 * time.Second

// DefaultMaxDelay is the maximum delay between retries.
const DefaultMaxDelay = 30 * time.Second

// RetryConfig configures retry behavior.
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts (default: 3)
	MaxRetries int

	// BaseDelay is the initial delay before first retry (default: 1s)
	BaseDelay time.Duration

	// MaxDelay is the maximum delay between retries (default: 30s)
	MaxDelay time.Duration

	// OnRetry is called before each retry attempt
	OnRetry func(attempt int, err error, delay time.Duration)
}

// DefaultRetryConfig returns the default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: DefaultMaxRetries,
		BaseDelay:  DefaultBaseDelay,
		MaxDelay:   DefaultMaxDelay,
	}
}

// SetRetryConfig sets the retry configuration for the client.
func (c *Client) SetRetryConfig(config RetryConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if config.MaxRetries <= 0 {
		config.MaxRetries = DefaultMaxRetries
	}
	if config.BaseDelay <= 0 {
		config.BaseDelay = DefaultBaseDelay
	}
	if config.MaxDelay <= 0 {
		config.MaxDelay = DefaultMaxDelay
	}

	c.retryConfig = config
}

// DoWithRetry executes a request with automatic retry for transient failures.
// Note: This method only supports GET requests or requests without a body.
// Requests with a body cannot be retried because the body is consumed on the first attempt.
func (c *Client) DoWithRetry(req *http.Request) (*http.Response, error) {
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		return nil, fmt.Errorf("DoWithRetry: requests with body must have GetBody set for retry support")
	}
	c.mu.RLock()
	config := c.retryConfig
	c.mu.RUnlock()

	// Use defaults if not configured
	if config.MaxRetries == 0 {
		config = DefaultRetryConfig()
	}

	var lastErr error
	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Reset request body for retry if needed
			if req.GetBody != nil {
				body, err := req.GetBody()
				if err != nil {
					return nil, fmt.Errorf("resetting request body for retry: %w", err)
				}
				req.Body = body
			}

			// Wait with exponential backoff
			delay := calculateBackoff(attempt, config.BaseDelay, config.MaxDelay)

			if config.OnRetry != nil {
				config.OnRetry(attempt, lastErr, delay)
			}

			select {
			case <-time.After(delay):
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}

		resp, err := c.Do(req)
		if err != nil {
			if isRetryableError(err) {
				lastErr = err
				continue
			}
			return nil, err
		}

		// Check if response indicates a retryable error
		if isRetryableStatusCode(resp.StatusCode) {
			resp.Body.Close()
			lastErr = &HTTPError{StatusCode: resp.StatusCode}
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// GetWithRetry performs a GET request with automatic retry.
func (c *Client) GetWithRetry(ctx context.Context, path string) (*http.Response, error) {
	req, err := c.newRequest(ctx, http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	return c.DoWithRetry(req)
}

// HTTPError represents an HTTP error response.
type HTTPError struct {
	StatusCode int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d", e.StatusCode)
}

// calculateBackoff calculates the delay for a retry attempt using exponential backoff with jitter.
func calculateBackoff(attempt int, baseDelay, maxDelay time.Duration) time.Duration {
	// Exponential backoff: baseDelay * 2^attempt
	delay := baseDelay * (1 << uint(attempt-1))

	// Cap at maxDelay
	if delay > maxDelay {
		delay = maxDelay
	}

	// Add jitter (±25%) using math/rand/v2 (auto-seeded, no global state issues)
	jitterRange := int64(delay / 2)
	if jitterRange > 0 {
		jitter := time.Duration(rand.Int64N(jitterRange))
		if rand.IntN(2) == 0 {
			delay += jitter / 2
		} else {
			delay -= jitter / 2
		}
	}

	return delay
}

// isRetryableError checks if an error is transient and should be retried.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Context errors are not retryable
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// DNS errors are retryable (temporary network issues)
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return dnsErr.IsTemporary || dnsErr.IsTimeout
	}

	// Network operation errors (connection refused, reset, etc.) are retryable
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}

	// Timeout errors are retryable
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	return false
}

// isRetryableStatusCode checks if an HTTP status code indicates a retryable error.
func isRetryableStatusCode(code int) bool {
	switch code {
	case http.StatusTooManyRequests: // 429 - Rate limited
		return true
	case http.StatusInternalServerError: // 500
		return true
	case http.StatusBadGateway: // 502
		return true
	case http.StatusServiceUnavailable: // 503
		return true
	case http.StatusGatewayTimeout: // 504
		return true
	default:
		return false
	}
}

// IsNotFoundError checks if an error indicates a 404 response.
func IsNotFoundError(err error) bool {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == http.StatusNotFound
	}
	return false
}

// IsPermanentError checks if an error is permanent (should not retry).
func IsPermanentError(err error) bool {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		// 4xx errors except 429 are permanent
		return httpErr.StatusCode >= 400 && httpErr.StatusCode < 500 &&
			httpErr.StatusCode != http.StatusTooManyRequests
	}
	return false
}
