package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/glyphux/glyphux/pkg/sdk"
)

// ErrProviderHostNotAllowed is returned by every Service method when the
// calling plugin's manifest does not allowlist the configured adapter's
// host (slice 2.6's host.AllowsNetworkHost) — mirrors
// capabilities/commerce.ErrGatewayHostNotAllowed exactly: outbound access
// is refused before any request is attempted, never after.
var ErrProviderHostNotAllowed = errors.New("ai: provider host not allowed by this plugin's network permission")

// requireAPIScope returns an error shaped like pkg/sdk's own scope-denial
// errors (see e.g. scopedContent.requireScope) if host's manifest did not
// declare capability:scope — the same "not present in the HostAPI surface a
// plugin receives" discipline (PRD §8.3), extended here to a capability
// (ai) with no dedicated HostAPI method of its own via HostAPI.HasAPIScope
// (added by this slice specifically so a capability package outside
// pkg/sdk can enforce its own domain-API scope).
func requireAPIScope(host sdk.HostAPI, capability, scope string) error {
	if !host.HasAPIScope(capability, scope) {
		return fmt.Errorf("%s:%s: %w", capability, scope, sdk.ErrScopeNotDeclared)
	}
	return nil
}

// Service is the scoped, rate-limited AI surface a calling plugin gets
// (PRD §14.1: "a plugin wanting AI declares api: [ai: [generate]] ... and
// receives a scoped, rate-limited surface"). It is deliberately a value the
// caller holds and calls methods on, not a HostAPI method — a design
// related to, but structurally distinct from, capabilities/commerce and
// capabilities/membership's pure-free-function pattern (e.g.
// commerce.StartCheckout(ctx, host, gateway, req),
// membership.ProcessRenewal(ctx, host, gateway, subscriptionID), neither of
// which has a persistent object holding gateway/config across calls).
// Service is a stateful holder for Adapter+Limits configuration that
// callers invoke methods on, rather than a plain function — rate-limit
// configuration benefits from living in one place a caller can adjust
// once (Plugin.Service.Limits) rather than threading it through every
// call site. Every Service method still takes the CALLING plugin's own
// sdk.HostAPI as an explicit per-call parameter, never growing pkg/sdk's
// own HostAPI interface with a provider-specific method surface — which is
// what preserves the same "no bypass of the caller's own manifest scope"
// property those free functions have. Every method:
//  1. Checks the caller's host declared the corresponding "ai" api scope
//     (HasAPIScope) — never a bypass of the manifest's own consent.
//  2. Checks host.AllowsNetworkHost(Adapter.AllowlistHost()) — never a raw
//     network reach to the provider that bypassed the plugin's declared
//     network permission.
//  3. Enforces this Service's own Limits at this boundary (ratelimit.go) —
//     never delegated to the adapter.
//  4. Calls through Adapter — never a provider SDK or provider-specific
//     shape reaches this file.
//  5. Best-effort emits an observability event via host.Emit, ONLY if the
//     caller's own host also declared events:emit — emission is additive
//     when available, never a hard requirement to use Generate/Embed/
//     Classify (a caller that only wants "ai" access is not forced to also
//     take on "events" just to get an answer back).
type Service struct {
	Adapter Adapter
	Limits  Limits
}

// NewService returns a Service backed by adapter, using DefaultLimits.
func NewService(adapter Adapter) *Service {
	return &Service{Adapter: adapter, Limits: DefaultLimits()}
}

// emitIfDeclared best-effort emits event on host if host's manifest
// declared events:emit — see Service's doc comment, point 5. A failed
// emission never masks the primary operation's own result (the caller
// already has that from the adapter call); this is additive observability
// only.
func emitIfDeclared(ctx context.Context, host sdk.HostAPI, event string, payload any) {
	if !host.HasAPIScope("events", "emit") {
		return
	}
	_ = host.Emit(ctx, event, payload)
}

// GeneratedEvent is emitted as "ai.generated" after a successful Generate
// call (best-effort; see emitIfDeclared).
type GeneratedEvent struct {
	Model        string
	FinishReason string
}

// EmbeddedEvent is emitted as "ai.embedded" after a successful Embed call.
type EmbeddedEvent struct {
	Model string
	Dims  int
}

// ClassifiedEvent is emitted as "ai.classified" after a successful Classify
// call.
type ClassifiedEvent struct {
	Model string
	Label string
}

