package api_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glyphux/glyphux/capabilities/commerce"
	"github.com/glyphux/glyphux/capabilities/forms"
	"github.com/glyphux/glyphux/capabilities/membership"
	"github.com/glyphux/glyphux/capabilities/notifications"
	"github.com/glyphux/glyphux/capabilities/seo"
	"github.com/glyphux/glyphux/internal/api"
	"github.com/glyphux/glyphux/internal/audit"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/consent"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// consentServer boots the API transport with the consent engine wired via
// api.WithConsent (the T4 daemon path), over a real SQLite database with the
// consent+audit migrations, and the five first-party capability plugins
// registered — the same set cmd/glyphuxd registers at boot.
func consentServer(t *testing.T) (http.Handler, *consent.Engine) {
	t.Helper()
	ctx := context.Background()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	migs = append(migs, content.Migrations...)
	migs = append(migs, consent.Migrations...)
	migs = append(migs, audit.Migrations...)
	if err := d.Migrate(ctx, migs); err != nil {
		t.Fatal(err)
	}
	comps := composition.NewStore(d)
	if err := comps.Save(ctx, nil, &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
	}); err != nil {
		t.Fatal(err)
	}
	ids := identity.NewService(d)
	if err := ids.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if _, err := ids.CreateUser(ctx, "editor@example.com", "editor password", "editor"); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := consent.NewEngine(d, consent.WithAudit(audit.NewLogger(d)))
	srv := api.New(comps, content.NewAPI(comps, content.NewStore(d)), newTestMediaAPI(t, d), ids, identity.NewSessions(d), log,
		api.WithConsent(engine, firstPartyPlugins()),
	)
	mux := http.NewServeMux()
	srv.Routes(mux)
	return mux, engine
}

func firstPartyPlugins() []sdk.Plugin {
	return []sdk.Plugin{
		forms.New(),
		seo.New(),
		commerce.New(nil),
		membership.New(nil),
		notifications.New(notifications.NewMemoryMailerAdapter()),
	}
}

func loginAs(t *testing.T, h http.Handler, email, password string) authCreds {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{"email": email, "password": password})
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s = %d, body %s", email, rec.Code, rec.Body.String())
	}
	return sessionCookie(t, rec)
}

