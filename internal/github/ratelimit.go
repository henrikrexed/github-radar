package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// DefaultRateLimitThreshold is the default threshold (remaining requests)
// below which the client will start backing off.
const DefaultRateLimitThreshold = 100

// RateLimitWarningFunc is a callback function called when rate limit is low.
type RateLimitWarningFunc func(remaining int, reset time.Time)

// RateLimitOptions configures rate limit handling behavior.
type RateLimitOptions struct {
	// Threshold is the number of remaining requests below which
	// the client will back off. Default: 100.
	Threshold int

	// WaitOnExhaustion causes the client to wait until rate limit resets
	// rather than returning an error. Default: false.
	WaitOnExhaustion bool

	// OnWarning is called when remaining requests fall below threshold.
	OnWarning RateLimitWarningFunc
}

// SetRateLimitOptions configures rate limit handling for the client.
func (c *Client) SetRateLimitOptions(opts RateLimitOptions) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if opts.Threshold <= 0 {
		opts.Threshold = DefaultRateLimitThreshold
	}

	c.rateLimitOpts = opts
}

// ShouldBackoff returns true if the client should slow down due to rate limits.
func (c *Client) ShouldBackoff() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	threshold := c.rateLimitOpts.Threshold
	if threshold <= 0 {
		threshold = DefaultRateLimitThreshold
	}

	// If we haven't received rate limit info yet, don't back off
	if c.rateLimit.Limit == 0 {
		return false
	}

	return c.rateLimit.Remaining < threshold
}

// IsRateLimitExhausted returns true if rate limit is completely exhausted.
// Treats a reset timestamp that has already passed as "not exhausted" and
// clears stale state so the next request fetches a fresh rate-limit header.
func (c *Client) IsRateLimitExhausted() bool {
	c.mu.RLock()
	stale := !c.rateLimit.Reset.IsZero() && c.rateLimit.Reset.Before(time.Now())
	exhausted := c.rateLimit.Remaining == 0 && c.rateLimit.Limit > 0
	c.mu.RUnlock()

	if !stale {
		return exhausted
	}

	c.mu.Lock()
	if !c.rateLimit.Reset.IsZero() && c.rateLimit.Reset.Before(time.Now()) {
		c.rateLimit.Remaining = 0
		c.rateLimit.Limit = 0
	}
	c.mu.Unlock()
	return false
}

// TimeUntilReset returns the duration until the rate limit resets.
// Returns 0 if the rate limit has already reset or is unknown.
func (c *Client) TimeUntilReset() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.rateLimit.Reset.IsZero() {
		return 0
	}

	duration := time.Until(c.rateLimit.Reset)
	if duration < 0 {
		return 0
	}

	return duration
}

const rateLimitPollInterval = 30 * time.Second

// WaitForRateLimit polls until the rate limit resets, checking every
// rateLimitPollInterval (30 s). This replaces the previous single-shot
// time.After that could block for 9+ minutes with zero log output
// (ISI-1025 Issue 1). Each poll iteration logs the remaining wait so
// operators can see active backoff in journalctl output.
func (c *Client) WaitForRateLimit(ctx context.Context) error {
	if !c.IsRateLimitExhausted() {
		return nil
	}

	waitDuration := c.TimeUntilReset()
	if waitDuration == 0 {
		return nil
	}

	deadline := time.Now().Add(waitDuration + 5*time.Second)
	poll := rateLimitPollInterval
	if waitDuration < poll {
		poll = waitDuration
	}

	slog.Info("rate limit exhausted, backing off until reset",
		"wait_seconds", int(waitDuration.Seconds()),
		"poll_interval", poll.String())

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}

		if !c.IsRateLimitExhausted() {
			slog.Info("rate limit refreshed, resuming")
			return nil
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			slog.Warn("rate limit poll exceeded deadline; clearing stale state and resuming")
			c.mu.Lock()
			c.rateLimit.Remaining = 0
			c.rateLimit.Limit = 0
			c.mu.Unlock()
			return nil
		}

		next := rateLimitPollInterval
		if remaining < next {
			next = remaining
		}
		slog.Info("rate limit still exhausted, next poll",
			"remaining_seconds", int(remaining.Seconds()))
		poll = next
	}
}

// checkRateLimit checks rate limit before a request and handles accordingly.
func (c *Client) checkRateLimit(ctx context.Context) error {
	// Trigger warning callback if below threshold
	if c.ShouldBackoff() {
		c.mu.RLock()
		opts := c.rateLimitOpts
		rl := c.rateLimit
		c.mu.RUnlock()

		if opts.OnWarning != nil {
			opts.OnWarning(rl.Remaining, rl.Reset)
		}
	}

	// If exhausted and configured to wait, wait for reset
	if c.IsRateLimitExhausted() {
		c.mu.RLock()
		waitOnExhaustion := c.rateLimitOpts.WaitOnExhaustion
		c.mu.RUnlock()

		if waitOnExhaustion {
			if err := c.WaitForRateLimit(ctx); err != nil {
				return fmt.Errorf("waiting for rate limit: %w", err)
			}
		} else {
			c.mu.RLock()
			reset := c.rateLimit.Reset
			c.mu.RUnlock()
			return &RateLimitError{
				Reset: reset,
			}
		}
	}

	return nil
}

// RateLimitError is returned when rate limit is exhausted.
type RateLimitError struct {
	Reset time.Time
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limit exhausted, resets at %s", e.Reset.Format(time.RFC3339))
}

// IsRateLimitError checks if an error is a rate limit error.
func IsRateLimitError(err error) bool {
	var rlErr *RateLimitError
	return errors.As(err, &rlErr)
}
