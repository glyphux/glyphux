package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// providerErrorEnvelope is the shape shared, field-for-field-optionally, by
// every provider's real error response body this slice's three adapters
// see: Anthropic and OpenAI both nest {"error": {"type", "message"}};
// Gemini nests {"error": {"status", "message"}} (plus a "code" this package
// has no use for). All three carry "message" under "error" — Type/Status
// are mutually exclusive across providers in practice (each API sets
// exactly one of them, never both), so httpJSON prefers whichever is
// non-empty to label the error without needing a provider-specific decode
// step.
type providerErrorEnvelope struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Status  string `json:"status"`
	} `json:"error"`
}

// httpJSON is the one shared HTTP-transport seam every adapter's real
// provider call goes through (claude.go/openai.go/gemini.go each reduce to
// a thin wrapper setting up url/headers and calling this): marshal body,
// POST it to url with headers set, and either decode a 2xx JSON response
// into out or return a formatted error built from the real provider's
// error envelope shape on a non-2xx response. errPrefix (e.g.
// "ai: claude adapter") labels every error this call can return, so each
// adapter's own errors stay distinguishable in a log without each adapter
// reimplementing this ~25-line request/response cycle three times over.
func httpJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, body any, out any, errPrefix string) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%s: encode request: %w", errPrefix, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%s: build request: %w", errPrefix, err)
	}
	req.Header.Set("content-type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: request: %w", errPrefix, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%s: read response: %w", errPrefix, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var envelope providerErrorEnvelope
		if err := json.Unmarshal(respBody, &envelope); err == nil && envelope.Error.Message != "" {
			label := envelope.Error.Type
			if label == "" {
				label = envelope.Error.Status
			}
			if label != "" {
				return fmt.Errorf("%s: %s: %s", errPrefix, label, envelope.Error.Message)
			}
			return fmt.Errorf("%s: %s", errPrefix, envelope.Error.Message)
		}
		return fmt.Errorf("%s: unexpected status %d: %s", errPrefix, resp.StatusCode, string(respBody))
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("%s: decode response: %w", errPrefix, err)
	}
	return nil
}
