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

	"github.com/glyphux/glyphux/internal/api"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/pkg/contract"
)

func newTestMediaAPI(t *testing.T, d *db.DB) *media.API {
	t.Helper()
	if err := d.Migrate(context.Background(), media.Migrations); err != nil {
		t.Fatal(err)
	}
	return media.NewAPI(media.NewStore(d), filepath.Join(t.TempDir(), "media"))
}

func testServerWithAuth(t *testing.T) (http.Handler, authDeps) {
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
		ContentTypes: map[string]contract.ContentType{
			"article": {Fields: map[string]contract.Field{
				"title": {Type: contract.FieldString, Required: true},
				"body":  {Type: contract.FieldRichText},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	deps := authDeps{identities: identity.NewService(d), sessions: identity.NewSessions(d)}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := api.New(comps, content.NewAPI(comps, content.NewStore(d)), newTestMediaAPI(t, d), deps.identities, deps.sessions, log)
	mux := http.NewServeMux()
	srv.Routes(mux)
	return mux, deps
}

// TestLoginCookieIgnoresUnvouchedForwardedProto proves the session cookie's
// Secure flag isn't set from a spoofable X-Forwarded-Proto header by default
// — only when the operator has explicitly opted in via TrustProxyHeaders,
// mirroring setup.Wizard's identical trust decision (internal/setup/setup.go).
// Trusting the header unconditionally would let a client behind no real
// proxy claim HTTPS and get a cookie that looks secure but is sent in the
// clear.
func TestLoginCookieIgnoresUnvouchedForwardedProto(t *testing.T) {
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}
	identities := identity.NewService(d)
	if err := identities.CreateAdmin(context.Background(), "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	comps := composition.NewStore(d)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	login := func(srv *api.Server) *httptest.ResponseRecorder {
		mux := http.NewServeMux()
		srv.Routes(mux)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/login", strings.NewReader(
			`{"email":"admin@example.com","password":"correct horse battery"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-Proto", "https")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	untrusted := api.New(comps, content.NewAPI(comps, content.NewStore(d)), newTestMediaAPI(t, d), identities, identity.NewSessions(d), log)
	rec := login(untrusted)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d, body %s", rec.Code, rec.Body.String())
	}
	if cookie := findSessionCookie(rec); cookie == nil || cookie.Secure {
		t.Fatalf("cookie Secure = %v, want false without TrustProxyHeaders opt-in", cookie)
	}

	trusted := api.New(comps, content.NewAPI(comps, content.NewStore(d)), newTestMediaAPI(t, d), identities, identity.NewSessions(d), log, api.TrustProxyHeaders(true))
	rec = login(trusted)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d, body %s", rec.Code, rec.Body.String())
	}
	if cookie := findSessionCookie(rec); cookie == nil || !cookie.Secure {
		t.Fatalf("cookie Secure = %v, want true with TrustProxyHeaders opt-in", cookie)
	}
}

func findSessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "glyphux_session" {
			return c
		}
	}
	return nil
}

// testServerWithLocalizedContentType is like testServerWithAuth but declares
// an article type with a localized title field, for slice 1.4 tests.
func testServerWithLocalizedContentType(t *testing.T) (http.Handler, authDeps) {
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
		ContentTypes: map[string]contract.ContentType{
			"article": {Fields: map[string]contract.Field{
				"title": {Type: contract.FieldString, Required: true, Localized: true},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	deps := authDeps{identities: identity.NewService(d), sessions: identity.NewSessions(d)}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := api.New(comps, content.NewAPI(comps, content.NewStore(d)), newTestMediaAPI(t, d), deps.identities, deps.sessions, log)
	mux := http.NewServeMux()
	srv.Routes(mux)
	return mux, deps
}

func TestAuthLoginMeLogout(t *testing.T) {
	h, deps := testServerWithAuth(t)
	ctx := context.Background()
	if err := deps.identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}

	// Unauthenticated /me is 401.
	if rec := do(t, h, http.MethodGet, "/api/v0/auth/me", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon /me = %d, want 401", rec.Code)
	}

	// Wrong password is 401.
	if rec := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "admin@example.com", "password": "nope",
	}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad login = %d, want 401", rec.Code)
	}

	// Correct login sets a session cookie.
	rec := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "admin@example.com", "password": "correct horse battery",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d, body %s", rec.Code, rec.Body.String())
	}
	cookie := sessionCookie(t, rec)

	// /me with the cookie returns the user.
	rec = doWithCookie(t, h, http.MethodGet, "/api/v0/auth/me", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("authed /me = %d", rec.Code)
	}
	me := decode(t, rec)
	if me["email"] != "admin@example.com" {
		t.Errorf("/me email = %v", me["email"])
	}
	if me["active"] != true {
		t.Errorf("/me active = %v, want true — regression check for Sessions.Lookup dropping the active column", me["active"])
	}

	// Logout revokes the session.
	rec = doWithCookie(t, h, http.MethodPost, "/api/v0/auth/logout", cookie)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", rec.Code)
	}
	rec = doWithCookie(t, h, http.MethodGet, "/api/v0/auth/me", cookie)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("/me after logout = %d, want 401", rec.Code)
	}
}

// TestLoginResponseIncludesBearerToken proves the login response body carries
// a usable bearer token, not just a Set-Cookie header — programmatic clients
// (the SDKs, scripts, server-to-server callers) have no cookie jar and cannot
// otherwise obtain a session token to authenticate subsequent requests.
func TestLoginResponseIncludesBearerToken(t *testing.T) {
	h, deps := testServerWithAuth(t)
	ctx := context.Background()
	if err := deps.identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}

	rec := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "admin@example.com", "password": "correct horse battery",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("login response has no token: %v", body)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	meRec := httptest.NewRecorder()
	h.ServeHTTP(meRec, req)
	if meRec.Code != http.StatusOK {
		t.Fatalf("bearer /me with login token = %d, want 200", meRec.Code)
	}
	if me := decode(t, meRec); me["email"] != "admin@example.com" {
		t.Errorf("/me email = %v", me["email"])
	}
}

func TestBearerTokenAuthenticates(t *testing.T) {
	h, deps := testServerWithAuth(t)
	ctx := context.Background()
	_ = deps.identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery")
	u, _ := deps.identities.Authenticate(ctx, "admin@example.com", "correct horse battery")
	sess, _ := deps.sessions.Create(ctx, u.ID)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+sess.Token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("bearer /me = %d, want 200", rec.Code)
	}
}

// authCreds bundles the cookies a real authenticated browser session
// carries: the HttpOnly session cookie and the CSRF cookie a mutating
// request must mirror in the X-CSRF-Token header (slice 1.9). Test helpers
// pass this around wherever a raw *http.Cookie used to suffice, so every
// existing cookie-driven mutation test also exercises the CSRF check
// exactly like a real browser would, without each call site needing to know
// about it individually.
type authCreds struct {
	session *http.Cookie
	csrf    *http.Cookie
}

// addTo attaches both cookies and the mirrored CSRF header to req, matching
// what a real authenticated browser request (or the admin SPA) sends.
func (c authCreds) addTo(req *http.Request) {
	req.AddCookie(c.session)
	if c.csrf != nil {
		req.AddCookie(c.csrf)
		req.Header.Set("X-CSRF-Token", c.csrf.Value)
	}
}

// sessionCookie extracts the authenticated session + CSRF cookies a login
// response set.
func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) authCreds {
	t.Helper()
	var creds authCreds
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case "glyphux_session":
			if !c.HttpOnly {
				t.Error("session cookie is not HttpOnly")
			}
			creds.session = c
		case "glyphux_csrf":
			if c.HttpOnly {
				t.Error("CSRF cookie must not be HttpOnly — client JS must be able to read it")
			}
			creds.csrf = c
		}
	}
	if creds.session == nil {
		t.Fatal("no session cookie set")
	}
	return creds
}

func doWithCookie(t *testing.T, h http.Handler, method, path string, c authCreds) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	c.addTo(req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// authDeps exposes the identity services the auth tests seed against.
type authDeps struct {
	identities *identity.Service
	sessions   *identity.Sessions
}
