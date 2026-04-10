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
	"strings"
	"time"
)

// OllamaClient communicates with the Ollama /api/chat endpoint.
type OllamaClient struct {
	endpoint   string
	model      string
	httpClient *http.Client
	categories map[string]bool
}

// NewOllamaClient creates a client for the given Ollama endpoint, model, and timeout.
// categories is the allowed list of classification categories.
func NewOllamaClient(endpoint, model string, timeoutMs int, categories []string) *OllamaClient {
	catMap := make(map[string]bool, len(categories))
	for _, c := range categories {
		catMap[c] = true
	}
	return &OllamaClient{
		endpoint: strings.TrimSuffix(endpoint, "/"),
		model:    model,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutMs) * time.Millisecond,
		},
		categories: catMap,
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
func (c *OllamaClient) Classify(ctx context.Context, systemPrompt, userPrompt string) (*ClassificationResult, error) {
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
			return nil, ErrOllamaUnreachable
		}
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

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
