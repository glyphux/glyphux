package api_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/blocks/firstparty"
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

// testServerWithPresets boots a server with the Layer-2 block/layout
// transport AND the preset/bundle transport (WithLayouts + WithPresets)
// wired in, the first-party blocks registered, and an admin account already
// logged in — the equivalent of testServerWithLayouts for this ticket's
// routes.
func testServerWithPresets(t *testing.T) (http.Handler, authCreds) {
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
		ContentTypes: map[string]contract.ContentType{
			"article": {Fields: map[string]contract.Field{
				"title": {Type: contract.FieldString, Required: true},
			}},
		},
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
	)
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

func headingPresetBody() map[string]any {
	return map[string]any{
		"contract_version": "composition-preset/v1",
		"name":             "hero-section",
		"layout": map[string]any{
			"contract_version": "layout-composition/v1",
			"regions": map[string]any{
				"main": map[string]any{
					"blocks": []any{map[string]any{"type": "heading", "props": map[string]any{"text": "Hi"}}},
				},
			},
		},
		"manifest": map[string]any{
			"requires_contract": "layout-composition/v1",
			"blocks":            []any{"heading"},
			"slots":             []any{"main"},
		},
	}
}

func TestPresetsListIs404WhenNotConfigured(t *testing.T) {
	h := testServer(t)
	rec := do(t, h, http.MethodGet, "/api/v0/presets", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /presets without WithPresets = %d, want 404", rec.Code)
	}
}

func TestPresetCreateThenGetRoundTrips(t *testing.T) {
	h, cookie := testServerWithPresets(t)
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/presets", cookie, headingPresetBody())
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST preset = %d, body %s", rec.Code, rec.Body.String())
	}
	created := decode(t, rec)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("created preset has no id: %v", created)
	}

	rec = do(t, h, http.MethodGet, "/api/v0/presets/"+id, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET preset = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	if body["name"] != "hero-section" {
		t.Fatalf("name = %v, want hero-section", body["name"])
	}

	rec = do(t, h, http.MethodGet, "/api/v0/presets", nil)
	list := decode(t, rec)
	items, _ := list["presets"].([]any)
	if len(items) != 1 {
		t.Fatalf("presets list = %v, want one item", items)
	}
}

func TestPresetCreateRejectsUnregisteredBlockType(t *testing.T) {
	h, cookie := testServerWithPresets(t)
	body := headingPresetBody()
	body["layout"].(map[string]any)["regions"].(map[string]any)["main"].(map[string]any)["blocks"] = []any{
		map[string]any{"type": "does-not-exist"},
	}
	body["manifest"].(map[string]any)["blocks"] = []any{"does-not-exist"}
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/presets", cookie, body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST preset with unregistered block = %d, want 422, body %s", rec.Code, rec.Body.String())
	}
}

func TestPresetCreateRequiresAdmin(t *testing.T) {
	h, _ := testServerWithPresets(t)
	rec := do(t, h, http.MethodPost, "/api/v0/presets", headingPresetBody())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous POST preset = %d, want 401", rec.Code)
	}
}

func TestPresetCheckReportsMissingSlot(t *testing.T) {
	h, cookie := testServerWithPresets(t)
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/presets", cookie, headingPresetBody())
	id := decode(t, rec)["id"].(string)

	rec = do(t, h, http.MethodGet, "/api/v0/presets/"+id+"/check?theme_regions=header,footer", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET check = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	if body["compatible"] != false {
		t.Fatalf("compatible = %v, want false (main not declared)", body["compatible"])
	}
	missing, _ := body["missing_slots"].([]any)
	if len(missing) != 1 || missing[0] != "main" {
		t.Fatalf("missing_slots = %v, want [main]", missing)
	}
}

func TestPresetImportMergesIntoRouteWhenCompatible(t *testing.T) {
	h, cookie := testServerWithPresets(t)
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/presets", cookie, headingPresetBody())
	id := decode(t, rec)["id"].(string)

	rec = doWithCookieBody(t, h, http.MethodPost, "/api/v0/presets/"+id+"/import", cookie, map[string]any{
		"route": "home", "theme_regions": []any{"main"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST import = %d, body %s", rec.Code, rec.Body.String())
	}
	result := decode(t, rec)
	if result["compatible"] != true {
		t.Fatalf("import compatible = %v, want true, body %v", result["compatible"], result)
	}

	rec = do(t, h, http.MethodGet, "/api/v0/layouts/home", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET merged layout = %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestPresetImportRequiresAdmin(t *testing.T) {
	h, cookie := testServerWithPresets(t)
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/presets", cookie, headingPresetBody())
	id := decode(t, rec)["id"].(string)

	rec = do(t, h, http.MethodPost, "/api/v0/presets/"+id+"/import", map[string]any{"route": "home"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous import = %d, want 401", rec.Code)
	}
}
