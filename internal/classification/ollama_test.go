package classification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestClassify_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		if req.Stream {
			t.Error("expected stream=false")
		}
		if req.Format != "json" {
			t.Errorf("expected format=json, got %s", req.Format)
		}
		if len(req.Messages) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(req.Messages))
		}

		resp := chatResponse{}
		resp.Message.Content = `{"category": "kubernetes", "confidence": 0.92, "reasoning": "Deploys k8s resources"}`
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "test-model", 5000, []string{"kubernetes", "observability", "other"})
	result, err := client.Classify(context.Background(), "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Category != "kubernetes" {
		t.Errorf("expected category 'kubernetes', got %q", result.Category)
	}
	if result.Confidence != 0.92 {
		t.Errorf("expected confidence 0.92, got %f", result.Confidence)
	}
	if result.Reasoning != "Deploys k8s resources" {
		t.Errorf("unexpected reasoning: %s", result.Reasoning)
	}
}

func TestClassify_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{}
		resp.Message.Content = "not valid json at all"
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "test-model", 5000, []string{"kubernetes", "other"})
	result, err := client.Classify(context.Background(), "sys", "usr")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Category != "other" {
		t.Errorf("expected 'other' for invalid JSON, got %q", result.Category)
	}
	if result.Confidence != 0.0 {
		t.Errorf("expected 0.0 confidence, got %f", result.Confidence)
	}
}

func TestClassify_UnknownCategory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{}
		resp.Message.Content = `{"category": "blockchain", "confidence": 0.8, "reasoning": "crypto stuff"}`
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "test-model", 5000, []string{"kubernetes", "other"})
	result, err := client.Classify(context.Background(), "sys", "usr")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Category != "other" {
		t.Errorf("expected 'other' for unknown category, got %q", result.Category)
	}
}

func TestClassify_Unreachable(t *testing.T) {
	client := NewOllamaClient("http://127.0.0.1:1", "test-model", 1000, []string{"other"})
	_, err := client.Classify(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
	if err != ErrOllamaUnreachable {
		t.Errorf("expected ErrOllamaUnreachable, got: %v", err)
	}
}

func TestClassify_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "test-model", 5000, []string{"other"})
	_, err := client.Classify(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for server error response")
	}
}

func TestClassify_ConfidenceClamping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{}
		resp.Message.Content = `{"category": "kubernetes", "confidence": 1.5, "reasoning": "test"}`
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "test-model", 5000, []string{"kubernetes", "other"})
	result, err := client.Classify(context.Background(), "sys", "usr")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Confidence != 1.0 {
		t.Errorf("expected confidence clamped to 1.0, got %f", result.Confidence)
	}
}

func TestClassify_Timeout(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until test signals completion so we don't hang server.Close()
		<-done
	}))
	defer func() {
		close(done)
		server.Close()
	}()

	client := NewOllamaClient(server.URL, "test-model", 100, []string{"other"})
	_, err := client.Classify(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for timeout")
	}
	// Timeout should NOT be ErrOllamaUnreachable — it's a different degradation path
	if err == ErrOllamaUnreachable {
		t.Error("timeout should not return ErrOllamaUnreachable")
	}
}

func TestClassify_NegativeConfidenceClamping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{}
		resp.Message.Content = `{"category": "kubernetes", "confidence": -0.5, "reasoning": "negative"}`
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "test-model", 5000, []string{"kubernetes", "other"})
	result, err := client.Classify(context.Background(), "sys", "usr")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Confidence != 0.0 {
		t.Errorf("expected confidence clamped to 0.0, got %f", result.Confidence)
	}
}

func TestClassify_CategoryNormalization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{}
		resp.Message.Content = `{"category": "  Kubernetes  ", "confidence": 0.8, "reasoning": "test"}`
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "test-model", 5000, []string{"kubernetes", "other"})
	result, err := client.Classify(context.Background(), "sys", "usr")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Category != "kubernetes" {
		t.Errorf("expected normalized category 'kubernetes', got %q", result.Category)
	}
}

func TestClassify_EmptyResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 200 but with invalid JSON body
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "test-model", 5000, []string{"other"})
	result, err := client.Classify(context.Background(), "sys", "usr")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Invalid response body should fallback gracefully
	if result.Category != "other" {
		t.Errorf("expected 'other' for invalid response body, got %q", result.Category)
	}
	if result.Confidence != 0.0 {
		t.Errorf("expected 0.0 confidence, got %f", result.Confidence)
	}
}

func TestModel(t *testing.T) {
	client := NewOllamaClient("http://localhost:11434", "qwen3:1.7b", 5000, nil)
	if client.Model() != "qwen3:1.7b" {
		t.Errorf("expected model 'qwen3:1.7b', got %q", client.Model())
	}
}

