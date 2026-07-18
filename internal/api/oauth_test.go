package api_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/api"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/pkg/contract"
)

// testServerWithOAuth is like testServerWithAuth but also wires an
// api.WithOAuth option — a separate helper (rather than growing
// testServerWithAuth's signature) since OAuth-configured servers are the
// exception, not the rule, across this package's tests.
func testServerWithOAuth(t *testing.T, opts ...api.Option) http.Handler {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	migs = append(migs, content.Migrations...)
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
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := api.New(comps, content.NewAPI(comps, content.NewStore(d)), newTestMediaAPI(t, d), identities, sessions, log, opts...)
	mux := http.NewServeMux()
	srv.Routes(mux)
	return mux
}

// fakeGitHubServer is a protocol-accurate double for GitHub's OAuth2
// endpoints, mirroring internal/identity/oauth_test.go's fakeGitHub — real
// end-to-end verification against github.com itself isn't practical in
// this sandbox (no live client credentials, no network egress), so this
// double exercises the exact request/response shapes the real server
// (internal/api's handleOAuthStart/handleOAuthCallback) drives.
func fakeGitHubServer(t *testing.T) (provider identity.OAuthProvider, code string, client *http.Client) {
	t.Helper()
	code = "test-code"
	const token = "test-token"
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.FormValue("code") != code {
			http.Error(w, "bad code", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token, "token_type": "bearer"})
	})
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "login": "octocat"})
	})
	mux.HandleFunc("GET /user/emails", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"email": "octocat@example.com", "primary": true, "verified": true},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return identity.OAuthProvider{
		Name:         "github",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		AuthURL:      srv.URL + "/login/oauth/authorize",
		TokenURL:     srv.URL + "/login/oauth/access_token",
		UserInfoURL:  srv.URL + "/user",
		EmailsURL:    srv.URL + "/user/emails",
		Scopes:       []string{"user:email"},
	}, code, srv.Client()
}

// TestOAuthLoginCreatesAccountEndToEnd drives the full authorization-code
// flow through internal/api's actual HTTP handlers (handleOAuthStart,
// handleOAuthCallback) against the fake GitHub double above: /start issues
// a redirect + state cookie, the test then plays "the browser coming back
// from GitHub" by hitting /callback with that state and a fake code, and
// the callback is expected to create a new (viewer-role) account and issue
// a real session.
func TestOAuthLoginCreatesAccountEndToEnd(t *testing.T) {
	provider, code, client := fakeGitHubServer(t)
	oauthMgr := identity.NewOAuthManager(client, provider)
	mux := testServerWithOAuth(t, api.WithOAuth(oauthMgr, "https://glyphux.example"))

	// Step 1: /start issues a redirect to the (fake) provider and sets the
	// CSRF state cookie.
	startReq := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oauth/github/start", nil)
	startRec := httptest.NewRecorder()
	mux.ServeHTTP(startRec, startReq)
	if startRec.Code != http.StatusFound {
		t.Fatalf("start = %d, want 302", startRec.Code)
	}
	loc, err := url.Parse(startRec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatal("no state in authorize redirect")
	}
	var stateCookie *http.Cookie
	for _, c := range startRec.Result().Cookies() {
		if c.Name == "glyphux_oauth_state" {
			stateCookie = c
		}
	}
	if stateCookie == nil {
		t.Fatal("no oauth state cookie set")
	}

	// Step 2: the "browser" hits /callback with the provider's code+state.
	callbackReq := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oauth/github/callback?code="+code+"&state="+state, nil)
	callbackReq.AddCookie(stateCookie)
	callbackRec := httptest.NewRecorder()
	mux.ServeHTTP(callbackRec, callbackReq)
	if callbackRec.Code != http.StatusOK {
		t.Fatalf("callback = %d, body %s", callbackRec.Code, callbackRec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(callbackRec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["email"] != "octocat@example.com" {
		t.Errorf("email = %v, want octocat@example.com", body["email"])
	}
	if body["role"] != "viewer" {
		t.Errorf("new OAuth account role = %v, want viewer", body["role"])
	}
	if body["active"] != true {
		t.Errorf("new OAuth account active = %v, want true", body["active"])
	}
	if body["token"] == nil || body["token"] == "" {
		t.Error("expected a session token")
	}

	// Regression: the OAuth callback must set a CSRF cookie exactly like a
	// normal login does, or an OAuth-only account could never pass
	// requireCSRF on any mutation route.
	var sawCSRFCookie bool
	for _, c := range callbackRec.Result().Cookies() {
		if c.Name == "glyphux_csrf" && c.Value != "" {
			sawCSRFCookie = true
		}
	}
	if !sawCSRFCookie {
		t.Error("OAuth callback response set no CSRF cookie")
	}
}

func TestOAuthRoutesDisabledWithoutConfiguration(t *testing.T) {
	h := testServer(t)
	rec := do(t, h, http.MethodGet, "/api/v0/auth/oauth/github/start", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("start without OAuth configured = %d, want 404", rec.Code)
	}
}
