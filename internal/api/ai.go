package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	capai "github.com/glyphux/glyphux/capabilities/ai"
	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/compat"
	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/sdk"
	"github.com/glyphux/glyphux/pkg/theme"
	"github.com/glyphux/glyphux/themes/starter"
)

// aiComposeRequest is POST /api/v0/ai/compose's body: what the user typed,
// and which route/theme the proposed fragment is destined for (the same
// destination context a real preset import (handlePresetImport) already
// takes, so this endpoint's compat check has the same theme_regions
// restriction available to it). Model lets a caller pick which model the
// configured Adapter should use (capabilities/ai.GenerateRequest.Model is
// passed through opaquely to whatever provider Adapter this daemon was
// wired with — see WithAI's doc comment on why no default is guessed here).
type aiComposeRequest struct {
	Prompt       string   `json:"prompt"`
	Route        string   `json:"route"`
	ThemeRegions []string `json:"theme_regions,omitempty"`
	Model        string   `json:"model"`
}

// aiPreview is the rendered-HTML half of a successful compose response —
// the identical shape handleLayoutPreview already returns, reused verbatim
// rather than inventing a second "preview result" JSON shape.
type aiPreview struct {
	HTML        string `json:"html"`
	ContentType string `json:"content_type"`
}

// aiComposeResponse is POST /api/v0/ai/compose's success-shaped body.
// compat.Result is embedded (not wrapped) so its fields (compatible,
// missing_blocks, missing_slots, unsupported_contract) appear at the top
// level — the identical JSON shape handlePresetCheck/handlePresetImport
// already produce, so an admin-ui client that already knows how to render
// "this preset needs the pricing-table block" for an ordinary import
// renders the identical diagnostic here with no new shape to learn.
// Fragment and Preview are only populated when Compatible is true — a
// caller distinguishes "declined, here's why" from "here's what to
// preview" by checking .compatible, exactly like handlePresetImport's own
// established convention.
type aiComposeResponse struct {
	compat.Result
	Fragment *contract.CompositionPreset `json:"fragment,omitempty"`
	Preview  *aiPreview                  `json:"preview,omitempty"`
}

// aiCallerManifest builds the sdk.Manifest the admin server itself declares
// in order to call capabilities/ai.Service.Generate on this request's
// behalf — PRD §14.1's "a plugin wanting AI declares api: [ai: [generate]]
// ... and receives a scoped, rate-limited surface," applied to the admin
// server acting as its OWN caller for this one first-party feature.
//
// There was no prior internal/api precedent for this (internal/api calls
// every other domain API — preset.Store, layout.Store, content.API —
// directly as a plain Go value, never through a constructed sdk.HostAPI;
// this ticket's tracking doc documents that investigation). The reason this
// endpoint needs one at all, where those others don't, is that
// capabilities/ai.Service's entire domain-API boundary (Service.Generate)
// is deliberately gated on a CALLER-supplied sdk.HostAPI's declared manifest
// scopes (service.go's callGated) — the same "no bypass of the caller's own
// manifest scope" property every third-party plugin gets, which this
// endpoint must go through rather than around, exactly like any other
// caller of this capability.
//
// adapterHost is the configured Adapter's AllowlistHost() — declared here as
// this manifest's own "network" permission so
// HostAPI.AllowsNetworkHost(adapterHost) succeeds inside Service.Generate
// (mirroring every other first-party capability's own AllowlistHost gate,
// e.g. capabilities/commerce's payment gateway). Building a fresh HostAPI
// per request (rather than caching one on Server) costs one map
// construction and keeps this function pure/testable in isolation; nothing
// about it depends on request state, so a future caller could hoist and
// cache it if that ever mattered.
func aiCallerManifest(adapterHost string) sdk.Manifest {
	m := sdk.Manifest{
		Name:    "glyphuxd-admin-ai-compose",
		Version: "1.0.0",
		Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: string(contract.ContentCompositionV0),
		},
		API: []sdk.APIScope{
			{Capability: "ai", Scopes: []string{"generate"}},
		},
	}
	if adapterHost != "" {
		m.Permissions = []sdk.Permission{{Name: "network", Args: []string{adapterHost}}}
	}
	return m
}

