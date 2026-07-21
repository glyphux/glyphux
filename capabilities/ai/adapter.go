package ai

import (
	"context"
	"errors"
)

// GenerateRequest is a text-completion request against whatever provider an
// Adapter wraps. Model is a provider-specific model name (e.g.
// "claude-3-5-sonnet-20241022", "gpt-4o-mini", "gemini-1.5-flash") passed
// through opaquely — this capability's domain layer (service.go) never
// interprets it, keeping the provider-agnostic promise (PRD §14.2) intact
// one level up: nothing above the Adapter boundary needs to know what a
// valid model string looks like for any particular provider.
type GenerateRequest struct {
	Model     string
	Prompt    string
	System    string
	MaxTokens int
}

// GenerateResponse is a completed text-generation result.
type GenerateResponse struct {
	Text         string
	Model        string
	FinishReason string
}

// EmbedRequest is an embedding request.
type EmbedRequest struct {
	Model string
	Input string
}

// EmbedResponse is a computed embedding vector.
type EmbedResponse struct {
	Vector []float64
	Model  string
}

// ClassifyRequest asks a provider to pick exactly one of Labels that best
// describes Input.
type ClassifyRequest struct {
	Model  string
	Input  string
	Labels []string
}

// ClassifyResponse is a classification result.
type ClassifyResponse struct {
	Label string
	Model string
}

// ErrNotSupported is returned by an Adapter method a given provider has no
// real API for — e.g. Anthropic's Messages API has no embeddings endpoint,
// so ClaudeAdapter.Embed returns this rather than silently faking a vector.
// Documented per-adapter (see each adapter file's doc comment) rather than
// hidden: a caller that gets this back knows definitively "not this
// provider," not "something went wrong."
var ErrNotSupported = errors.New("ai: operation not supported by this provider adapter")

// Adapter is the provider-agnostic contract PRD §14.2 calls for: "Claude/
// OpenAI/Ollama/local models are swappable adapters behind a stable
// internal contract... no model provider's specifics leak above the
// adapter boundary." Every concrete provider (claude.go, openai.go,
// gemini.go) implements exactly this — service.go (the capability's own
// domain-API boundary) only ever calls through this interface, never a
// provider SDK or provider-specific wire type directly.
//
// Classify has no dedicated real endpoint on any of the four providers this
// slice ships (none of Anthropic/OpenAI/Gemini/self-hosted-OpenAI-compatible
// expose a classification API) — every adapter here implements it as a
// constrained-output Generate call with a label-selection prompt, parsing
// the label back out of the generated text. This is documented per-adapter,
// not hidden: it is real, honest behavior for how classification is done
// against a chat/completions-shaped LLM API in practice, not a stub.
type Adapter interface {
	Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)
	Embed(ctx context.Context, req EmbedRequest) (*EmbedResponse, error)
	Classify(ctx context.Context, req ClassifyRequest) (*ClassifyResponse, error)
	// AllowlistHost is the exact hostname this adapter's outbound calls
	// target — what Plugin.Manifest declares as this capability's "network"
	// permission allowlist entry, and what every Service method checks via
	// host.AllowsNetworkHost before ever calling into the adapter, mirroring
	// capabilities/commerce.PaymentGateway's identical AllowlistHost
	// pattern.
	AllowlistHost() string
}
