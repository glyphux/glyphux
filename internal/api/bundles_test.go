package api_test

import (
	"net/http"
	"testing"
)

func starterBundleBody() map[string]any {
	return map[string]any{
		"contract_version": "composition-bundle/v1",
		"name":             "starter-site",
		"theme":            "starter",
		"pages": map[string]any{
			"home": map[string]any{
				"contract_version": "layout-composition/v1",
				"regions": map[string]any{
					"main": map[string]any{
						"blocks": []any{map[string]any{"type": "heading", "props": map[string]any{"text": "Hi"}}},
					},
				},
			},
		},
		"sample_content": []any{
			map[string]any{"type": "article", "data": map[string]any{"title": "Hello world"}},
		},
		"manifest": map[string]any{
			"requires_contract": "layout-composition/v1",
			"blocks":            []any{"heading"},
			"slots":             []any{"main"},
		},
	}
}

func TestBundlesListIs404WhenNotConfigured(t *testing.T) {
	h := testServer(t)
	rec := do(t, h, http.MethodGet, "/api/v0/bundles", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /bundles without WithPresets = %d, want 404", rec.Code)
	}
}

func TestBundleCreateThenGetRoundTrips(t *testing.T) {
	h, cookie := testServerWithPresets(t)
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/bundles", cookie, starterBundleBody())
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST bundle = %d, body %s", rec.Code, rec.Body.String())
	}
	created := decode(t, rec)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("created bundle has no id: %v", created)
	}

	rec = do(t, h, http.MethodGet, "/api/v0/bundles/"+id, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET bundle = %d, body %s", rec.Code, rec.Body.String())
	}
	if decode(t, rec)["name"] != "starter-site" {
		t.Fatalf("name mismatch: %v", decode(t, rec))
	}
}

func TestBundleImportMergesPagesAndCreatesSampleContent(t *testing.T) {
	h, cookie := testServerWithPresets(t)
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/bundles", cookie, starterBundleBody())
	id := decode(t, rec)["id"].(string)

	rec = doWithCookieBody(t, h, http.MethodPost, "/api/v0/bundles/"+id+"/import", cookie, map[string]any{
		"theme_regions": []any{"main"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST bundle import = %d, body %s", rec.Code, rec.Body.String())
	}
	result := decode(t, rec)
	compat, _ := result["compat"].(map[string]any)
	if compat["compatible"] != true {
		t.Fatalf("import compat = %v, want compatible, full body %v", compat, result)
	}
	pages, _ := result["imported_pages"].([]any)
	if len(pages) != 1 || pages[0] != "home" {
		t.Fatalf("imported_pages = %v, want [home]", pages)
	}
	created, _ := result["created_content"].([]any)
	if len(created) != 1 {
		t.Fatalf("created_content = %v, want one item", created)
	}

	rec = do(t, h, http.MethodGet, "/api/v0/layouts/home", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET merged layout = %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestBundleImportRequiresAdmin(t *testing.T) {
	h, cookie := testServerWithPresets(t)
	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/bundles", cookie, starterBundleBody())
	id := decode(t, rec)["id"].(string)

	rec = do(t, h, http.MethodPost, "/api/v0/bundles/"+id+"/import", map[string]any{})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous bundle import = %d, want 401", rec.Code)
	}
}