// aiComposeSystemPrompt engineers the instruction that constrains the
// model's output to a single JSON contract.CompositionPreset document (PRD
// §14.1: "AI in the builder emits composition, not markup") — never raw
// HTML, never prose alongside the JSON. availableBlocks (this daemon's real,
// live *blocks.Registry contents) is named explicitly and the model is told
// not to invent block types outside that list; whether it actually complies
// is exactly what this endpoint's reuse of the preset-import validation
// path (compat.CheckPreset) below catches and reports, not something this
// prompt is trusted to guarantee on its own.
func aiComposeSystemPrompt(availableBlocks []string) string {
	sort.Strings(availableBlocks)
	return fmt.Sprintf(`You are a website composition assistant embedded in a headless CMS's visual builder.
Respond with ONLY a single JSON object — no prose, no markdown code fences — matching exactly this shape:
{
  "contract_version": "composition-preset/v1",
  "name": "short-kebab-case-name",
  "description": "one sentence describing the fragment",
  "layout": {
    "contract_version": "layout-composition/v1",
    "regions": {
      "<region-name>": {
        "blocks": [ { "type": "<block-type>", "props": { }, "slots": { } } ]
      }
    }
  },
  "manifest": {
    "requires_contract": "layout-composition/v1",
    "blocks": ["<every block type used above>"],
    "slots": ["<every region name used above>"]
  }
}
Only use these registered block types, exactly as spelled (do not invent new ones): %s
`, strings.Join(availableBlocks, ", "))
}

// extractJSONObject strips a leading/trailing markdown code fence (some
// providers wrap JSON in ```json ... ``` even when explicitly told not to)
// and returns the remaining trimmed text — best-effort tolerance for the
// one common formatting deviation observed from chat-completion-shaped
// models, not a general markdown parser. json.Unmarshal is what actually
// validates the result; this only removes the one wrapper known to make an
// otherwise-valid JSON document fail to parse verbatim.
func extractJSONObject(text string) string {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "```") {
		t = strings.TrimPrefix(t, "```json")
		t = strings.TrimPrefix(t, "```")
		t = strings.TrimSuffix(t, "```")
	}
	return strings.TrimSpace(t)
}

