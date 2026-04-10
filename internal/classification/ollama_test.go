package classification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