// callGated is the one shared seam Generate/Embed/Classify each funnel
// through — the four-step gate sequence (scope check, network-host check,
// rate limit, adapter call) plus best-effort event emission would
// otherwise be repeated three times over, differing only by operation
// name/limit/event name/error-wrap string. Mirrors
// capabilities/notifications.dispatch's existing use of a generic helper
// in this codebase for the same reason: one seam, however many operations
// grow to share its shape. call is a method value off Adapter (e.g.
// s.Adapter.Generate); emit is called with the successful response only
// (never on error), and is expected to itself call emitIfDeclared.
func callGated[Req, Resp any](
	ctx context.Context,
	host sdk.HostAPI,
	adapterHost string,
	operation string,
	limit RateLimit,
	req Req,
	call func(context.Context, Req) (*Resp, error),
	emit func(*Resp),
) (*Resp, error) {
	if err := requireAPIScope(host, "ai", operation); err != nil {
		return nil, err
	}
	if !host.AllowsNetworkHost(adapterHost) {
		return nil, fmt.Errorf("%w: %q", ErrProviderHostNotAllowed, adapterHost)
	}
	if err := checkRateLimit(ctx, host, operation, limit); err != nil {
		return nil, err
	}
	resp, err := call(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("ai: %s: %w", operation, err)
	}
	emit(resp)
	return resp, nil
}

// Generate performs a text-completion request, gated on ai:generate,
// network allowlisting, and this Service's Limits.Generate budget — see
// Service's own doc comment for the full boundary.
func (s *Service) Generate(ctx context.Context, host sdk.HostAPI, req GenerateRequest) (*GenerateResponse, error) {
	return callGated(ctx, host, s.Adapter.AllowlistHost(), "generate", s.Limits.Generate, req, s.Adapter.Generate, func(resp *GenerateResponse) {
		emitIfDeclared(ctx, host, "ai.generated", GeneratedEvent{Model: resp.Model, FinishReason: resp.FinishReason})
	})
}

// Embed computes an embedding vector, gated on ai:embed, network
// allowlisting, and this Service's Limits.Embed budget.
func (s *Service) Embed(ctx context.Context, host sdk.HostAPI, req EmbedRequest) (*EmbedResponse, error) {
	return callGated(ctx, host, s.Adapter.AllowlistHost(), "embed", s.Limits.Embed, req, s.Adapter.Embed, func(resp *EmbedResponse) {
		emitIfDeclared(ctx, host, "ai.embedded", EmbeddedEvent{Model: resp.Model, Dims: len(resp.Vector)})
	})
}

// Classify picks one of req.Labels, gated on ai:classify, network
// allowlisting, and this Service's Limits.Classify budget.
func (s *Service) Classify(ctx context.Context, host sdk.HostAPI, req ClassifyRequest) (*ClassifyResponse, error) {
	return callGated(ctx, host, s.Adapter.AllowlistHost(), "classify", s.Limits.Classify, req, s.Adapter.Classify, func(resp *ClassifyResponse) {
		emitIfDeclared(ctx, host, "ai.classified", ClassifiedEvent{Model: resp.Model, Label: resp.Label})
	})
}

// SummarizeContentItem is the one concrete, tested use of this capability's
// "content" dependency (PRD §7.2: "ai -> content, events"): it fetches
// typeName/id through the CALLING plugin's own host.Content() (so it is
// gated on that plugin's own declared content:read scope, exactly like any
// other content read), appends the item's data as JSON to req.Prompt, and
// otherwise behaves exactly like Generate (same ai:generate/network/
// rate-limit gates, since it calls Generate directly). This is a
// convenience the domain-API boundary offers, not a new gate of its own —
// it demonstrates "content is a real, usable dependency of this
// capability," not just a graph-declared placeholder.
func (s *Service) SummarizeContentItem(ctx context.Context, host sdk.HostAPI, typeName, id string, req GenerateRequest) (*GenerateResponse, error) {
	if err := requireAPIScope(host, "content", "read"); err != nil {
		return nil, err
	}
	item, err := host.Content().Get(ctx, typeName, id)
	if err != nil {
		return nil, fmt.Errorf("ai: summarize content item: %w", err)
	}
	data, err := json.Marshal(item.Data)
	if err != nil {
		return nil, fmt.Errorf("ai: summarize content item: encode content data: %w", err)
	}
	req.Prompt = req.Prompt + "\n\n" + string(data)
	return s.Generate(ctx, host, req)
}
