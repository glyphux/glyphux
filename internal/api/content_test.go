package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func doWithCookieBody(t *testing.T, h http.Handler, method, path string, c *http.Cookie, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func testServer(t *testing.T) http.Handler {
	t.Helper()
	h, _ := testServerWithAuth(t)
	return h
}

// authedServer boots a server with an admin account already logged in,
// returning the handler and the session cookie for driving mutations.
func authedServer(t *testing.T) (http.Handler, *http.Cookie) {
	t.Helper()
	h, deps := testServerWithAuth(t)
	ctx := context.Background()
	if err := deps.identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	rec := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "admin@example.com", "password": "correct horse battery",
	})
	return h, sessionCookie(t, rec)
}

func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return m
}

// localizedServer boots a server whose article type declares a localized
// title field, with an admin already logged in.
func localizedServer(t *testing.T) (http.Handler, *http.Cookie) {
	t.Helper()
	h, deps := testServerWithLocalizedContentType(t)
	ctx := context.Background()
	if err := deps.identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	rec := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "admin@example.com", "password": "correct horse battery",
	})
	return h, sessionCookie(t, rec)
}

func TestContentCRUDOverHTTP(t *testing.T) {
	h, cookie := authedServer(t)

	// Create.
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/content/article", cookie, map[string]any{"title": "Hello", "body": "World"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, body %s", rec.Code, rec.Body.String())
	}
	created := decode(t, rec)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("created item has no id: %v", created)
	}

	// Get.
	rec = do(t, h, http.MethodGet, "/api/v0/content/article/"+id, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d", rec.Code)
	}
	got := decode(t, rec)
	data, _ := got["data"].(map[string]any)
	if data["title"] != "Hello" {
		t.Errorf("GET title = %v, want Hello", data["title"])
	}

	// List.
	rec = do(t, h, http.MethodGet, "/api/v0/content/article", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("LIST status = %d", rec.Code)
	}
	list := decode(t, rec)
	items, _ := list["items"].([]any)
	if len(items) != 1 {
		t.Errorf("LIST returned %d items, want 1", len(items))
	}

	// Update.
	rec = doWithCookieBody(t, h, http.MethodPut, "/api/v0/content/article/"+id, cookie, map[string]any{"title": "Changed"})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", rec.Code, rec.Body.String())
	}

	// Delete.
	rec = doWithCookieBody(t, h, http.MethodDelete, "/api/v0/content/article/"+id, cookie, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodGet, "/api/v0/content/article/"+id, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET after delete = %d, want 404", rec.Code)
	}
}

func TestContentHTTPErrorMapping(t *testing.T) {
	h, cookie := authedServer(t)

	// Unknown type → 404.
	if rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/content/widget", cookie, map[string]any{"x": 1}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown type POST = %d, want 404", rec.Code)
	}
	// Validation failure → 422.
	if rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/content/article", cookie, map[string]any{"body": "no title"}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid POST = %d, want 422", rec.Code)
	}
	// Malformed JSON → 400.
	req := httptest.NewRequest(http.MethodPost, "/api/v0/content/article", bytes.NewReader([]byte("{not json")))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("malformed POST = %d, want 400", rec.Code)
	}
	// Missing id → 404.
	if rec := do(t, h, http.MethodGet, "/api/v0/content/article/nope", nil); rec.Code != http.StatusNotFound {
		t.Errorf("missing GET = %d, want 404", rec.Code)
	}
}

// Ping remains reachable — the {type} route must not shadow it.
func TestPingStillRoutes(t *testing.T) {
	h := testServer(t)
	if rec := do(t, h, http.MethodGet, "/api/v0/content/ping", nil); rec.Code != http.StatusOK {
		t.Errorf("ping = %d, want 200", rec.Code)
	}
}

