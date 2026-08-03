// CORE-05 REVIEW (behavior-first): CSRF double-submit-cookie enforcement.
//
// Given a state-changing (POST) request riding a valid session cookie,
// When the CSRF middleware runs without a token, Then it is rejected
// (403). Given a valid mirrored token, Then it is accepted. Given a
// tampered token, Then it is rejected. Exercises the real
// internal/api.Server.requireCSRF through the HTTP surface
// (/api/v0/content/article), reusing the api_test auth fixtures.
package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestReviewCSRFRejectsMissingToken — session cookie only (the forged
// cross-site request): no CSRF cookie, no header -> 403.
func TestReviewCSRFRejectsMissingToken(t *testing.T) {
	t.Run("Given a POST riding only the session cookie, When requireCSRF runs, Then it is rejected 403 (missing token)", func(t *testing.T) {
		h, deps := testServerWithAuth(t)
		ctx := t.Context()
		if err := deps.identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
			t.Fatal(err)
		}
		rec := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
			"email": "admin@example.com", "password": "correct horse battery",
		})
		creds := sessionCookie(t, rec)
		t.Logf("Given login succeeded; session cookie present (CSRF cookie not yet issued for this request shape)")
		t.Logf("When  POST /api/v0/content/article carries only the session cookie (no CSRF token)")

		req := httptest.NewRequest(http.MethodPost, "/api/v0/content/article", nil)
		req.AddCookie(creds.session) // ambient session cookie only — no CSRF
		blocked := httptest.NewRecorder()
		h.ServeHTTP(blocked, req)
		if blocked.Code != http.StatusForbidden {
			t.Errorf("FAIL: cookie-only mutating request = %d, want 403; body %s", blocked.Code, blocked.Body.String())
			return
		}
		t.Logf("PASS: rejected with 403 (body %q)", blocked.Body.String())
	})
}

// TestReviewCSRFRejectsTamperedToken — CSRF cookie present but header does
// not mirror it -> 403 (invalid token).
func TestReviewCSRFRejectsTamperedToken(t *testing.T) {
	t.Run("Given a CSRF cookie plus a tampered header value, When requireCSRF runs, Then it is rejected 403", func(t *testing.T) {
		h, deps := testServerWithAuth(t)
		ctx := t.Context()
		if err := deps.identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
			t.Fatal(err)
		}
		rec := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
			"email": "admin@example.com", "password": "correct horse battery",
		})
		creds := sessionCookie(t, rec)
		if creds.csrf == nil {
			t.Fatalf("login set no CSRF cookie")
		}
		t.Logf("Given CSRF cookie value %q", creds.csrf.Value)
		t.Logf("When  the request mirrors a TAMPERED header value instead of the cookie's")

		req := httptest.NewRequest(http.MethodPost, "/api/v0/content/article", nil)
		req.AddCookie(creds.session)
		req.AddCookie(creds.csrf)
		req.Header.Set("X-CSRF-Token", "tampered-deadbeef0000000000000000000000000000000000000000000000000000")
		blocked := httptest.NewRecorder()
		h.ServeHTTP(blocked, req)
		if blocked.Code != http.StatusForbidden {
			t.Errorf("FAIL: tampered-token mutating request = %d, want 403; body %s", blocked.Code, blocked.Body.String())
			return
		}
		t.Logf("PASS: tampered token rejected with 403 (body %q)", blocked.Body.String())
	})
}

// TestReviewCSRFAcceptsValidToken — cookie + mirrored header -> accepted.
func TestReviewCSRFAcceptsValidToken(t *testing.T) {
	t.Run("Given a session cookie and the matching CSRF token mirrored in the header, When requireCSRF runs, Then the request is accepted", func(t *testing.T) {
		h, deps := testServerWithAuth(t)
		ctx := t.Context()
		if err := deps.identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
			t.Fatal(err)
		}
		rec := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
			"email": "admin@example.com", "password": "correct horse battery",
		})
		creds := sessionCookie(t, rec)
		if creds.csrf == nil {
			t.Fatalf("login set no CSRF cookie")
		}
		t.Logf("Given session + CSRF cookie; header mirrors the CSRF cookie value")
		t.Logf("When  POST /api/v0/content/article is sent with the mirrored token")

		allowed := doWithCookieBody(t, h, http.MethodPost, "/api/v0/content/article", creds, map[string]any{"title": "hi"})
		if allowed.Code != http.StatusCreated {
			t.Errorf("FAIL: cookie + valid CSRF header request = %d, want 201; body %s", allowed.Code, allowed.Body.String())
			return
		}
		t.Logf("PASS: accepted with 201")
	})
}
