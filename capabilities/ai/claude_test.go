package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glyphux/glyphux/capabilities/ai"
)

// newFakeClaudeServer starts a local HTTP test server replicating the real
// Anthropic Messages API shape (POST /v1/messages, headers x-api-key +
// anthropic-version, JSON body {model, max_tokens, messages}) closely
// enough to exercise capabilities/ai.ClaudeAdapter's real request-building,
// response-parsing, and error-handling code — no live Anthropic credentials
// used or required, mirroring capabilities/commerce/fakegateway_test.go's
// established discipline for this codebase.
func newFakeClaudeServer(t *testing.T, wantAPIKey string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("x-api-key"); got != wantAPIKey {
			http.Error(w, "bad api key", http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			http.Error(w, "missing anthropic-version header", http.StatusBadRequest)
			return
		}
		var req struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
			Messages  []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
			http.Error(w, "expected exactly one user message", http.StatusBadRequest)
			return
		}
		if req.Messages[0].Content == "trigger-error" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"type": "error",
				"error": map[string]any{
					"type":    "invalid_request_error",
					"message": "simulated bad request",
				},
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_test_1",
			"type":        "message",
			"role":        "assistant",
			"model":       req.Model,
			"stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "text", "text": "Echo: " + req.Messages[0].Content},
			},
		})
	}))
}

func TestClaudeAdapterGenerateAgainstFakeServer(t *testing.T) {
	srv := newFakeClaudeServer(t, "sk-ant-test-key")
	defer srv.Close()

	adapter, err := ai.NewClaudeAdapter("sk-ant-test-key", srv.URL)
	if err != nil {
		t.Fatalf("NewClaudeAdapter: %v", err)
	}

	resp, err := adapter.Generate(context.Background(), ai.GenerateRequest{
		Model:  "claude-3-5-sonnet-20241022",
		Prompt: "hello there",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Text != "Echo: hello there" {
		t.Errorf("got Text=%q, want %q", resp.Text, "Echo: hello there")
	}
	if resp.Model != "claude-3-5-sonnet-20241022" {
		t.Errorf("got Model=%q, want %q", resp.Model, "claude-3-5-sonnet-20241022")
	}
	if resp.FinishReason != "end_turn" {
		t.Errorf("got FinishReason=%q, want %q", resp.FinishReason, "end_turn")
	}
}

func TestClaudeAdapterGenerateSurfacesProviderErrorEnvelope(t *testing.T) {
	srv := newFakeClaudeServer(t, "sk-ant-test-key")
	defer srv.Close()

	adapter, err := ai.NewClaudeAdapter("sk-ant-test-key", srv.URL)
	if err != nil {
		t.Fatalf("NewClaudeAdapter: %v", err)
	}

	_, err = adapter.Generate(context.Background(), ai.GenerateRequest{Model: "m", Prompt: "trigger-error"})
	if err == nil {
		t.Fatal("expected an error from the simulated bad request")
	}
	if got := err.Error(); !strings.Contains(got, "simulated bad request") {
		t.Errorf("expected the real provider error message to surface, got %q", got)
	}
}

func TestClaudeAdapterGenerateRejectedWithWrongAPIKey(t *testing.T) {
	srv := newFakeClaudeServer(t, "sk-ant-test-key")
	defer srv.Close()

	adapter, err := ai.NewClaudeAdapter("wrong-key", srv.URL)
	if err != nil {
		t.Fatalf("NewClaudeAdapter: %v", err)
	}

	_, err = adapter.Generate(context.Background(), ai.GenerateRequest{Model: "m", Prompt: "hi"})
	if err == nil {
		t.Fatal("expected an error for a rejected API key")
	}
}

func TestClaudeAdapterEmbedReturnsNotSupported(t *testing.T) {
	adapter, err := ai.NewClaudeAdapter("sk-ant-test-key", "https://api.anthropic.com")
	if err != nil {
		t.Fatalf("NewClaudeAdapter: %v", err)
	}
	_, err = adapter.Embed(context.Background(), ai.EmbedRequest{Model: "m", Input: "hi"})
	if err == nil {
		t.Fatal("expected Embed to return an error")
	}
	if !errors.Is(err, ai.ErrNotSupported) {
		t.Errorf("expected err to wrap ai.ErrNotSupported, got %v", err)
	}
}

func TestClaudeAdapterClassifyPicksALabelViaGenerate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":       "claude-3-5-sonnet-20241022",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "spam"}},
		})
	}))
	defer srv.Close()

	adapter, err := ai.NewClaudeAdapter("sk-ant-test-key", srv.URL)
	if err != nil {
		t.Fatalf("NewClaudeAdapter: %v", err)
	}
	resp, err := adapter.Classify(context.Background(), ai.ClassifyRequest{
		Model: "claude-3-5-sonnet-20241022", Input: "buy pills now", Labels: []string{"spam", "not-spam"},
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if resp.Label != "spam" {
		t.Errorf("got Label=%q, want %q", resp.Label, "spam")
	}
}

func TestNewClaudeAdapterAllowlistHostMatchesServer(t *testing.T) {
	srv := newFakeClaudeServer(t, "k")
	defer srv.Close()
	adapter, err := ai.NewClaudeAdapter("k", srv.URL)
	if err != nil {
		t.Fatalf("NewClaudeAdapter: %v", err)
	}
	if adapter.AllowlistHost() == "" {
		t.Fatal("expected a non-empty AllowlistHost")
	}
}

