package api_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestContentTypesListIsPublic(t *testing.T) {
	h := testServer(t)
	rec := do(t, h, http.MethodGet, "/api/v0/content-types", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /content-types = %d, want 200", rec.Code)
	}
	body := decode(t, rec)
	types, _ := body["content_types"].(map[string]any)
	if _, ok := types["article"]; !ok {
		t.Errorf("content_types = %v, want article present (seeded by testServerWithAuth)", types)
	}
}

// TestContentTypesListReturnsEmptyObjectNotNull proves a composition with no
// content types declared serializes content_types as {}, not JSON null — a
// nil Go map and a genuinely-empty one are indistinguishable to callers
// otherwise, and null broke non-Go clients expecting an iterable record
// (found via the admin shell's real end-to-end pass; the JS SDK papers over
// it client-side, but the wire contract itself should not emit null).
func TestContentTypesListReturnsEmptyObjectNotNull(t *testing.T) {
	h, cookie := authedServer(t)

	// testServerWithAuth seeds one content type ("article"); remove it via
	// the endpoint under test's own sibling route so the composition's
	// ContentTypes map is genuinely empty for this request.
	rec := doWithCookieBody(t, h, http.MethodDelete, "/api/v0/content-types/article", cookie, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE article = %d, want 204, body %s", rec.Code, rec.Body.String())
	}

	rec = do(t, h, http.MethodGet, "/api/v0/content-types", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /content-types = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, `"content_types":{}`) {
		t.Errorf("body = %s, want content_types to serialize as {} not null", got)
	}
}

func TestContentTypesPutCreatesAndUpdates(t *testing.T) {
	h, cookie := authedServer(t)

	rec := doWithCookieBody(t, h, http.MethodPut, "/api/v0/content-types/product", cookie, map[string]any{
		"fields": map[string]any{
			"name": map[string]any{"type": "string", "required": true},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT create = %d, body %s", rec.Code, rec.Body.String())
	}
	created := decode(t, rec)
	fields, _ := created["fields"].(map[string]any)
	if len(fields) != 1 {
		t.Fatalf("PUT create fields = %v, want 1", fields)
	}

	// A subsequent read via /content-types reflects it.
	rec = doWithCookieBody(t, h, http.MethodGet, "/api/v0/content-types", cookie, nil)
	body := decode(t, rec)
	types, _ := body["content_types"].(map[string]any)
	if _, ok := types["product"]; !ok {
		t.Fatalf("content_types after PUT = %v, want product present", types)
	}

	// Update: replace the field set.
	rec = doWithCookieBody(t, h, http.MethodPut, "/api/v0/content-types/product", cookie, map[string]any{
		"fields": map[string]any{
			"name":  map[string]any{"type": "string", "required": true},
			"price": map[string]any{"type": "number"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT update = %d, body %s", rec.Code, rec.Body.String())
	}
	updated := decode(t, rec)
	fields, _ = updated["fields"].(map[string]any)
	if len(fields) != 2 {
		t.Fatalf("PUT update fields = %v, want 2", fields)
	}
}

func TestContentTypesPutRequiresAdmin(t *testing.T) {
	h := testServer(t)
	rec := do(t, h, http.MethodPut, "/api/v0/content-types/product", map[string]any{
		"fields": map[string]any{"name": map[string]any{"type": "string"}},
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous PUT = %d, want 401", rec.Code)
	}
}

func TestContentTypesPutRejectsInvalidShape(t *testing.T) {
	h, cookie := authedServer(t)
	rec := doWithCookieBody(t, h, http.MethodPut, "/api/v0/content-types/broken", cookie, map[string]any{
		"fields": map[string]any{
			"linked": map[string]any{"type": "relation", "to": "does-not-exist"},
		},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("PUT invalid shape = %d, want 422, body %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	if body["error"] != "validation failed" {
		t.Errorf("error = %v, want validation failed", body["error"])
	}
	if _, ok := body["issues"]; !ok {
		t.Errorf("body = %v, want issues field", body)
	}
}

func TestContentTypesDeleteRemovesEmptyType(t *testing.T) {
	h, cookie := authedServer(t)
	doWithCookieBody(t, h, http.MethodPut, "/api/v0/content-types/tag", cookie, map[string]any{
		"fields": map[string]any{"name": map[string]any{"type": "string", "required": true}},
	})

	rec := doWithCookieBody(t, h, http.MethodDelete, "/api/v0/content-types/tag", cookie, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204, body %s", rec.Code, rec.Body.String())
	}

	rec = doWithCookieBody(t, h, http.MethodGet, "/api/v0/content-types", cookie, nil)
	body := decode(t, rec)
	types, _ := body["content_types"].(map[string]any)
	if _, ok := types["tag"]; ok {
		t.Errorf("content_types after DELETE = %v, want tag absent", types)
	}
}

func TestContentTypesDeleteUnknownReturns404(t *testing.T) {
	h, cookie := authedServer(t)
	rec := doWithCookieBody(t, h, http.MethodDelete, "/api/v0/content-types/does-not-exist", cookie, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE unknown = %d, want 404", rec.Code)
	}
}

func TestContentTypesDeleteBlockedWhenItemsExist(t *testing.T) {
	h, cookie := authedServer(t)
	// "article" is seeded by authedServer/testServerWithAuth.
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/content/article", cookie, map[string]any{
		"title": "Keeps the type alive", "body": "x",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed content: %d, body %s", rec.Code, rec.Body.String())
	}

	rec = doWithCookieBody(t, h, http.MethodDelete, "/api/v0/content-types/article", cookie, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("DELETE with items = %d, want 409, body %s", rec.Code, rec.Body.String())
	}

	// The type must still be usable — the guard did not partially apply.
	rec = doWithCookieBody(t, h, http.MethodGet, "/api/v0/content-types", cookie, nil)
	body := decode(t, rec)
	types, _ := body["content_types"].(map[string]any)
	if _, ok := types["article"]; !ok {
		t.Errorf("content_types after blocked DELETE = %v, want article still present", types)
	}
}

func TestContentTypesDeleteRequiresAdmin(t *testing.T) {
	h := testServer(t)
	rec := do(t, h, http.MethodDelete, "/api/v0/content-types/article", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous DELETE = %d, want 401", rec.Code)
	}
}