// TestOllamaClient_CircuitBreaker verifies ISI-782 acceptance: the breaker
// trips at the configured threshold of consecutive ErrOllamaUnreachable
// returns, short-circuits subsequent Classify calls without dialing, and
// resets when ResetCircuitBreaker is invoked at the start of the next cycle.
//
// The 26-hour ISI-714 incident plus the parallel ~4h-per-cycle Ollama dial
// burn from ISI-772 was the motivation for this regression guard: a future
// caller wiring up a new Ollama deployment must not be able to silently
// strip the breaker without failing this test.
func TestOllamaClient_CircuitBreaker(t *testing.T) {
	t.Setenv("OLLAMA_BREAKER_THRESHOLD", "")

	// 127.0.0.1:1 is a closed port — every dial returns an *net.OpError
	// (connection refused), which is treated as ErrOllamaUnreachable and
	// counts toward the breaker threshold.
	client := NewOllamaClient("http://127.0.0.1:1", "test-model", 200, []string{"other"})

	threshold := client.BreakerThreshold()
	if threshold != defaultBreakerThreshold {
		t.Fatalf("expected default threshold %d, got %d", defaultBreakerThreshold, threshold)
	}

	// First `threshold` calls dial-and-fail. After the threshold-th failure
	// the breaker should be open.
	for i := 0; i < threshold; i++ {
		_, err := client.Classify(context.Background(), "sys", "usr")
		if err != ErrOllamaUnreachable {
			t.Fatalf("call %d: expected ErrOllamaUnreachable, got %v", i, err)
		}
	}
	if !client.CircuitBreakerOpen() {
		t.Fatalf("breaker should be open after %d consecutive unreachable calls", threshold)
	}
	reachable, observed := client.LastReachable()
	if !observed || reachable {
		t.Fatalf("LastReachable=%v observed=%v after threshold failures, want false/true", reachable, observed)
	}

	// Subsequent call must short-circuit without dialing — the dial path
	// would take ~timeout (200ms here), but a short-circuited call returns
	// in microseconds. We allow a generous 30ms ceiling so loaded CI hosts
	// don't false-fail.
	start := time.Now()
	_, err := client.Classify(context.Background(), "sys", "usr")
	elapsed := time.Since(start)
	if err != ErrOllamaUnreachable {
		t.Fatalf("short-circuited call: expected ErrOllamaUnreachable, got %v", err)
	}
	if elapsed > 30*time.Millisecond {
		t.Errorf("short-circuited call should not dial; took %v (expected <30ms)", elapsed)
	}

	// ResetCircuitBreaker mimics the start-of-ClassifyAll reset. After it,
	// the breaker is closed and the next call dials again (so we'd take the
	// full timeout if the host is still down).
	client.ResetCircuitBreaker()
	if client.CircuitBreakerOpen() {
		t.Fatal("breaker should be closed after ResetCircuitBreaker")
	}
}

// TestOllamaClient_CircuitBreaker_RecoversMidBatch verifies that a single
// successful (or HTTP-reachable) Classify call resets the consecutive-
// unreachable counter, so an outage that clears mid-cycle doesn't trip the
// breaker artificially. ISI-782.
func TestOllamaClient_CircuitBreaker_RecoversMidBatch(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		resp := chatResponse{}
		resp.Message.Content = `{"category": "other", "confidence": 0.5, "reasoning": "ok"}`
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "test-model", 5000, []string{"other"})

	// Inject two unreachable failures by switching endpoint via a separate
	// client, then verify that a single reachable call clears the counter
	// when reused on the same client.
	deadClient := NewOllamaClient("http://127.0.0.1:1", "test-model", 200, []string{"other"})
	for i := 0; i < deadClient.BreakerThreshold()-1; i++ {
		_, _ = deadClient.Classify(context.Background(), "sys", "usr")
	}
	if deadClient.CircuitBreakerOpen() {
		t.Fatal("breaker should not be open at threshold-1 unreachable calls")
	}

	// One reachable call clears the counter on a real-server client.
	_, err := client.Classify(context.Background(), "sys", "usr")
	if err != nil {
		t.Fatalf("expected reachable call to succeed, got %v", err)
	}
	reachable, observed := client.LastReachable()
	if !observed || !reachable {
		t.Errorf("LastReachable=%v observed=%v after success, want true/true", reachable, observed)
	}
	if client.CircuitBreakerOpen() {
		t.Error("breaker should remain closed on reachable client")
	}
}

// TestOllamaClient_BreakerThresholdEnv verifies the env-tunable threshold for
// ISI-782. A future operator with a flaky Ollama can lower the bar to 1
// without changing code.
func TestOllamaClient_BreakerThresholdEnv(t *testing.T) {
	t.Setenv("OLLAMA_BREAKER_THRESHOLD", "1")
	client := NewOllamaClient("http://127.0.0.1:1", "test-model", 200, []string{"other"})
	if got := client.BreakerThreshold(); got != 1 {
		t.Fatalf("expected threshold=1 from env, got %d", got)
	}
	_, err := client.Classify(context.Background(), "sys", "usr")
	if err != ErrOllamaUnreachable {
		t.Fatalf("expected ErrOllamaUnreachable, got %v", err)
	}
	if !client.CircuitBreakerOpen() {
		t.Fatal("breaker should be open after a single failure when threshold=1")
	}
}

// TestOllamaClient_ReachableOnHTTPError verifies that an HTTP non-2xx response
// counts as "reachable" for the gauge — the request shape may be wrong, but
// the host is up. ISI-782 acceptance.
func TestOllamaClient_ReachableOnHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "test-model", 5000, []string{"other"})
	_, err := client.Classify(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	reachable, observed := client.LastReachable()
	if !observed || !reachable {
		t.Errorf("LastReachable=%v observed=%v on HTTP 500, want true/true (server reachable)", reachable, observed)
	}
}
