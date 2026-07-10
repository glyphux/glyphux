package api_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/glyphux/glyphux/pkg/contract"
)

func testServer(t *testing.T) http.Handler {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, composition.Migrations...), content.Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}
	comps := composition.NewStore(d)
	if err := comps.Save(context.Background(), &contract.Composition{
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
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := api.New(comps, content.NewAPI(comps, content.NewStore(d)), log)
	mux := http.NewServeMux()
	srv.Routes(mux)
	return mux
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

func TestContentCRUDOverHTTP(t *testing.T) {
	h := testServer(t)

	// Create.
	rec := do(t, h, http.MethodPost, "/api/v0/content/article", map[string]any{"title": "Hello", "body": "World"})
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
	rec = do(t, h, http.MethodPut, "/api/v0/content/article/"+id, map[string]any{"title": "Changed"})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", rec.Code, rec.Body.String())
	}

	// Delete.
	rec = do(t, h, http.MethodDelete, "/api/v0/content/article/"+id, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodGet, "/api/v0/content/article/"+id, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET after delete = %d, want 404", rec.Code)
	}
}

func TestContentHTTPErrorMapping(t *testing.T) {
	h := testServer(t)

	// Unknown type → 404.
	if rec := do(t, h, http.MethodPost, "/api/v0/content/widget", map[string]any{"x": 1}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown type POST = %d, want 404", rec.Code)
	}
	// Validation failure → 422.
	if rec := do(t, h, http.MethodPost, "/api/v0/content/article", map[string]any{"body": "no title"}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid POST = %d, want 422", rec.Code)
	}
	// Malformed JSON → 400.
	req := httptest.NewRequest(http.MethodPost, "/api/v0/content/article", bytes.NewReader([]byte("{not json")))
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
