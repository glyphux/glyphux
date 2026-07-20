package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// claudeAPIVersion is the Anthropic Messages API version header every real
// request must carry (https://docs.anthropic.com/en/api/messages) — a
// fixed, documented API version, not a provider-agnostic value, which is
// exactly why it lives in this file and nowhere above the Adapter
// boundary.
const claudeAPIVersion = "2023-06-01"

// ClaudeAdapter is an Adapter backed by the real Anthropic Messages API
// wire shape (POST {baseURL}/v1/messages, headers x-api-key +
// anthropic-version, body {model, max_tokens, messages: [...]}) — proven in
// claude_test.go against a local httptest fake server replicating that
// shape closely enough to exercise this adapter's real request-building,
// response-parsing, and error-handling code, never live Anthropic
// credentials.
type ClaudeAdapter struct {
	apiKey  string
	baseURL string
	host    string
	client  *http.Client
}

// NewClaudeAdapter returns a ClaudeAdapter whose Generate calls hit
// baseURL (e.g. "https://api.anthropic.com" in production; a local fake
// server's URL in tests) using apiKey.
func NewClaudeAdapter(apiKey, baseURL string) (*ClaudeAdapter, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("ai: invalid claude base URL %q: %w", baseURL, err)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("ai: claude base URL %q has no host", baseURL)
	}
	return &ClaudeAdapter{apiKey: apiKey, baseURL: strings.TrimRight(baseURL, "/"), host: u.Hostname(), client: http.DefaultClient}, nil
}

// AllowlistHost returns the hostname this adapter's calls target.
func (a *ClaudeAdapter) AllowlistHost() string { return a.host }

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    string          `json:"system,omitempty"`
	Messages  []claudeMessage `json:"messages"`
}

type claudeContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type claudeResponse struct {
	ID         string               `json:"id"`
	Model      string               `json:"model"`
	StopReason string               `json:"stop_reason"`
	Content    []claudeContentBlock `json:"content"`
}

type claudeErrorEnvelope struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Generate performs a real Anthropic Messages API request.
func (a *ClaudeAdapter) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	body := claudeRequest{
		Model:     req.Model,
		MaxTokens: maxTokens,
		System:    req.System,
		Messages:  []claudeMessage{{Role: "user", Content: req.Prompt}},
	}
	var resp claudeResponse
	if err := a.doJSON(ctx, "/v1/messages", body, &resp); err != nil {
		return nil, err
	}
	var text strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	return &GenerateResponse{Text: text.String(), Model: resp.Model, FinishReason: resp.StopReason}, nil
}

// Embed always returns ErrNotSupported: the real Anthropic API has no
// embeddings endpoint (Anthropic does not offer a first-party embedding
// model as of this writing) — a caller configured against Claude for
// embeddings gets a clear, typed "not this provider" rather than a silently
// fabricated vector.
func (a *ClaudeAdapter) Embed(ctx context.Context, req EmbedRequest) (*EmbedResponse, error) {
	return nil, fmt.Errorf("ai: claude adapter: %w", ErrNotSupported)
}

// Classify has no dedicated Anthropic endpoint (see Adapter's doc comment),
// so this builds a constrained-output prompt asking the model to respond
// with exactly one of req.Labels, then calls Generate and parses the label
// back out.
func (a *ClaudeAdapter) Classify(ctx context.Context, req ClassifyRequest) (*ClassifyResponse, error) {
	genResp, err := a.Generate(ctx, classificationGenerateRequest(req))
	if err != nil {
		return nil, err
	}
	return &ClassifyResponse{Label: matchLabel(genResp.Text, req.Labels), Model: genResp.Model}, nil
}

// doJSON POSTs body as JSON to path against a.baseURL, decoding a 2xx
// response into out or returning a formatted error built from the real
// Anthropic error envelope shape on failure.
func (a *ClaudeAdapter) doJSON(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("ai: claude adapter: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("ai: claude adapter: build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", claudeAPIVersion)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ai: claude adapter: request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("ai: claude adapter: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var envelope claudeErrorEnvelope
		if err := json.Unmarshal(respBody, &envelope); err == nil && envelope.Error.Message != "" {
			return fmt.Errorf("ai: claude adapter: %s: %s", envelope.Error.Type, envelope.Error.Message)
		}
		return fmt.Errorf("ai: claude adapter: unexpected status %d: %s", resp.StatusCode, string(respBody))
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("ai: claude adapter: decode response: %w", err)
	}
	return nil
}

// classificationGenerateRequest builds the shared constrained-output
// prompt every adapter without a native classify endpoint uses (see
// Adapter's doc comment) — kept as one shared helper (not duplicated per
// adapter) since the prompt shape itself is provider-agnostic; only the
// transport that carries it differs per adapter.
func classificationGenerateRequest(req ClassifyRequest) GenerateRequest {
	return GenerateRequest{
		Model:  req.Model,
		System: "You are a strict text classifier. Respond with exactly one label from the provided list and nothing else.",
		Prompt: fmt.Sprintf("Labels: %s\n\nText: %s\n\nWhich single label best applies? Respond with only the label text.",
			strings.Join(req.Labels, ", "), req.Input),
		MaxTokens: 32,
	}
}

// matchLabel finds which of labels the generated text names, matching
// case-insensitively and tolerating surrounding whitespace/punctuation a
// model might add (e.g. a trailing period) — a documented, simple best
// effort, not full NLP. It checks for an exact (trimmed, case-insensitive)
// match first; failing that, falls back to a substring search ordered by
// LONGEST label first, so a label that is itself a substring of another
// (e.g. "spam" inside "not-spam") never shadows the more specific one. If
// no label matches at all, the trimmed raw text is returned as-is so a
// caller can still see what the model said rather than getting a silently
// empty label.
func matchLabel(text string, labels []string) string {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)

	for _, label := range labels {
		if strings.ToLower(label) == lower {
			return label
		}
	}

	byLength := append([]string(nil), labels...)
	sort.Slice(byLength, func(i, j int) bool { return len(byLength[i]) > len(byLength[j]) })
	for _, label := range byLength {
		if strings.Contains(lower, strings.ToLower(label)) {
			return label
		}
	}
	return trimmed
}
