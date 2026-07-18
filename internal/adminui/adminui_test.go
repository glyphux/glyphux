package adminui_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glyphux/glyphux/internal/adminui"
)

// TestDeepRouteServesSPAShell proves a client-side route (no matching real
// file) still serves the SPA's index.html on a hard navigation/reload.
func TestDeepRouteServesSPAShell(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/content-types", nil)
	rec := httptest.NewRecorder()
	adminui.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("deep route = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<!doctype html") {
		t.Errorf("deep route body = %q, want the SPA's index.html shell", rec.Body.String())
	}
}

// TestMissingAssetReturns404 proves a request for a build artifact that
// doesn't exist (e.g. a stale content-hashed chunk after a redeploy, or a
// typo'd URL) gets a real 404 — not the SPA shell, which would surface to
// the browser as "Unexpected token '<'" while loading a JS module instead of
// an honest missing-file error.
func TestMissingAssetReturns404(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/assets/index-doesnotexist123.js", nil)
	rec := httptest.NewRecorder()
	adminui.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset = %d, want 404, body %s", rec.Code, rec.Body.String())
	}
}
