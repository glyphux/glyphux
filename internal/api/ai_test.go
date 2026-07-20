package api_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/blocks/firstparty"
	capai "github.com/glyphux/glyphux/capabilities/ai"
	"github.com/glyphux/glyphux/internal/api"
	"github.com/glyphux/glyphux/internal/bundle"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/internal/preset"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/contract"
)

// fakeAIAdapter is an in-process, hermetic stand-in for capabilities/ai.
// Adapter (implemented directly, per this ticket's testing discipline — no
// live provider, no httptest transport server, since this suite tests the
// compose-and-validate flow, not any provider's wire shape, which
// capabilities/ai's own adapter tests already cover independently). Text is
// returned verbatim as the "model's" Generate response, letting each test
// script exactly what a real model would have answered — including
// malformed or incompatible answers, which is the whole point of this
// endpoint's validation step.
type fakeAIAdapter struct {
	text string
	err  error
}

func (f *fakeAIAdapter) AllowlistHost() string { return "fake-ai.test" }

func (f *fakeAIAdapter) Generate(ctx context.Context, req capai.GenerateRequest) (*capai.GenerateResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &capai.GenerateResponse{Text: f.text, Model: req.Model, FinishReason: "stop"}, nil
}

func (f *fakeAIAdapter) Embed(ctx context.Context, req capai.EmbedRequest) (*capai.EmbedResponse, error) {
	return nil, capai.ErrNotSupported
}

func (f *fakeAIAdapter) Classify(ctx context.Context, req capai.ClassifyRequest) (*capai.ClassifyResponse, error) {
	return nil, capai.ErrNotSupported
}

// testServerWithAI boots a server with the Layer-2 block/layout transport,
// the preset/bundle transport, AND the AI compose transport (WithLayouts +
// WithPresets + WithAI) wired in, backed by adapter — the equivalent of
// testServerWithPresets for this ticket's one new route. Also returns the
// underlying *db.DB so a test needing to simulate a real backend failure
// (e.g. TestAIComposeLayoutLoadErrorReturns500) can close it mid-test,
// mirroring internal/api/readiness_test.go's identical "d.Close() to
// simulate a dropped/unreachable database connection" pattern — closing it
// again via this function's own t.Cleanup at test end is harmless (every
// other caller ignores this return value entirely).
func testServerWithAI(t *testing.T, adapter capai.Adapter) (http.Handler, authCreds, *db.DB) {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	migs = append(migs, content.Migrations...)
	migs = append(migs, layout.Migrations...)
	migs = append(migs, preset.Migrations...)
	migs = append(migs, bundle.Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}
	comps := composition.NewStore(d)
	if err := comps.Save(context.Background(), nil, &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
	}); err != nil {
		t.Fatal(err)
	}
	identities := identity.NewService(d)
	sessions := identity.NewSessions(d)
	registry := blocks.New()
	if err := firstparty.RegisterAll(registry); err != nil {
		t.Fatal(err)
	}
	contentAPI := content.NewAPI(comps, content.NewStore(d))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := api.New(comps, contentAPI, newTestMediaAPI(t, d), identities, sessions, log,
		api.WithLayouts(layout.NewStore(d), registry),
		api.WithPresets(preset.NewStore(d), bundle.NewStore(d)),
		api.WithAI(capai.NewService(adapter)),
	)
	mux := http.NewServeMux()
	srv.Routes(mux)

	if err := identities.CreateAdmin(context.Background(), "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	rec := do(t, mux, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "admin@example.com", "password": "correct horse battery",
	})
	return mux, sessionCookie(t, rec), d
}

const validFragmentJSON = `{
	"contract_version": "composition-preset/v1",
	"name": "ai-hero",
	"description": "a hero section",
	"layout": {
		"contract_version": "layout-composition/v1",
		"regions": {
			"main": {
				"blocks": [ { "type": "heading", "props": { "text": "Welcome" } } ]
			}
		}
	},
	"manifest": {
		"requires_contract": "layout-composition/v1",
		"blocks": ["heading"],
		"slots": ["main"]
	}
}`

const invalidBlockFragmentJSON = `{
	"contract_version": "composition-preset/v1",
	"name": "ai-hero",
	"layout": {
		"contract_version": "layout-composition/v1",
		"regions": {
			"main": {
				"blocks": [ { "type": "does-not-exist" } ]
			}
		}
	},
	"manifest": {
		"requires_contract": "layout-composition/v1",
		"blocks": ["does-not-exist"],
		"slots": ["main"]
	}
}`

func composeBody() map[string]any {
	return map[string]any{
		"prompt": "a friendly hero section",
		"route":  "home",
		"model":  "fake-model",
	}
}

// TestAIComposeIs404WhenNotConfigured must use an authenticated admin
// request (authedServer, no WithAI) rather than an anonymous one against
// testServer: an anonymous caller is rejected by requireCapability (401)
// before handleAICompose's own nil-service check ever runs, which would
// prove nothing about the "WithAI omitted" 404 path this test targets —
// mirrors TestLayoutPreviewIs404WhenNotConfigured's identical reasoning.
func TestAIComposeIs404WhenNotConfigured(t *testing.T) {
	h, cookie := authedServer(t)
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/ai/compose", cookie, composeBody())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST ai/compose without WithAI = %d, want 404, body %s", rec.Code, rec.Body.String())
	}
}