// handleAICompose is Ticket P4.8's endpoint (PRD §14.1 Surface 2): calls
// capabilities/ai.Service.Generate with a prompt engineered to return a
// Layer-2 composition fragment (aiComposeSystemPrompt), then runs the
// parsed result through the EXACT SAME validation a real preset save
// already uses — contract.CompositionPreset.Validate then
// pkg/compat.CheckPreset, in that order, mirroring internal/preset.
// Store.Save's own sequence verbatim (not internal/preset.Store.Import,
// which additionally requires the preset already be saved under an ID; this
// endpoint validates an UNSAVED, in-memory fragment, the same "draft"
// relationship handleLayoutPreview already has to layout.Store.Save). This
// is never a parallel AI-specific validator: "AI used a block you don't
// have" fails exactly the way an incompatible preset save/import already
// fails today.
//
// Failure shapes, both reused from existing call sites rather than invented
// here:
//   - Malformed model output (not JSON, or JSON that fails
//     CompositionPreset.Validate's structural check) maps to
//     contract.ValidationErrors via writeDomainError — byte-for-byte the
//     same 422 shape handlePresetCreate already returns when Store.Save's
//     own p.Validate() fails.
//   - Structurally valid but referencing an unregistered block/slot returns
//     200 with the real compat.Result (Compatible: false, MissingBlocks/
//     MissingSlots populated) — byte-for-byte the same shape
//     handlePresetCheck/handlePresetImport already return for an
//     incompatible preset.
//
// On success (Compatible), the fragment's Layout is merged onto whatever
// Layout is already saved for req.Route (the same merge
// internal/preset.Store.Import performs, without persisting it) and
// rendered through the real themes/starter theme via layout.ValidateDraft +
// starter.Render — the identical machinery handleLayoutPreview (slice 4.5)
// already uses, so what this returns as a preview is exactly what a real
// import into that route would render as.
//
// Requires layouts:manage AND presets:manage (this endpoint proposes a
// fragment that, once accepted, mutates both a Layout and the presets
// table via the existing save/import endpoints — the same admin-only,
// "structural site-wide change" blast radius both capabilities already
// gate), checked here at the transport boundary; capabilities/ai.Service
// separately enforces its own ai:generate scope and rate limit against the
// caller manifest this handler builds (aiCallerManifest) — neither gate
// substitutes for the other. Also requires CSRF protection (wired at the
// route registration in api.go), matching every other state-changing-shaped
// POST in this package even though this handler itself performs no
// persistence.
func (s *Server) handleAICompose(w http.ResponseWriter, r *http.Request) {
	if s.ai == nil || s.blocks == nil || s.layouts == nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	principal := s.principal(r)
	if !permission.AllowsPrincipal(principal, permission.LayoutsManage) || !permission.AllowsPrincipal(principal, permission.PresetsManage) {
		s.writeDomainError(w, permission.ErrDenied, "ai compose request")
		return
	}

	var req aiComposeRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		s.writeError(w, http.StatusBadRequest, "prompt must not be empty")
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		s.writeError(w, http.StatusBadRequest, "model must not be empty")
		return
	}

	callerHost, err := sdk.NewHostAPI(aiCallerManifest(s.ai.Adapter.AllowlistHost()), sdk.KernelDeps{})
	if err != nil {
		s.log.Error("build ai caller host", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	available := s.blocks.List()
	blockNames := make([]string, 0, len(available))
	for _, b := range available {
		blockNames = append(blockNames, b.Name)
	}

	genResp, err := s.ai.Generate(r.Context(), callerHost, capai.GenerateRequest{
		Model:  req.Model,
		System: aiComposeSystemPrompt(blockNames),
		Prompt: req.Prompt,
	})
	if err != nil {
		s.log.Error("ai compose generate", "error", err)
		s.writeError(w, http.StatusBadGateway, "ai provider request failed")
		return
	}

	var fragment contract.CompositionPreset
	if err := json.Unmarshal([]byte(extractJSONObject(genResp.Text)), &fragment); err != nil {
		s.writeDomainError(w, contract.ValidationErrors{{
			Path:    "",
			Message: "model did not return valid JSON: " + err.Error(),
		}}, "ai compose request")
		return
	}
	if err := fragment.Validate(); err != nil {
		s.writeDomainError(w, err, "ai compose request")
		return
	}

	result := compat.CheckPreset(&fragment, s.blocks, req.ThemeRegions)
	if !result.Compatible {
		s.writeJSON(w, http.StatusOK, aiComposeResponse{Result: result})
		return
	}

	target := &contract.Layout{ContractVersion: contract.LayoutCompositionV1, Regions: map[string]contract.Region{}}
	if req.Route != "" {
		existing, err := s.layouts.Load(r.Context(), req.Route)
		switch {
		case err == nil:
			target = existing
		case errors.Is(err, layout.ErrNotFound):
			// No layout saved for this route yet — start from the empty
			// target already constructed above, exactly like
			// internal/preset.Store.Import's identical branch.
		default:
			// A real backend failure (e.g. a dropped DB connection) is NOT
			// the same as "nothing saved yet" — silently falling through to
			// an empty layout here would produce a misleading preview
			// instead of surfacing the actual failure, so this mirrors
			// Store.Import's own "any other error" branch verbatim.
			s.log.Error("load target layout for ai compose preview", "route", req.Route, "error", err)
			s.writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if target.Regions == nil {
		target.Regions = map[string]contract.Region{}
	}
	for name, region := range fragment.Layout.Regions {
		target.Regions[name] = region
	}

	if err := layout.ValidateDraft(target, s.blocks); err != nil {
		s.writeLayoutError(w, err)
		return
	}
	view := theme.NewCompositionView(nil, target)
	html, contentType, err := starter.New(s.blocks).Render(r.Context(), view)
	if err != nil {
		s.log.Error("render ai compose preview", "error", err)
		s.writeError(w, http.StatusInternalServerError, "render failed")
		return
	}

	s.writeJSON(w, http.StatusOK, aiComposeResponse{
		Result:   result,
		Fragment: &fragment,
		Preview:  &aiPreview{HTML: string(html), ContentType: contentType},
	})
}
