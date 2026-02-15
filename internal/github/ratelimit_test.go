package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_ShouldBackoff(t *testing.T) {
	tests := []struct {
		name       string
		limit      int
		remaining  int
		threshold  int
		wantBackoff bool
	}{
		{"above threshold", 5000, 200, 100, false},
		{"at threshold", 5000, 100, 100, false},
		{"below threshold", 5000, 50, 100, true},
		{"zero remaining", 5000, 0, 100, true},
		{"no rate info yet", 0, 0, 100, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := NewClient("test-token")
			client.rateLimit = RateLimit{
				Limit:     tc.limit,
				Remaining: tc.remaining,
			}
			client.SetRateLimitOptions(RateLimitOptions{
				Threshold: tc.threshold,
			})

			got := client.ShouldBackoff()
			if got != tc.wantBackoff {
				t.Errorf("ShouldBackoff() = %v, want %v", got, tc.wantBackoff)
			}
		})
	}
}

func TestClient_IsRateLimitExhausted(t *testing.T) {
	tests := []struct {
		name      string
		limit     int
		remaining int
		want      bool
	}{
		{"exhausted", 5000, 0, true},
		{"not exhausted", 5000, 100, false},
		{"no info yet", 0, 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := NewClient("test-token")
			client.rateLimit = RateLimit{
				Limit:     tc.limit,
				Remaining: tc.remaining,
			}

			got := client.IsRateLimitExhausted()
			if got != tc.want {
				t.Errorf("IsRateLimitExhausted() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClient_TimeUntilReset(t *testing.T) {
	client, _ := NewClient("test-token")

	// No reset time set
	if d := client.TimeUntilReset(); d != 0 {
		t.Errorf("TimeUntilReset() with no reset = %v, want 0", d)
	}

	// Reset in the past
	client.rateLimit.Reset = time.Now().Add(-1 * time.Hour)
	if d := client.TimeUntilReset(); d != 0 {
		t.Errorf("TimeUntilReset() with past reset = %v, want 0", d)
	}

	// Reset in the future
	future := time.Now().Add(10 * time.Minute)
	client.rateLimit.Reset = future
	d := client.TimeUntilReset()
	if d < 9*time.Minute || d > 11*time.Minute {
		t.Errorf("TimeUntilReset() = %v, want ~10min", d)
	}
}

func TestClient_WaitForRateLimit_NotExhausted(t *testing.T) {
	client, _ := NewClient("test-token")
	client.rateLimit = RateLimit{
		Limit:     5000,
		Remaining: 100,
	}

	// Should return immediately
	start := time.Now()
	err := client.WaitForRateLimit(context.Background())
	if err != nil {
		t.Errorf("WaitForRateLimit error: %v", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Error("WaitForRateLimit should return immediately when not exhausted")
	}
}

func TestClient_WaitForRateLimit_ContextCancellation(t *testing.T) {
	client, _ := NewClient("test-token")
	client.rateLimit = RateLimit{
		Limit:     5000,
		Remaining: 0,
		Reset:     time.Now().Add(1 * time.Hour),
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := client.WaitForRateLimit(ctx)
	if err != context.Canceled {
		t.Errorf("WaitForRateLimit error = %v, want context.Canceled", err)
	}
}

func TestClient_OnRateLimitWarning(t *testing.T) {
	var warningCalled int32

	resetTime := time.Now().Add(30 * time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "50")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)
	client.SetRateLimitOptions(RateLimitOptions{
		Threshold: 100,
		OnWarning: func(remaining int, reset time.Time) {
			atomic.AddInt32(&warningCalled, 1)
		},
	})

	// First request to set the rate limit
	_, _ = client.Get(context.Background(), "/test")

	// Now check rate limit (this would be done before requests)
	_ = client.checkRateLimit(context.Background())

	if atomic.LoadInt32(&warningCalled) == 0 {
		t.Error("OnWarning callback was not called")
	}
}

func TestRateLimitError(t *testing.T) {
	reset := time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC)
	err := &RateLimitError{Reset: reset}

	if !IsRateLimitError(err) {
		t.Error("IsRateLimitError returned false for RateLimitError")
	}

	if IsRateLimitError(context.Canceled) {
		t.Error("IsRateLimitError returned true for non-RateLimitError")
	}

	expectedMsg := "rate limit exhausted, resets at 2026-02-15T12:00:00Z"
	if err.Error() != expectedMsg {
		t.Errorf("Error() = %q, want %q", err.Error(), expectedMsg)
	}
}

func TestClient_SetRateLimitOptions_DefaultThreshold(t *testing.T) {
	client, _ := NewClient("test-token")

	// Set with zero threshold should use default
	client.SetRateLimitOptions(RateLimitOptions{Threshold: 0})

	// Force rate limit info
	client.rateLimit = RateLimit{
		Limit:     5000,
		Remaining: 50, // Below default threshold of 100
	}

	if !client.ShouldBackoff() {
		t.Error("ShouldBackoff() should use default threshold")
	}
}
