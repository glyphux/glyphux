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

// GeminiAdapter is an Adapter backed by the real Google Gemini
// generateContent/embedContent wire shape (POST {baseURL}/v1beta/models/
// {model}:generateContent?key=..., POST {baseURL}/v1beta/models/{model}:
// embedContent?key=..., body {"contents": [{"parts": [{"text": "..."}]}]})
// — proven in gemini_test.go against a local httptest fake server. Unlike
// Claude/OpenAI, Gemini authenticates via an API-key QUERY PARAMETER rather
// than a header, and its model name is part of the URL PATH rather than
// the JSON body — both real, documented differences in this provider's
// wire shape that stay entirely inside this file, per the Adapter
// boundary's "no provider specifics leak above it" discipline (PRD §14.2).
type GeminiAdapter struct {
	apiKey  string
	baseURL string
	host    string
	client  *http.Client
}

// NewGeminiAdapter returns a GeminiAdapter whose calls hit baseURL (e.g.
// "https://generativelanguage.googleapis.com" in production; a local fake
// server's URL in tests) using apiKey.
func NewGeminiAdapter(apiKey, baseURL string) (*GeminiAdapter, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("ai: invalid gemini base URL %q: %w", baseURL, err)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("ai: gemini base URL %q has no host", baseURL)
	}
	return &GeminiAdapter{apiKey: apiKey, baseURL: strings.TrimRight(baseURL, "/"), host: u.Hostname(), client: http.DefaultClient}, nil
}

// AllowlistHost returns the hostname this adapter's calls target.
func (a *GeminiAdapter) AllowlistHost() string { return a.host }

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type geminiGenerateRequest struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type geminiGenerateResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
}

type geminiEmbedRequest struct {
	Content geminiContent `json:"content"`
}

type geminiEmbedding struct {
	Values []float64 `json:"values"`
}

type geminiEmbedResponse struct {
	Embedding geminiEmbedding `json:"embedding"`
}

type geminiErrorEnvelope struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// Generate performs a real Gemini generateContent request.
func (a *GeminiAdapter) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	body := geminiGenerateRequest{
		Contents: []geminiContent{{Role: "user", Parts: []geminiPart{{Text: req.Prompt}}}},
	}
	if req.System != "" {
		body.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: req.System}}}
	}
	if req.MaxTokens > 0 {
		body.GenerationConfig = &geminiGenerationConfig{MaxOutputTokens: req.MaxTokens}
	}
	var resp geminiGenerateResponse
	if err := a.doJSON(ctx, fmt.Sprintf("/v1beta/models/%s:generateContent", req.Model), body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("ai: gemini adapter: response had no candidates")
	}
	var text strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		text.WriteString(part.Text)
	}
	return &GenerateResponse{Text: text.String(), Model: req.Model, FinishReason: resp.Candidates[0].FinishReason}, nil
}

// Embed performs a real Gemini embedContent request.
func (a *GeminiAdapter) Embed(ctx context.Context, req EmbedRequest) (*EmbedResponse, error) {
	body := geminiEmbedRequest{Content: geminiContent{Parts: []geminiPart{{Text: req.Input}}}}
	var resp geminiEmbedResponse
	if err := a.doJSON(ctx, fmt.Sprintf("/v1beta/models/%s:embedContent", req.Model), body, &resp); err != nil {
		return nil, err
	}
	return &EmbedResponse{Vector: resp.Embedding.Values, Model: req.Model}, nil
}

// Classify has no dedicated Gemini endpoint (see Adapter's doc comment), so
// this builds the same constrained-output prompt claude.go's/openai.go's
// Classify use and calls Generate.
func (a *GeminiAdapter) Classify(ctx context.Context, req ClassifyRequest) (*ClassifyResponse, error) {
	genResp, err := a.Generate(ctx, classificationGenerateRequest(req))
	if err != nil {
		return nil, err
	}
	return &ClassifyResponse{Label: matchLabel(genResp.Text, req.Labels), Model: genResp.Model}, nil
}

// doJSON POSTs body as JSON to path against a.baseURL with the API key as a
// query parameter (Gemini's real authentication shape — see this type's
// doc comment), decoding a 2xx response into out or returning a formatted
// error built from the real Gemini error envelope shape on failure.
func (a *GeminiAdapter) doJSON(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("ai: gemini adapter: encode request: %w", err)
	}
	reqURL := a.baseURL + path + "?key=" + url.QueryEscape(a.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("ai: gemini adapter: build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ai: gemini adapter: request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("ai: gemini adapter: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var envelope geminiErrorEnvelope
		if err := json.Unmarshal(respBody, &envelope); err == nil && envelope.Error.Message != "" {
			return fmt.Errorf("ai: gemini adapter: %s: %s", envelope.Error.Status, envelope.Error.Message)
		}
		return fmt.Errorf("ai: gemini adapter: unexpected status %d: %s", resp.StatusCode, string(respBody))
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("ai: gemini adapter: decode response: %w", err)
	}
	return nil
}
