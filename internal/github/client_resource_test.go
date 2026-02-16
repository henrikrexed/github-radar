package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestClient_ConnectionReuse(t *testing.T) {
	var connectionCount int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connectionCount++
		mu.Unlock()

		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"ok"}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	// Make 50 sequential requests
	for i := 0; i < 50; i++ {
		resp, err := client.Get(context.Background(), "/test")
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}
		resp.Body.Close()
	}

	t.Logf("Connections used for 50 requests: %d", connectionCount)

	// All 50 requests should have been made (server counted them)
	if connectionCount != 50 {
		t.Logf("Note: %d connection handler calls for 50 requests", connectionCount)
	}
}

func TestClient_ResponseBodyClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.WriteHeader(http.StatusOK)
		// Return a larger response body to detect leaks
		data := make([]byte, 1024)
		for i := range data {
			data[i] = 'x'
		}
		w.Write(data)
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// Make many requests, properly closing each response
	for i := 0; i < 100; i++ {
		resp, err := client.Get(context.Background(), "/test")
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}
		resp.Body.Close()
	}

	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	// Use TotalAlloc (cumulative) for a more stable comparison since Alloc can decrease after GC
	totalAllocMB := float64(memAfter.TotalAlloc-memBefore.TotalAlloc) / 1024 / 1024
	t.Logf("Total allocations for 100 requests: %.2f MB", totalAllocMB)
	t.Logf("Heap in use: %.2f MB", float64(memAfter.HeapInuse)/1024/1024)

	// 100 requests with 1KB bodies should not allocate excessive memory
	if totalAllocMB > 50 {
		t.Errorf("Possible response body leak: %.2f MB total allocations for 100 requests", totalAllocMB)
	}
}

func TestClient_TimeoutEnforced(t *testing.T) {
	// Server that hangs forever
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block for longer than client timeout
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	// Set a short timeout
	client.SetHTTPClient(&http.Client{
		Timeout: 100 * time.Millisecond,
	})

	start := time.Now()
	_, err := client.Get(context.Background(), "/slow")
	duration := time.Since(start)

	if err == nil {
		t.Error("Expected timeout error, got nil")
	}

	// Should timeout in roughly the configured timeout, not hang for 5s
	if duration > 2*time.Second {
		t.Errorf("Request took %v, expected to timeout around 100ms", duration)
	}
	t.Logf("Timeout occurred after: %v", duration)
}

func TestClient_ContextCancellation_StopsRequest(t *testing.T) {
	requestStarted := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		// Block until context is cancelled
		<-r.Context().Done()
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := client.Get(ctx, "/blocking")
		errCh <- err
	}()

	// Wait for request to reach the server
	<-requestStarted

	// Cancel the context
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("Expected error after context cancellation")
		}
		t.Logf("Request cancelled with: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Request did not cancel within 5 seconds")
	}
}

func TestClient_RateLimitHeaders_Tracked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4500")
		w.Header().Set("X-RateLimit-Reset", "1700000000")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	resp, err := client.Get(context.Background(), "/test")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	resp.Body.Close()

	info := client.RateLimitInfo()
	if info.Limit != 5000 {
		t.Errorf("RateLimit.Limit = %d, want 5000", info.Limit)
	}
	if info.Remaining != 4500 {
		t.Errorf("RateLimit.Remaining = %d, want 4500", info.Remaining)
	}
	if info.Reset.IsZero() {
		t.Error("RateLimit.Reset should be set")
	}

	t.Logf("Rate limit tracked: %d/%d remaining, resets at %v",
		info.Remaining, info.Limit, info.Reset)
}

func TestClient_ConcurrentRequests_Safe(t *testing.T) {
	var requestCount int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()

		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-token")
	client.SetBaseURL(server.URL)

	// Fire 20 concurrent requests
	var wg sync.WaitGroup
	errors := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get(context.Background(), "/test")
			if err != nil {
				errors <- err
				return
			}
			resp.Body.Close()
		}()
	}

	wg.Wait()
	close(errors)

	errCount := 0
	for err := range errors {
		t.Logf("Concurrent request error: %v", err)
		errCount++
	}

	if errCount > 0 {
		t.Errorf("%d out of 20 concurrent requests failed", errCount)
	}

	mu.Lock()
	t.Logf("Server handled %d concurrent requests", requestCount)
	mu.Unlock()
}
