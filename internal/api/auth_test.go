package api_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	if me := decode(t, rec); me["email"] != "admin@example.com" {
		t.Errorf("/me email = %v", me["email"])
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

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == "glyphux_session" {
			if !c.HttpOnly {
				t.Error("session cookie is not HttpOnly")
			}
			return c
		}
	}
	t.Fatal("no session cookie set")
	return nil
}

func doWithCookie(t *testing.T, h http.Handler, method, path string, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// authDeps exposes the identity services the auth tests seed against.
type authDeps struct {
	identities *identity.Service
	sessions   *identity.Sessions
}
