package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_DoWithRetry_Success(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/test", nil)
	resp, err := client.DoWithRetry(req)
	if err != nil {
		t.Fatalf("DoWithRetry error: %v", err)
	}
	defer resp.Body.Close()

	if atomic.LoadInt32(&requestCount) != 1 {
		t.Errorf("request count = %d, want 1", requestCount)
	}
}

func TestClient_DoWithRetry_RetryOn500(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)
	client.SetRetryConfig(RetryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
		MaxDelay:   100 * time.Millisecond,
	})

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/test", nil)
	resp, err := client.DoWithRetry(req)
	if err != nil {
		t.Fatalf("DoWithRetry error: %v", err)
	}
	defer resp.Body.Close()

	if atomic.LoadInt32(&requestCount) != 3 {
		t.Errorf("request count = %d, want 3", requestCount)
	}
}

func TestClient_DoWithRetry_MaxRetriesExceeded(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)
	client.SetRetryConfig(RetryConfig{
		MaxRetries: 2,
		BaseDelay:  10 * time.Millisecond,
		MaxDelay:   100 * time.Millisecond,
	})

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/test", nil)
	_, err := client.DoWithRetry(req)
	if err == nil {
		t.Fatal("expected error after max retries")
	}

	// Should have made 1 initial + 2 retries = 3 requests
	if atomic.LoadInt32(&requestCount) != 3 {
		t.Errorf("request count = %d, want 3", requestCount)
	}
}

func TestClient_DoWithRetry_NoRetryOn404(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)
	client.SetRetryConfig(RetryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
	})

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/test", nil)
	resp, err := client.DoWithRetry(req)
	if err != nil {
		t.Fatalf("DoWithRetry error: %v", err)
	}
	defer resp.Body.Close()

	// Should NOT retry on 404
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Errorf("request count = %d, want 1 (no retry on 404)", requestCount)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestClient_DoWithRetry_RetryOn429(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		if count == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)
	client.SetRetryConfig(RetryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
	})

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/test", nil)
	resp, err := client.DoWithRetry(req)
	if err != nil {
		t.Fatalf("DoWithRetry error: %v", err)
	}
	defer resp.Body.Close()

	if atomic.LoadInt32(&requestCount) != 2 {
		t.Errorf("request count = %d, want 2", requestCount)
	}
}

func TestClient_DoWithRetry_OnRetryCallback(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		if count < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var retryAttempts []int
	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)
	client.SetRetryConfig(RetryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
		OnRetry: func(attempt int, err error, delay time.Duration) {
			retryAttempts = append(retryAttempts, attempt)
		},
	})

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/test", nil)
	resp, err := client.DoWithRetry(req)
	if err != nil {
		t.Fatalf("DoWithRetry error: %v", err)
	}
	defer resp.Body.Close()

	if len(retryAttempts) != 2 {
		t.Errorf("retry attempts = %v, want [1, 2]", retryAttempts)
	}
}

func TestClient_DoWithRetry_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)
	client.SetRetryConfig(RetryConfig{
		MaxRetries: 10,
		BaseDelay:  100 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/test", nil)
	_, err := client.DoWithRetry(req)
	if err != context.Canceled {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestIsRetryableStatusCode(t *testing.T) {
	tests := []struct {
		code      int
		retryable bool
	}{
		{200, false},
		{201, false},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{504, true},
	}

	for _, tc := range tests {
		got := isRetryableStatusCode(tc.code)
		if got != tc.retryable {
			t.Errorf("isRetryableStatusCode(%d) = %v, want %v", tc.code, got, tc.retryable)
		}
	}
}

func TestCalculateBackoff(t *testing.T) {
	baseDelay := 100 * time.Millisecond
	maxDelay := 1 * time.Second

	// Test exponential growth
	delay1 := calculateBackoff(1, baseDelay, maxDelay)
	delay2 := calculateBackoff(2, baseDelay, maxDelay)
	delay3 := calculateBackoff(3, baseDelay, maxDelay)

	// Delays should generally increase (accounting for jitter)
	if delay2 < delay1/2 {
		t.Errorf("delay2 (%v) should be larger than delay1 (%v)", delay2, delay1)
	}
	if delay3 < delay2/2 {
		t.Errorf("delay3 (%v) should be larger than delay2 (%v)", delay3, delay2)
	}

	// Should not exceed maxDelay
	delay10 := calculateBackoff(10, baseDelay, maxDelay)
	if delay10 > maxDelay+maxDelay/2 { // Account for jitter
		t.Errorf("delay10 (%v) should not greatly exceed maxDelay (%v)", delay10, maxDelay)
	}
}

func TestIsPermanentError(t *testing.T) {
	tests := []struct {
		err       error
		permanent bool
	}{
		{&HTTPError{StatusCode: 400}, true},
		{&HTTPError{StatusCode: 401}, true},
		{&HTTPError{StatusCode: 403}, true},
		{&HTTPError{StatusCode: 404}, true},
		{&HTTPError{StatusCode: 429}, false}, // Rate limit is retryable
		{&HTTPError{StatusCode: 500}, false},
		{&HTTPError{StatusCode: 503}, false},
		{nil, false},
	}

	for _, tc := range tests {
		got := IsPermanentError(tc.err)
		if got != tc.permanent {
			t.Errorf("IsPermanentError(%v) = %v, want %v", tc.err, got, tc.permanent)
		}
	}
}
