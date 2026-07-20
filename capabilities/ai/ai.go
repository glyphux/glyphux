// Package ai is the PRD §14 slice 3.6 first-party capability: "AI for the
// products users build" (§14.1 Surface 1) — provider-agnostic
// generate/embed/classify behind a swappable Adapter (§14.2), scoped and
// rate-limited at this capability's own domain-API boundary (service.go),
// never handing a caller plugin a raw provider API key or raw network
// reach (every outbound call is gated by host.AllowsNetworkHost first,
// exactly like capabilities/commerce and capabilities/membership's own
// gateway calls).
//
// This package follows capabilities/commerce's precedent closely: a
// Plugin wraps the one configured Adapter and implements sdk.Plugin
// (Manifest/Register); the actual domain API (Service.Generate/Embed/
// Classify) is exposed as methods a CALLING plugin invokes directly,
// passing ITS OWN sdk.HostAPI (built from ITS OWN manifest declaring
// api: [ai: [generate]] etc.) — mirroring commerce.StartCheckout(ctx, host,
// gateway, req) and membership.ProcessRenewal(ctx, host, gateway, id)'s
// established shape of "free functions/methods over an explicit host
// parameter," not a new pkg/sdk.HostAPI method. See service.go's own doc
// comment for the full boundary this enforces.
//
// Runtime tier and other judgment calls (including the OpenAI-vs-OpenAI-
// compatible adapter design decision) are documented in this slice's
// tracking doc (docs/implementation/active/0034-phase3-slice3.6-ai-capability.md).
package ai

import (
	"github.com/glyphux/glyphux/pkg/sdk"
)

// Plugin is the ai capability's sdk.Plugin implementation.
type Plugin struct {
	Service *Service
}

// New returns an ai Plugin whose Generate/Embed/Classify calls (via
// Plugin.Service) go through adapter, using DefaultLimits. Adjust
// Plugin.Service.Limits after construction to override.
func New(adapter Adapter) *Plugin {
	return &Plugin{Service: NewService(adapter)}
}

// Manifest declares this capability's identity and its consent-relevant
// axes: content[read] and events[emit] (PRD §7.2: "ai -> content, events"
// — content backs SummarizeContentItem, events backs the best-effort
// ai.generated/ai.embedded/ai.classified observability emission), and
// network (the configured adapter's allowlisted host — see
// NetworkPermission's precedent in capabilities/commerce.Plugin.Manifest).
//
// This capability does NOT declare its own "ai" api scope — that scope is
// declared by CALLING plugins (api: [ai: [generate]] etc.) in THEIR OWN
// manifests, checked against THEIR OWN host by Service's methods
// (HostAPI.HasAPIScope). The ai Plugin itself is the capability that HOSTS
// the adapter, not a caller of its own domain API.
func (p *Plugin) Manifest() sdk.Manifest {
	perms := []sdk.Permission(nil)
	if p.Service != nil && p.Service.Adapter != nil {
		if host := p.Service.Adapter.AllowlistHost(); host != "" {
			perms = append(perms, sdk.Permission{Name: "network", Args: []string{host}})
		}
	}
	return sdk.Manifest{
		Name:    "ai",
		Version: "1.0.0",
		Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
		API: []sdk.APIScope{
			{Capability: "content", Scopes: []string{"read"}},
			{Capability: "events", Scopes: []string{"emit"}},
		},
		Permissions: perms,
	}
}

// Register is a no-op: this capability declares no content type of its own
// (SummarizeContentItem operates on whatever content type/id a caller
// already has, not a type ai itself owns), registers no admin page, and
// subscribes to no event — its entire surface is the Service methods a
// calling plugin invokes directly against its own host. A denial of this
// Plugin's own manifest (e.g. a host built without content:read or
// events:emit) has no effect here since Register makes no gated call; the
// gates that matter are the ones Service's methods check against the
// CALLING plugin's host, not this one.
func (p *Plugin) Register(host sdk.HostAPI) error {
	return nil
}
