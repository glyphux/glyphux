package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// openAIAdapter is an Adapter backed by the OpenAI chat-completions wire
// shape (POST {baseURL}/v1/chat/completions and POST {baseURL}/v1/
// embeddings, body {model, messages/input, ...}, optional
// "Authorization: Bearer <key>") — proven in openai_test.go against a local
// httptest fake server.
//
// Design decision (OpenAI vs. "OpenAI-compatible" — see this slice's
// tracking doc for the full write-up): this is ONE unexported
// implementation parameterized by baseURL and apiKey, not two separate
// adapter types. Real self-hosted/local runtimes (Ollama and most others)
// deliberately implement the exact same OpenAI chat-completions/embeddings
// wire shape so existing OpenAI clients work against them unmodified — the
// only real differences are the base URL (a local address instead of
// api.openai.com) and authentication (local runtimes commonly require no
// API key at all, or accept any placeholder value). Modeling that as two
// separate Go types would duplicate every line of request-building/
// response-parsing/error-handling for zero behavioral difference; modeling
// it as one parameterized type and exposing two constructors
// (NewOpenAIAdapter pins the real OpenAI host; NewOpenAICompatibleAdapter
// takes any base URL) captures the actual relationship precisely: OpenAI's
// own API is simply the OpenAI-compatible shape at one canonical, always-
// authenticated host. A thin wrapper type would have added an indirection
// with no independent behavior to justify it.
type openAIAdapter struct {
	baseURL string
	apiKey  string
	host    string
	client  *http.Client
}

// NewOpenAIAdapter returns an Adapter backed by the real OpenAI API
// (https://api.openai.com) using apiKey.
func NewOpenAIAdapter(apiKey string) (Adapter, error) {
	return newOpenAIAdapter("https://api.openai.com", apiKey)
}

// NewOpenAICompatibleAdapter returns an Adapter backed by any self-hosted
// or third-party runtime that speaks the OpenAI chat-completions/
// embeddings wire shape at baseURL — the documented path for Ollama and
// other local/self-hosted models (PRD §14.2's "Ollama/local" case).
// apiKey may be empty for a runtime that requires no authentication (the
// common case for a local Ollama instance); when non-empty it is sent as
// the same "Authorization: Bearer <apiKey>" header the real OpenAI API
// uses, since OpenAI-compatible runtimes that DO require a key
// conventionally accept it the same way.
func NewOpenAICompatibleAdapter(baseURL, apiKey string) (Adapter, error) {
	return newOpenAIAdapter(baseURL, apiKey)
}

func newOpenAIAdapter(baseURL, apiKey string) (*openAIAdapter, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("ai: invalid openai-compatible base URL %q: %w", baseURL, err)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("ai: openai-compatible base URL %q has no host", baseURL)
	}
	return &openAIAdapter{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, host: u.Hostname(), client: http.DefaultClient}, nil
}

// AllowlistHost returns the hostname this adapter's calls target.
func (a *openAIAdapter) AllowlistHost() string { return a.host }

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatRequest struct {
	Model     string              `json:"model"`
	Messages  []openAIChatMessage `json:"messages"`
	MaxTokens int                 `json:"max_tokens,omitempty"`
}

type openAIChatChoice struct {
	Message      openAIChatMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type openAIChatResponse struct {
	Model   string             `json:"model"`
	Choices []openAIChatChoice `json:"choices"`
}

type openAIEmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type openAIEmbedDatum struct {
	Embedding []float64 `json:"embedding"`
}

type openAIEmbedResponse struct {
	Model string             `json:"model"`
	Data  []openAIEmbedDatum `json:"data"`
}

type openAIErrorEnvelope struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Generate performs a real OpenAI-shaped chat-completions request.
func (a *openAIAdapter) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	messages := make([]openAIChatMessage, 0, 2)
	if req.System != "" {
		messages = append(messages, openAIChatMessage{Role: "system", Content: req.System})
	}
	messages = append(messages, openAIChatMessage{Role: "user", Content: req.Prompt})

	body := openAIChatRequest{Model: req.Model, Messages: messages, MaxTokens: req.MaxTokens}
	var resp openAIChatResponse
	if err := a.doJSON(ctx, "/v1/chat/completions", body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("ai: openai adapter: response had no choices")
	}
	return &GenerateResponse{
		Text:         resp.Choices[0].Message.Content,
		Model:        resp.Model,
		FinishReason: resp.Choices[0].FinishReason,
	}, nil
}

// Embed performs a real OpenAI-shaped embeddings request.
func (a *openAIAdapter) Embed(ctx context.Context, req EmbedRequest) (*EmbedResponse, error) {
	body := openAIEmbedRequest{Model: req.Model, Input: req.Input}
	var resp openAIEmbedResponse
	if err := a.doJSON(ctx, "/v1/embeddings", body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("ai: openai adapter: response had no embedding data")
	}
	return &EmbedResponse{Vector: resp.Data[0].Embedding, Model: resp.Model}, nil
}

// Classify has no dedicated OpenAI endpoint (see Adapter's doc comment), so
// this builds the same constrained-output prompt claude.go's Classify uses
// and calls Generate.
func (a *openAIAdapter) Classify(ctx context.Context, req ClassifyRequest) (*ClassifyResponse, error) {
	genResp, err := a.Generate(ctx, classificationGenerateRequest(req))
	if err != nil {
		return nil, err
	}
	return &ClassifyResponse{Label: matchLabel(genResp.Text, req.Labels), Model: genResp.Model}, nil
}

// doJSON POSTs body as JSON to path against a.baseURL, decoding a 2xx
// response into out or returning a formatted error built from the real
// OpenAI error envelope shape on failure.
func (a *openAIAdapter) doJSON(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("ai: openai adapter: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("ai: openai adapter: build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	if a.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ai: openai adapter: request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("ai: openai adapter: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var envelope openAIErrorEnvelope
		if err := json.Unmarshal(respBody, &envelope); err == nil && envelope.Error.Message != "" {
			return fmt.Errorf("ai: openai adapter: %s: %s", envelope.Error.Type, envelope.Error.Message)
		}
		return fmt.Errorf("ai: openai adapter: unexpected status %d: %s", resp.StatusCode, string(respBody))
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("ai: openai adapter: decode response: %w", err)
	}
	return nil
}