func TestAIComposeRequiresAdmin(t *testing.T) {
	h, _, _ := testServerWithAI(t, &fakeAIAdapter{text: validFragmentJSON})
	rec := do(t, h, http.MethodPost, "/api/v0/ai/compose", composeBody())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous POST ai/compose = %d, want 401", rec.Code)
	}
}

// TestAIComposeValidFragmentIsValidatedAndPreviewed is this ticket's
// success-path seam: a fake adapter returns a well-formed JSON fragment
// using only registered block types, and the endpoint returns it validated
// (compatible: true) plus a rendered HTML preview — never persisting
// anything (GET /api/v0/layouts/home below still 404s).
func TestAIComposeValidFragmentIsValidatedAndPreviewed(t *testing.T) {
	h, cookie, _ := testServerWithAI(t, &fakeAIAdapter{text: validFragmentJSON})
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/ai/compose", cookie, composeBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("POST ai/compose = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	if body["compatible"] != true {
		t.Fatalf("compatible = %v, want true, body %v", body["compatible"], body)
	}
	fragment, _ := body["fragment"].(map[string]any)
	if fragment == nil || fragment["name"] != "ai-hero" {
		t.Fatalf("fragment = %v, want the ai-hero fragment", body["fragment"])
	}
	preview, _ := body["preview"].(map[string]any)
	if preview == nil || preview["html"] == "" {
		t.Fatalf("preview = %v, want rendered html", body["preview"])
	}

	// Never persisted: no Layout was saved for "home" as a side effect.
	rec = do(t, h, http.MethodGet, "/api/v0/layouts/home", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET layouts/home after compose = %d, want 404 (compose must not persist)", rec.Code)
	}
}

// TestAIComposeUnknownBlockReturnsRealCompatDiagnostics is this ticket's
// failure-path seam: a fake adapter references a block type that isn't
// registered, and the endpoint must surface the exact same compat.Result
// diagnostic shape an incompatible preset import already produces today —
// not a generic/opaque AI error.
func TestAIComposeUnknownBlockReturnsRealCompatDiagnostics(t *testing.T) {
	h, cookie, _ := testServerWithAI(t, &fakeAIAdapter{text: invalidBlockFragmentJSON})
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/ai/compose", cookie, composeBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("POST ai/compose = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	if body["compatible"] != false {
		t.Fatalf("compatible = %v, want false", body["compatible"])
	}
	missing, _ := body["missing_blocks"].([]any)
	if len(missing) != 1 || missing[0] != "does-not-exist" {
		t.Fatalf("missing_blocks = %v, want [does-not-exist]", missing)
	}
	if _, ok := body["fragment"]; ok {
		t.Fatalf("fragment should be omitted on an incompatible result, got %v", body["fragment"])
	}
}

// TestAIComposeMalformedJSONReturnsValidationError covers the other
// documented failure mode: the model's raw text isn't JSON at all (or fails
// CompositionPreset.Validate's structural check) — this must map to the
// same 422 contract.ValidationErrors shape handlePresetCreate already
// returns for a structurally invalid preset, not a 200 or an opaque 500.
func TestAIComposeMalformedJSONReturnsValidationError(t *testing.T) {
	h, cookie, _ := testServerWithAI(t, &fakeAIAdapter{text: "not json at all"})
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/ai/compose", cookie, composeBody())
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST ai/compose with malformed model output = %d, want 422, body %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	if _, ok := body["issues"]; !ok {
		t.Fatalf("expected an `issues` list in the 422 body, got %v", body)
	}
}

func TestAIComposeRequiresPromptAndModel(t *testing.T) {
	h, cookie, _ := testServerWithAI(t, &fakeAIAdapter{text: validFragmentJSON})
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/ai/compose", cookie, map[string]any{"route": "home"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST ai/compose without prompt/model = %d, want 400, body %s", rec.Code, rec.Body.String())
	}
}

// TestAIComposeLayoutLoadErrorReturns500 proves handleAICompose distinguishes
// a real backend failure loading the target route's existing Layout from
// the true "no layout saved yet for this route" case
// (layout.ErrNotFound) — mirroring internal/preset.Store.Import's identical
// distinction. Dropping the `layouts` table out from under the running
// server (rather than closing the whole *db.DB, per
// internal/api/readiness_test.go's coarser "simulate a dropped/unreachable
// database connection" pattern — closing the whole DB here would also break
// this endpoint's own session-cookie lookup, producing a 401 that would
// prove nothing about this specific code path) forces
// layout.Store.Load to fail with a real, non-ErrNotFound error (the
// underlying `layouts` table no longer exists), which must surface as a
// real 500, not silently fall through to an empty-layout preview as if
// nothing had ever been saved for the route.
func TestAIComposeLayoutLoadErrorReturns500(t *testing.T) {
	h, cookie, d := testServerWithAI(t, &fakeAIAdapter{text: validFragmentJSON})
	if _, err := d.Exec(context.Background(), `DROP TABLE layouts`); err != nil {
		t.Fatal(err)
	}

	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/ai/compose", cookie, composeBody())
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("POST ai/compose with a broken layouts table = %d, want 500, body %s", rec.Code, rec.Body.String())
	}
}
