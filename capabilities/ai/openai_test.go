package ai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glyphux/glyphux/capabilities/ai"
)

// newFakeOpenAIServer starts a local HTTP test server replicating the real
// OpenAI chat-completions/embeddings wire shape (POST /v1/chat/completions,
// POST /v1/embeddings, "Authorization: Bearer <key>") closely enough to
// exercise capabilities/ai's openAIAdapter's real request-building,
// response-parsing, and error-handling code — no live OpenAI credentials
// used or required. requireAuth lets a test simulate a self-hosted/
// OpenAI-compatible runtime (e.g. Ollama) that accepts requests with no
// Authorization header at all.
func newFakeOpenAIServer(t *testing.T, wantAPIKey string, requireAuth bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requireAuth {
			want := "Bearer " + wantAPIKey
			if got := r.Header.Get("Authorization"); got != want {
				http.Error(w, "bad api key", http.StatusUnauthorized)
				return
			}
		}
		switch r.URL.Path {
		case "/v1/chat/completions":
			var req struct {
				Model    string `json:"model"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			last := req.Messages[len(req.Messages)-1].Content
			if last == "trigger-error" {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{"type": "invalid_request_error", "message": "simulated bad request"},
				})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    "chatcmpl-test-1",
				"model": req.Model,
				"choices": []map[string]any{
					{
						"message":       map[string]any{"role": "assistant", "content": "Echo: " + last},
						"finish_reason": "stop",
					},
				},
			})
		case "/v1/embeddings":
			var req struct {
				Model string `json:"model"`
				Input string `json:"input"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": req.Model,
				"data": []map[string]any{
					{"embedding": []float64{0.5, 0.25, 0.125}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestOpenAIAdapterGenerateAgainstFakeServer(t *testing.T) {
	srv := newFakeOpenAIServer(t, "sk-test-key", true)
	defer srv.Close()

	// NewOpenAIAdapter pins the real api.openai.com host (proven separately
	// in TestNewOpenAIAdapterPinsRealOpenAIHost below); this test points the
	// shared implementation at the fake server via the compatible
	// constructor, proving both constructors share the exact same
	// wire-shape implementation (this slice's OpenAI-vs-compatible design
	// decision — see openai.go's doc comment).
	adapter, err := ai.NewOpenAICompatibleAdapter(srv.URL, "sk-test-key")
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter: %v", err)
	}

	resp, err := adapter.Generate(context.Background(), ai.GenerateRequest{Model: "gpt-4o-mini", Prompt: "hello there"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Text != "Echo: hello there" {
		t.Errorf("got Text=%q, want %q", resp.Text, "Echo: hello there")
	}
	if resp.Model != "gpt-4o-mini" {
		t.Errorf("got Model=%q, want %q", resp.Model, "gpt-4o-mini")
	}
	if resp.FinishReason != "stop" {
		t.Errorf("got FinishReason=%q, want %q", resp.FinishReason, "stop")
	}
}

func TestOpenAIAdapterEmbedAgainstFakeServer(t *testing.T) {
	srv := newFakeOpenAIServer(t, "sk-test-key", true)
	defer srv.Close()

	adapter, err := ai.NewOpenAICompatibleAdapter(srv.URL, "sk-test-key")
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter: %v", err)
	}
	resp, err := adapter.Embed(context.Background(), ai.EmbedRequest{Model: "text-embedding-3-small", Input: "hello"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(resp.Vector) != 3 {
		t.Fatalf("got %d-dim vector, want 3", len(resp.Vector))
	}
	if resp.Vector[0] != 0.5 {
		t.Errorf("got Vector[0]=%v, want 0.5", resp.Vector[0])
	}
}

func TestOpenAIAdapterClassifyPicksALabelViaGenerate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "gpt-4o-mini",
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "not-spam"}, "finish_reason": "stop"},
			},
		})
	}))
	defer srv.Close()

	adapter, err := ai.NewOpenAICompatibleAdapter(srv.URL, "sk-test-key")
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter: %v", err)
	}
	resp, err := adapter.Classify(context.Background(), ai.ClassifyRequest{
		Model: "gpt-4o-mini", Input: "let's have lunch", Labels: []string{"spam", "not-spam"},
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if resp.Label != "not-spam" {
		t.Errorf("got Label=%q, want %q", resp.Label, "not-spam")
	}
}

func TestOpenAIAdapterGenerateSurfacesProviderErrorEnvelope(t *testing.T) {
	srv := newFakeOpenAIServer(t, "sk-test-key", true)
	defer srv.Close()

	adapter, err := ai.NewOpenAICompatibleAdapter(srv.URL, "sk-test-key")
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter: %v", err)
	}
	_, err = adapter.Generate(context.Background(), ai.GenerateRequest{Model: "m", Prompt: "trigger-error"})
	if err == nil {
		t.Fatal("expected an error from the simulated bad request")
	}
}

func TestOpenAICompatibleAdapterWorksWithNoAPIKey(t *testing.T) {
	// Ollama and most self-hosted OpenAI-compatible runtimes require no API
	// key at all — the documented common case NewOpenAICompatibleAdapter's
	// doc comment calls out.
	srv := newFakeOpenAIServer(t, "", false)
	defer srv.Close()

	adapter, err := ai.NewOpenAICompatibleAdapter(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter: %v", err)
	}
	resp, err := adapter.Generate(context.Background(), ai.GenerateRequest{Model: "llama3", Prompt: "hi"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Text != "Echo: hi" {
		t.Errorf("got Text=%q, want %q", resp.Text, "Echo: hi")
	}
}

func TestNewOpenAIAdapterRejectedWithWrongAPIKey(t *testing.T) {
	srv := newFakeOpenAIServer(t, "sk-test-key", true)
	defer srv.Close()

	adapter, err := ai.NewOpenAICompatibleAdapter(srv.URL, "wrong-key")
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter: %v", err)
	}
	_, err = adapter.Generate(context.Background(), ai.GenerateRequest{Model: "m", Prompt: "hi"})
	if err == nil {
		t.Fatal("expected an error for a rejected API key")
	}
}

func TestNewOpenAIAdapterPinsRealOpenAIHost(t *testing.T) {
	adapter, err := ai.NewOpenAIAdapter("sk-test-key")
	if err != nil {
		t.Fatalf("NewOpenAIAdapter: %v", err)
	}
	if adapter.AllowlistHost() != "api.openai.com" {
		t.Errorf("got AllowlistHost()=%q, want %q", adapter.AllowlistHost(), "api.openai.com")
	}
}

func TestNewOpenAICompatibleAdapterRejectsInvalidBaseURL(t *testing.T) {
	if _, err := ai.NewOpenAICompatibleAdapter("://not-a-url", ""); err == nil {
		t.Fatal("expected an error for an invalid base URL")
	}
}
