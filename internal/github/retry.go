package github

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
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
	// nextDelayOverride, when non-zero, replaces the next computed
	// exponential-backoff delay. Set when a server responds with a
	// Retry-After header (RFC 7231 §7.1.3) so we honour the contract
	// instead of stomping on it with our local schedule.
	var nextDelayOverride time.Duration
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

			// Wait with exponential backoff, taking max(backoff, Retry-After)
			// when the server supplied one.
			delay := calculateBackoff(attempt, config.BaseDelay, config.MaxDelay)
			if nextDelayOverride > delay {
				delay = nextDelayOverride
			}
			nextDelayOverride = 0

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
		if isRetryableStatusCode(resp.StatusCode) || isRateLimitedExhaustion(resp) {
			// GitHub's secondary rate limit returns 429 + Retry-After.
			// The primary rate limit returns 403 + X-RateLimit-Remaining: 0,
			// often with Retry-After as well. Honour the header in both cases.
			if d, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
				if d > config.MaxDelay {
					d = config.MaxDelay
				}
				nextDelayOverride = d
			}
			resp.Body.Close()
			lastErr = &HTTPError{StatusCode: resp.StatusCode}
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// isRateLimitedExhaustion reports whether the response is a primary
// GitHub rate-limit exhaustion (403 with X-RateLimit-Remaining: 0).
// These are retryable when paired with a Retry-After (or after the
// reset window), unlike a generic 403 which is permanent.
func isRateLimitedExhaustion(resp *http.Response) bool {
	if resp.StatusCode != http.StatusForbidden {
		return false
	}
	if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining == "0" {
		return true
	}
	return false
}

// parseRetryAfter parses the Retry-After header value per RFC 7231 §7.1.3.
// It accepts either delta-seconds (a non-negative integer) or an HTTP-date.
// The second form is computed relative to `now` so callers can inject a
// clock for tests. Returns (0, false) if the value is empty or unparseable.
func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	if value == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(value); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	// HTTP-date forms accepted per RFC 7231: IMF-fixdate, RFC850, asctime.
	if t, err := http.ParseTime(value); err == nil {
		d := t.Sub(now)
		if d < 0 {
			return 0, true
		}
		return d, true
	}
	return 0, false
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
