// Package classification provides LLM-based repository classification using Ollama.
package classification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// defaultBreakerThreshold is the default consecutive ErrOllamaUnreachable count
// at which OllamaClient.Classify starts short-circuiting. Tunable via env var
// OLLAMA_BREAKER_THRESHOLD (positive integer). See ISI-782.
const defaultBreakerThreshold = 3

// OllamaClient communicates with the Ollama /api/chat endpoint.
type OllamaClient struct {
	endpoint   string
	model      string
	httpClient *http.Client
	categories map[string]bool

	// breakerThreshold is the consecutive ErrOllamaUnreachable count after
	// which Classify short-circuits without dialing. ISI-782.
	breakerThreshold int

	// mu guards the breaker + reachability state below.
	mu                     sync.Mutex
	consecutiveUnreachable int
	breakerOpen            bool
	lastReachable          bool
	haveReachable          bool
}

// NewOllamaClient creates a client for the given Ollama endpoint, model, and timeout.
// categories is the allowed list of classification categories.
func NewOllamaClient(endpoint, model string, timeoutMs int, categories []string) *OllamaClient {
	catMap := make(map[string]bool, len(categories))
	for _, c := range categories {
		catMap[c] = true
	}
	threshold := defaultBreakerThreshold
	if v := os.Getenv("OLLAMA_BREAKER_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			threshold = n
		}
	}
	return &OllamaClient{
		endpoint: strings.TrimSuffix(endpoint, "/"),
		model:    model,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutMs) * time.Millisecond,
		},
		categories:       catMap,
		breakerThreshold: threshold,
	}
}

// chatRequest is the Ollama /api/chat request body.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Format   string        `json:"format"`
}

// chatMessage is a single message in the Ollama chat request.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatResponse is the relevant fields from the Ollama /api/chat response.
type chatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

// ClassificationResult holds the parsed LLM classification output.
type ClassificationResult struct {
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

// ErrOllamaUnreachable indicates the Ollama server could not be reached.
var ErrOllamaUnreachable = errors.New("ollama server unreachable")

// Classify sends the system and user prompts to Ollama and returns the parsed result.
// Graceful degradation:
//   - unreachable → ErrOllamaUnreachable (caller should skip+warn)
//   - timeout → wrapped error (caller should log+continue)
//   - invalid JSON → Result{Category:"other", Confidence:0.0}
//   - invalid category → remapped to "other"
//
// Circuit-breaker (ISI-782): once breakerThreshold consecutive ErrOllamaUnreachable
// returns have been observed, subsequent Classify calls short-circuit and return
// ErrOllamaUnreachable without dialing. Use ResetCircuitBreaker at the start of
// each ClassifyAll cycle so a recovered Ollama is re-tried promptly.
func (c *OllamaClient) Classify(ctx context.Context, systemPrompt, userPrompt string) (*ClassificationResult, error) {
	if c.shouldShortCircuit() {
		return nil, ErrOllamaUnreachable
	}

	reqBody := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Stream: false,
		Format: "json",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	url := c.endpoint + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if isConnectionError(err) {
			log.Printf("[classification] WARNING: Ollama unreachable at %s: %v", c.endpoint, err)
			c.recordUnreachable()
			return nil, ErrOllamaUnreachable
		}
		// Non-connection error (e.g. http client timeout). Don't update
		// reachability — it's a different degradation path; preserve the
		// last known reachable state.
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	// Server is reachable: we got an HTTP response, even if the status code
	// is non-2xx. Per ISI-782, the gauge should be 1 in this case so the
	// alert distinguishes "request shape may be wrong" from "host is down."
	c.recordReachable()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(body))
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		log.Printf("[classification] WARNING: invalid Ollama response body: %v", err)
		return &ClassificationResult{Category: "other", Confidence: 0.0, Reasoning: "invalid response from LLM"}, nil
	}

	return c.parseResult(chatResp.Message.Content), nil
}

// parseResult extracts {category, confidence, reasoning} from the LLM content string.
// Returns a fallback result on any parse failure.
func (c *OllamaClient) parseResult(content string) *ClassificationResult {
	var result ClassificationResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		log.Printf("[classification] WARNING: could not parse LLM JSON: %v (raw: %s)", err, content)
		return &ClassificationResult{Category: "other", Confidence: 0.0, Reasoning: "invalid JSON from LLM"}
	}

	// Normalize and validate category.
	result.Category = strings.TrimSpace(strings.ToLower(result.Category))
	if !c.categories[result.Category] {
		log.Printf("[classification] WARNING: LLM returned unknown category %q, remapping to 'other'", result.Category)
		result.Category = "other"
	}

	// Clamp confidence to [0, 1].
	if result.Confidence < 0 {
		result.Confidence = 0
	}
	if result.Confidence > 1 {
		result.Confidence = 1
	}

	return &result
}

// Model returns the configured model name.
func (c *OllamaClient) Model() string {
	return c.model
}

// Endpoint returns the configured Ollama endpoint URL (trimmed). Used as the
// `endpoint` attribute on the radar.classification.ollama_reachable gauge.
func (c *OllamaClient) Endpoint() string {
	return c.endpoint
}

// BreakerThreshold returns the consecutive-ErrOllamaUnreachable count at which
// Classify starts short-circuiting. Exposed so callers can include it in the
// abort log line.
func (c *OllamaClient) BreakerThreshold() int {
	return c.breakerThreshold
}

// ResetCircuitBreaker clears the consecutive-unreachable counter and closes the
// breaker. Callers MUST invoke this at the start of each ClassifyAll cycle so a
// recovered Ollama is re-tried promptly. Reachability state is preserved.
func (c *OllamaClient) ResetCircuitBreaker() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveUnreachable = 0
	c.breakerOpen = false
}

// CircuitBreakerOpen reports whether the breaker is currently tripped (i.e.
// subsequent Classify calls will short-circuit until ResetCircuitBreaker).
func (c *OllamaClient) CircuitBreakerOpen() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.breakerOpen
}

// LastReachable returns (reachable, observed) where observed=false means no
// Classify call has yet completed and the gauge should not be emitted with a
// fabricated value.
func (c *OllamaClient) LastReachable() (reachable, observed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastReachable, c.haveReachable
}

// shouldShortCircuit returns true if the breaker is tripped. Caller decides
// whether to skip dialing.
func (c *OllamaClient) shouldShortCircuit() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.breakerOpen
}

// recordReachable marks the last Classify call as reaching the server. Resets
// the consecutive-unreachable counter so the breaker can recover within the
// same cycle if an outage clears mid-batch.
func (c *OllamaClient) recordReachable() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveUnreachable = 0
	c.lastReachable = true
	c.haveReachable = true
}

// recordUnreachable marks the last Classify call as ErrOllamaUnreachable and
// trips the breaker once the threshold is hit.
func (c *OllamaClient) recordUnreachable() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveUnreachable++
	c.lastReachable = false
	c.haveReachable = true
	if c.consecutiveUnreachable >= c.breakerThreshold {
		c.breakerOpen = true
	}
}

// isConnectionError checks if the error indicates a connection failure (unreachable host).
func isConnectionError(err error) bool {
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	// Context deadline exceeded during dial also counts.
	if errors.Is(err, context.DeadlineExceeded) {
		return false // timeout is a different degradation path
	}
	// Check for "connection refused" style errors.
	if strings.Contains(err.Error(), "connection refused") {
		return true
	}
	return false
}