// TestPluginsListRequiresAdmin: the consent surface is admin-only — 401
// anonymous, 403 non-admin, 200 admin listing the five registered plugins
// (all undecided on a fresh store).
func TestPluginsListRequiresAdmin(t *testing.T) {
	h, _ := consentServer(t)

	rec := do(t, h, http.MethodGet, "/api/v0/plugins", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /plugins = %d, want 401", rec.Code)
	}

	editor := loginAsEditor(t, h)
	rec = doWithCookieBody(t, h, http.MethodGet, "/api/v0/plugins", editor, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor GET /plugins = %d, want 403", rec.Code)
	}

	admin := loginAsAdmin(t, h)
	rec = doWithCookieBody(t, h, http.MethodGet, "/api/v0/plugins", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin GET /plugins = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	list, _ := body["plugins"].([]any)
	if len(list) != 5 {
		t.Fatalf("plugins = %d entries, want 5", len(list))
	}
	for _, name := range []string{"forms", "seo", "commerce", "membership", "notifications"} {
		found := false
		for _, p := range list {
			if pm, ok := p.(map[string]any); ok && pm["name"] == name {
				found = true
				if pm["status"] != "undecided" {
					t.Errorf("%s status = %v, want undecided", name, pm["status"])
				}
			}
		}
		if !found {
			t.Errorf("plugins missing %q", name)
		}
	}
}

func loginAsAdmin(t *testing.T, h http.Handler) authCreds {
	t.Helper()
	return loginAs(t, h, "admin@example.com", "correct horse battery")
}

func loginAsEditor(t *testing.T, h http.Handler) authCreds {
	t.Helper()
	return loginAs(t, h, "editor@example.com", "editor password")
}

// TestConsentRequestsListsPending: on a fresh store every registered plugin
// is a pending request carrying its full permission request + fingerprint;
// a denied plugin stays pending (re-consent surfaced as pending).
func TestConsentRequestsListsPending(t *testing.T) {
	h, _ := consentServer(t)
	admin := loginAsAdmin(t, h)

	rec := doWithCookieBody(t, h, http.MethodGet, "/api/v0/plugins/consent-requests", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET consent-requests = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	reqs, _ := body["requests"].([]any)
	if len(reqs) != 5 {
		t.Fatalf("requests = %d, want 5 pending on a fresh store", len(reqs))
	}
	var commerceReq map[string]any
	for _, r := range reqs {
		if rm, ok := r.(map[string]any); ok && rm["name"] == "commerce" {
			commerceReq = rm
		}
	}
	if commerceReq == nil {
		t.Fatal("commerce request not listed")
	}
	if commerceReq["fingerprint"] == "" || len(commerceReq["fingerprint"].(string)) != 64 {
		t.Fatalf("commerce fingerprint = %v, want a 64-hex digest", commerceReq["fingerprint"])
	}
	apiAxis, _ := commerceReq["api"].([]any)
	if len(apiAxis) == 0 {
		t.Fatalf("commerce request api = %v, want the full permission request", commerceReq["api"])
	}

	// Deny commerce: it must remain pending (re-consent surfaced).
	rec = doWithCookieBody(t, h, http.MethodPost, "/api/v0/plugins/consent-requests/commerce/decide", admin, map[string]any{
		"decision": "denied",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("decide deny = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = doWithCookieBody(t, h, http.MethodGet, "/api/v0/plugins/consent-requests", admin, nil)
	body = decode(t, rec)
	reqs, _ = body["requests"].([]any)
	if len(reqs) != 5 {
		t.Fatalf("after deny, requests = %d, want 5 (denied plugin must surface re-consent as pending)", len(reqs))
	}
}

// TestDecidePartialGrantRoundTrip: an admin grants a strict subset of
// commerce's request; the decision is partial, GET /plugins reflects exactly
// the granted subset, and IsConsented exposes the same subset.
func TestDecidePartialGrantRoundTrip(t *testing.T) {
	ctx := context.Background()
	h, engine := consentServer(t)
	admin := loginAsAdmin(t, h)

	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/plugins/consent-requests/commerce/decide", admin, map[string]any{
		"decision": "partial",
		"granted_api": []any{
			map[string]any{"capability": "content", "scopes": []string{"read"}},
		},
		"granted_permissions": []any{},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("decide partial = %d, body %s", rec.Code, rec.Body.String())
	}
	dec := decode(t, rec)
	if dec["status"] != "partial" {
		t.Fatalf("decision status = %v, want partial", dec["status"])
	}

	// The engine exposes exactly the granted subset.
	live, ok, err := engine.IsConsented(ctx, commerce.New(nil).Manifest())
	if err != nil || !ok {
		t.Fatalf("IsConsented after partial grant = ok=%v err=%v", ok, err)
	}
	if len(live.GrantedAPI) != 1 || live.GrantedAPI[0].Capability != "content" || len(live.GrantedAPI[0].Scopes) != 1 || live.GrantedAPI[0].Scopes[0] != "read" {
		t.Fatalf("GrantedAPI = %+v, want exactly content:[read]", live.GrantedAPI)
	}

	// GET /plugins reflects the granted subset + status.
	rec = doWithCookieBody(t, h, http.MethodGet, "/api/v0/plugins", admin, nil)
	body := decode(t, rec)
	for _, p := range body["plugins"].([]any) {
		pm := p.(map[string]any)
		if pm["name"] == "commerce" {
			if pm["status"] != "partial" {
				t.Errorf("commerce status = %v, want partial", pm["status"])
			}
			granted, _ := pm["granted"].(map[string]any)
			api, _ := granted["api"].([]any)
			if len(api) != 1 {
				t.Errorf("commerce granted api = %v, want exactly content:[read]", granted)
			}
		}
	}
}

// TestDecideGrantExceedsRequestIs422: granting a capability the plugin
// never requested must fail with 422 (ErrGrantExceedsRequest).
func TestDecideGrantExceedsRequestIs422(t *testing.T) {
	h, _ := consentServer(t)
	admin := loginAsAdmin(t, h)

	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/plugins/consent-requests/commerce/decide", admin, map[string]any{
		"decision": "partial",
		"granted_api": []any{
			map[string]any{"capability": "users", "scopes": []string{"manage"}},
		},
		"granted_permissions": []any{},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("grant-exceeds-request = %d, want 422, body %s", rec.Code, rec.Body.String())
	}
}

// TestDecideDenyPersistsDenial: a denied decision persists, and the plugin
// stays unconsented.
func TestDecideDenyPersistsDenial(t *testing.T) {
	ctx := context.Background()
	h, engine := consentServer(t)
	admin := loginAsAdmin(t, h)

	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/plugins/consent-requests/commerce/decide", admin, map[string]any{
		"decision": "denied",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("decide deny = %d, body %s", rec.Code, rec.Body.String())
	}
	if _, live, _ := engine.IsConsented(ctx, commerce.New(nil).Manifest()); live {
		t.Fatal("denied plugin must not be consented")
	}
}

// TestDecideRequiresCSRFAndAdmin: mutations are CSRF-protected and
// admin-only — a valid session without the CSRF header is 403, an editor
// session is 403, an anonymous caller is 401.
func TestDecideRequiresCSRFAndAdmin(t *testing.T) {
	h, _ := consentServer(t)
	admin := loginAsAdmin(t, h)

	// Anonymous: 401 (requireUser fires before CSRF — no session cookie).
	rec := do(t, h, http.MethodPost, "/api/v0/plugins/consent-requests/commerce/decide", map[string]any{
		"decision": "denied",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous decide = %d, want 401", rec.Code)
	}

	// Editor session: 403 (plugins:manage is admin-only).
	editor := loginAsEditor(t, h)
	rec = doWithCookieBody(t, h, http.MethodPost, "/api/v0/plugins/consent-requests/commerce/decide", editor, map[string]any{
		"decision": "denied",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor decide = %d, want 403", rec.Code)
	}

	// Admin session WITHOUT the CSRF header: 403 (double-submit check).
	req := httptest.NewRequest(http.MethodPost, "/api/v0/plugins/consent-requests/commerce/decide",
		strings.NewReader(`{"decision":"denied"}`))
	admin.addTo(req) // addTo sets the CSRF header — strip it to simulate a naive client
	req.Header.Del("X-CSRF-Token")
	// keep only the session cookie, not the CSRF cookie
	req.Header.Del("Cookie")
	req.AddCookie(admin.session)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("decide without CSRF = %d, want 403", rec2.Code)
	}
}

// TestDecideDecisionRecordedByActingUser: DecidedBy must be the acting
// admin's user ID (the admin account consentServer creates is id 1).
func TestDecideDecisionRecordedByActingUser(t *testing.T) {
	ctx := context.Background()
	h, engine := consentServer(t)
	admin := loginAsAdmin(t, h)

	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/plugins/consent-requests/commerce/decide", admin, map[string]any{
		"decision": "approved",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("decide approve = %d, body %s", rec.Code, rec.Body.String())
	}
	dec, ok, err := engine.IsConsented(ctx, commerce.New(nil).Manifest())
	if err != nil || !ok {
		t.Fatalf("IsConsented after approve = ok=%v err=%v", ok, err)
	}
	if dec.DecidedBy != 1 {
		t.Fatalf("DecidedBy = %d, want the acting admin's user id (1)", dec.DecidedBy)
	}
}