// Publish, unpublish, list-versions, and rollback are reachable over HTTP;
// publish/unpublish/rollback are mutations gated behind auth (slice 1.5).
func TestContentPublishVersionsRollbackOverHTTP(t *testing.T) {
	h, cookie := authedServer(t)

	created := decode(t, doWithCookieBody(t, h, http.MethodPost, "/api/v0/content/article", cookie, map[string]any{"title": "V1"}))
	id, _ := created["id"].(string)

	// Anonymous publish is rejected.
	if rec := do(t, h, http.MethodPost, "/api/v0/content/article/"+id+"/publish", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon publish = %d, want 401", rec.Code)
	}

	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/content/article/"+id+"/publish", cookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish = %d, body %s", rec.Code, rec.Body.String())
	}
	if decode(t, rec)["status"] != "published" {
		t.Errorf("status after publish = %v, want published", decode(t, rec)["status"])
	}

	doWithCookieBody(t, h, http.MethodPut, "/api/v0/content/article/"+id, cookie, map[string]any{"title": "V2"})

	// Versions are readable without auth.
	rec = do(t, h, http.MethodGet, "/api/v0/content/article/"+id+"/versions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list versions = %d", rec.Code)
	}
	versions, _ := decode(t, rec)["versions"].([]any)
	if len(versions) != 2 {
		t.Fatalf("versions returned %d entries, want 2", len(versions))
	}

	rec = doWithCookieBody(t, h, http.MethodPost, "/api/v0/content/article/"+id+"/rollback/1", cookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("rollback = %d, body %s", rec.Code, rec.Body.String())
	}
	got := decode(t, rec)
	data, _ := got["data"].(map[string]any)
	if data["title"] != "V1" {
		t.Errorf("rollback title = %v, want V1", data["title"])
	}

	rec = doWithCookieBody(t, h, http.MethodPost, "/api/v0/content/article/"+id+"/unpublish", cookie, nil)
	if rec.Code != http.StatusOK || decode(t, rec)["status"] != "draft" {
		t.Fatalf("unpublish = %d, status %v", rec.Code, decode(t, rec)["status"])
	}
}

// The ?locale= query param resolves localized fields to a single value on
// both single-item and list reads (slice 1.4: localization).
func TestContentLocaleQueryParamResolvesLocalizedFields(t *testing.T) {
	h, cookie := localizedServer(t)

	created := decode(t, doWithCookieBody(t, h, http.MethodPost, "/api/v0/content/article", cookie, map[string]any{
		"title": map[string]any{"en": "Hello", "fr": "Bonjour"},
	}))
	id, _ := created["id"].(string)

	rec := do(t, h, http.MethodGet, "/api/v0/content/article/"+id+"?locale=fr", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET locale=fr = %d", rec.Code)
	}
	data, _ := decode(t, rec)["data"].(map[string]any)
	if data["title"] != "Bonjour" {
		t.Errorf("title = %v, want Bonjour", data["title"])
	}

	rec = do(t, h, http.MethodGet, "/api/v0/content/article?locale=en", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("LIST locale=en = %d", rec.Code)
	}
	items, _ := decode(t, rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("LIST returned %d items, want 1", len(items))
	}
	first, _ := items[0].(map[string]any)
	firstData, _ := first["data"].(map[string]any)
	if firstData["title"] != "Hello" {
		t.Errorf("list title = %v, want Hello", firstData["title"])
	}

	// Without ?locale=, the raw locale map passes through.
	rec = do(t, h, http.MethodGet, "/api/v0/content/article/"+id, nil)
	data, _ = decode(t, rec)["data"].(map[string]any)
	title, _ := data["title"].(map[string]any)
	if title["en"] != "Hello" || title["fr"] != "Bonjour" {
		t.Errorf("unresolved title = %v", title)
	}
}

// Content mutations require an authenticated admin session; reads stay
// public (slice 1.8: permission engine + auth enforcement).
func TestContentMutationsRequireAuth(t *testing.T) {
	h, deps := testServerWithAuth(t)
	ctx := context.Background()
	if err := deps.identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}

	// Unauthenticated create is rejected.
	if rec := do(t, h, http.MethodPost, "/api/v0/content/article", map[string]any{"title": "Hi"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon POST = %d, want 401", rec.Code)
	}

	// Reads remain public.
	if rec := do(t, h, http.MethodGet, "/api/v0/content/article", nil); rec.Code != http.StatusOK {
		t.Fatalf("anon LIST = %d, want 200", rec.Code)
	}

	// Log in as admin and drive the full mutation path with the session cookie.
	loginRec := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "admin@example.com", "password": "correct horse battery",
	})
	cookie := sessionCookie(t, loginRec)

	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/content/article", cookie, map[string]any{"title": "Hi"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("authed POST = %d, body %s", rec.Code, rec.Body.String())
	}
	id, _ := decode(t, rec)["id"].(string)

	if rec := doWithCookieBody(t, h, http.MethodPut, "/api/v0/content/article/"+id, cookie, map[string]any{"title": "Bye"}); rec.Code != http.StatusOK {
		t.Fatalf("authed PUT = %d, body %s", rec.Code, rec.Body.String())
	}

	// Unauthenticated update/delete on that same item are still rejected.
	if rec := do(t, h, http.MethodPut, "/api/v0/content/article/"+id, map[string]any{"title": "Nope"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon PUT = %d, want 401", rec.Code)
	}
	if rec := do(t, h, http.MethodDelete, "/api/v0/content/article/"+id, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon DELETE = %d, want 401", rec.Code)
	}

	if rec := doWithCookieBody(t, h, http.MethodDelete, "/api/v0/content/article/"+id, cookie, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("authed DELETE = %d", rec.Code)
	}
}
