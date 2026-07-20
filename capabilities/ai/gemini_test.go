package ai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glyphux/glyphux/capabilities/ai"
)

// newFakeGeminiServer starts a local HTTP test server replicating the real
// Google Gemini generateContent/embedContent wire shape (POST /v1beta/
// models/{model}:generateContent?key=..., POST /v1beta/models/{model}:
// embedContent?key=..., body {"contents": [{"parts": [{"text": "..."}]}]})
// closely enough to exercise capabilities/ai.GeminiAdapter's real
// request-building, response-parsing, and error-handling code — no live
// Gemini credentials used or required.
func newFakeGeminiServer(t *testing.T, wantAPIKey string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("key"); got != wantAPIKey {
			http.Error(w, "bad api key", http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, ":generateContent"):
			var req struct {
				Contents []struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"contents"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			text := req.Contents[0].Parts[0].Text
			if text == "trigger-error" {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{"code": 400, "status": "INVALID_ARGUMENT", "message": "simulated bad request"},
				})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"candidates": []map[string]any{
					{
						"content":      map[string]any{"role": "model", "parts": []map[string]any{{"text": "Echo: " + text}}},
						"finishReason": "STOP",
					},
				},
			})
		case strings.HasSuffix(r.URL.Path, ":embedContent"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"embedding": map[string]any{"values": []float64{0.9, 0.8, 0.7}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestGeminiAdapterGenerateAgainstFakeServer(t *testing.T) {
	srv := newFakeGeminiServer(t, "gemini-test-key")
	defer srv.Close()

	adapter, err := ai.NewGeminiAdapter("gemini-test-key", srv.URL)
	if err != nil {
		t.Fatalf("NewGeminiAdapter: %v", err)
	}
	resp, err := adapter.Generate(context.Background(), ai.GenerateRequest{Model: "gemini-1.5-flash", Prompt: "hello there"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Text != "Echo: hello there" {
		t.Errorf("got Text=%q, want %q", resp.Text, "Echo: hello there")
	}
	if resp.FinishReason != "STOP" {
		t.Errorf("got FinishReason=%q, want %q", resp.FinishReason, "STOP")
	}
}

func TestGeminiAdapterEmbedAgainstFakeServer(t *testing.T) {
	srv := newFakeGeminiServer(t, "gemini-test-key")
	defer srv.Close()

	adapter, err := ai.NewGeminiAdapter("gemini-test-key", srv.URL)
	if err != nil {
		t.Fatalf("NewGeminiAdapter: %v", err)
	}
	resp, err := adapter.Embed(context.Background(), ai.EmbedRequest{Model: "text-embedding-004", Input: "hello"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(resp.Vector) != 3 || resp.Vector[0] != 0.9 {
		t.Errorf("got Vector=%v, want [0.9 0.8 0.7]", resp.Vector)
	}
}

func TestGeminiAdapterClassifyPicksALabelViaGenerate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]any{{"text": "positive"}}}, "finishReason": "STOP"},
			},
		})
	}))
	defer srv.Close()

	adapter, err := ai.NewGeminiAdapter("gemini-test-key", srv.URL)
	if err != nil {
		t.Fatalf("NewGeminiAdapter: %v", err)
	}
	resp, err := adapter.Classify(context.Background(), ai.ClassifyRequest{
		Model: "gemini-1.5-flash", Input: "I love this", Labels: []string{"positive", "negative"},
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if resp.Label != "positive" {
		t.Errorf("got Label=%q, want %q", resp.Label, "positive")
	}
}

func TestGeminiAdapterGenerateSurfacesProviderErrorEnvelope(t *testing.T) {
	srv := newFakeGeminiServer(t, "gemini-test-key")
	defer srv.Close()

	adapter, err := ai.NewGeminiAdapter("gemini-test-key", srv.URL)
	if err != nil {
		t.Fatalf("NewGeminiAdapter: %v", err)
	}
	_, err = adapter.Generate(context.Background(), ai.GenerateRequest{Model: "m", Prompt: "trigger-error"})
	if err == nil {
		t.Fatal("expected an error from the simulated bad request")
	}
	if got := err.Error(); !strings.Contains(got, "simulated bad request") {
		t.Errorf("expected the real provider error message to surface, got %q", got)
	}
}

func TestGeminiAdapterGenerateRejectedWithWrongAPIKey(t *testing.T) {
	srv := newFakeGeminiServer(t, "gemini-test-key")
	defer srv.Close()

	adapter, err := ai.NewGeminiAdapter("wrong-key", srv.URL)
	if err != nil {
		t.Fatalf("NewGeminiAdapter: %v", err)
	}
	_, err = adapter.Generate(context.Background(), ai.GenerateRequest{Model: "m", Prompt: "hi"})
	if err == nil {
		t.Fatal("expected an error for a rejected API key")
	}
}

func TestNewGeminiAdapterRejectsInvalidBaseURL(t *testing.T) {
	if _, err := ai.NewGeminiAdapter("k", "://not-a-url"); err == nil {
		t.Fatal("expected an error for an invalid base URL")
	}
}
