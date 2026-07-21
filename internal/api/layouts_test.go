package api_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glyphux/glyphux/blocks/firstparty"
	"github.com/glyphux/glyphux/internal/api"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/contract"
)

// testServerWithLayouts boots a server with the Layer-2 block/layout
// transport wired in (WithLayouts), the first-party blocks registered, and
// an admin account already logged in — the equivalent of authedServer
// (content_test.go) for this slice's routes.
func testServerWithLayouts(t *testing.T) (http.Handler, authCreds) {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	migs = append(migs, content.Migrations...)
	migs = append(migs, layout.Migrations...)
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
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := api.New(comps, content.NewAPI(comps, content.NewStore(d)), newTestMediaAPI(t, d), identities, sessions, log,
		api.WithLayouts(layout.NewStore(d), registry))
	mux := http.NewServeMux()
	srv.Routes(mux)

	if err := identities.CreateAdmin(context.Background(), "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	rec := do(t, mux, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "admin@example.com", "password": "correct horse battery",
	})
	return mux, sessionCookie(t, rec)
}

func TestBlocksListReturnsFirstPartyBlocks(t *testing.T) {
	h, _ := testServerWithLayouts(t)
	rec := do(t, h, http.MethodGet, "/api/v0/blocks", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /blocks = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	list, _ := body["blocks"].([]any)
	if len(list) == 0 {
		t.Fatalf("blocks = %v, want first-party blocks present", list)
	}
	var names []string
	for _, b := range list {
		m, _ := b.(map[string]any)
		names = append(names, m["name"].(string))
	}
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	for _, want := range []string{"heading", "paragraph", "image", "container"} {
		if !found[want] {
			t.Errorf("blocks list = %v, want %q present", names, want)
		}
	}
}

func TestBlocksListIs404WhenNotConfigured(t *testing.T) {
	h := testServer(t)
	rec := do(t, h, http.MethodGet, "/api/v0/blocks", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /blocks without WithLayouts = %d, want 404", rec.Code)
	}
}

func TestLayoutGetReturns404WhenUnsaved(t *testing.T) {
	h, _ := testServerWithLayouts(t)
	rec := do(t, h, http.MethodGet, "/api/v0/layouts/home", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET unsaved layout = %d, want 404, body %s", rec.Code, rec.Body.String())
	}
}

func validLayoutBody() map[string]any {
	return map[string]any{
		"contract_version": "layout-composition/v1",
		"regions": map[string]any{
			"main": map[string]any{
				"blocks": []any{
					map[string]any{"type": "heading", "props": map[string]any{"text": "Hello"}},
				},
			},
		},
	}
}

func TestLayoutPutThenGetRoundTrips(t *testing.T) {
	h, cookie := testServerWithLayouts(t)
	rec := doWithCookieBody(t, h, http.MethodPut, "/api/v0/layouts/home", cookie, validLayoutBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT layout = %d, body %s", rec.Code, rec.Body.String())
	}

	rec = do(t, h, http.MethodGet, "/api/v0/layouts/home", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET layout after PUT = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	regions, _ := body["regions"].(map[string]any)
	if _, ok := regions["main"]; !ok {
		t.Fatalf("regions = %v, want main present", regions)
	}
}

func TestLayoutPutAcceptsMultiSegmentRoute(t *testing.T) {
	h, cookie := testServerWithLayouts(t)
	rec := doWithCookieBody(t, h, http.MethodPut, "/api/v0/layouts/blog/index", cookie, validLayoutBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT blog/index = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/api/v0/layouts/blog/index", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET blog/index = %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestLayoutPutRejectsUnregisteredBlockType(t *testing.T) {
	h, cookie := testServerWithLayouts(t)
	body := map[string]any{
		"contract_version": "layout-composition/v1",
		"regions": map[string]any{
			"main": map[string]any{
				"blocks": []any{map[string]any{"type": "does-not-exist"}},
			},
		},
	}
	rec := doWithCookieBody(t, h, http.MethodPut, "/api/v0/layouts/home", cookie, body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("PUT unregistered block type = %d, want 422, body %s", rec.Code, rec.Body.String())
	}
	respBody := decode(t, rec)
	if respBody["error"] != "validation failed" {
		t.Errorf("error = %v, want validation failed", respBody["error"])
	}
	if _, ok := respBody["issues"]; !ok {
		t.Errorf("body = %v, want issues field", respBody)
	}
}

func TestLayoutPutRejectsStructurallyInvalidLayout(t *testing.T) {
	h, cookie := testServerWithLayouts(t)
	body := map[string]any{"contract_version": "wrong-version"}
	rec := doWithCookieBody(t, h, http.MethodPut, "/api/v0/layouts/home", cookie, body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("PUT invalid contract version = %d, want 422, body %s", rec.Code, rec.Body.String())
	}
}

func TestLayoutPutRequiresAdmin(t *testing.T) {
	h, _ := testServerWithLayouts(t)
	rec := do(t, h, http.MethodPut, "/api/v0/layouts/home", validLayoutBody())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous PUT = %d, want 401", rec.Code)
	}
}

func TestLayoutGetIsPublic(t *testing.T) {
	h, cookie := testServerWithLayouts(t)
	doWithCookieBody(t, h, http.MethodPut, "/api/v0/layouts/home", cookie, validLayoutBody())

	rec := do(t, h, http.MethodGet, "/api/v0/layouts/home", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("anonymous GET layout = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
}

// --- Ticket P4.5: POST /api/v0/layouts/preview ---
//
// Renders an UNSAVED draft Layout through the real themes/starter theme and
// returns the resulting HTML — "editor renders the same composition the API
// serves" for a draft that may never have been PUT at all. Never persists
// anything (see TestLayoutPreviewDoesNotPersistTheDraft).

func TestLayoutPreviewRendersDraftThroughRealStarterTheme(t *testing.T) {
	h, cookie := testServerWithLayouts(t)
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/layouts/preview", cookie, validLayoutBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("POST preview = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	html, _ := body["html"].(string)
	if !strings.Contains(html, "<h1>Hello</h1>") {
		t.Errorf("html = %q, want it to contain the rendered heading block", html)
	}
	if body["content_type"] != "text/html; charset=utf-8" {
		t.Errorf("content_type = %v, want starter's HTML content type", body["content_type"])
	}
}

func TestLayoutPreviewEscapesUntrustedProps(t *testing.T) {
	h, cookie := testServerWithLayouts(t)
	body := map[string]any{
		"contract_version": "layout-composition/v1",
		"regions": map[string]any{
			"main": map[string]any{
				"blocks": []any{
					map[string]any{"type": "paragraph", "props": map[string]any{"text": "<script>alert(1)</script>"}},
				},
			},
		},
	}
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/layouts/preview", cookie, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST preview = %d, body %s", rec.Code, rec.Body.String())
	}
	respBody := decode(t, rec)
	html, _ := respBody["html"].(string)
	if strings.Contains(html, "<script>") {
		t.Errorf("html = %q, want the untrusted prop escaped, not rendered as live markup", html)
	}
}

func TestLayoutPreviewDoesNotPersistTheDraft(t *testing.T) {
	h, cookie := testServerWithLayouts(t)
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/layouts/preview", cookie, validLayoutBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("POST preview = %d, body %s", rec.Code, rec.Body.String())
	}

	rec = do(t, h, http.MethodGet, "/api/v0/layouts/home", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET home after preview-only = %d, want 404 (preview must not persist)", rec.Code)
	}
}

func TestLayoutPreviewRejectsUnregisteredBlockType(t *testing.T) {
	h, cookie := testServerWithLayouts(t)
	body := map[string]any{
		"contract_version": "layout-composition/v1",
		"regions": map[string]any{
			"main": map[string]any{
				"blocks": []any{map[string]any{"type": "does-not-exist"}},
			},
		},
	}
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/layouts/preview", cookie, body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST preview unregistered block type = %d, want 422, body %s", rec.Code, rec.Body.String())
	}
	respBody := decode(t, rec)
	if respBody["error"] != "validation failed" {
		t.Errorf("error = %v, want validation failed", respBody["error"])
	}
}

func TestLayoutPreviewRejectsStructurallyInvalidLayout(t *testing.T) {
	h, cookie := testServerWithLayouts(t)
	body := map[string]any{"contract_version": "wrong-version"}
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/layouts/preview", cookie, body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST preview invalid contract version = %d, want 422, body %s", rec.Code, rec.Body.String())
	}
}

func TestLayoutPreviewRequiresAdmin(t *testing.T) {
	h, _ := testServerWithLayouts(t)
	rec := do(t, h, http.MethodPost, "/api/v0/layouts/preview", validLayoutBody())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous POST preview = %d, want 401", rec.Code)
	}
}

func TestLayoutPreviewIs404WhenNotConfigured(t *testing.T) {
	// Must be an authenticated admin request (authedServer, no WithLayouts)
	// rather than an anonymous one against testServer: an anonymous caller
	// is rejected by requireCapability (401) before handleLayoutPreview's
	// own nil-store check ever runs, which would prove nothing about the
	// "WithLayouts omitted" 404 path this test targets.
	h, cookie := authedServer(t)
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/layouts/preview", cookie, validLayoutBody())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST preview without WithLayouts = %d, want 404, body %s", rec.Code, rec.Body.String())
	}
}
