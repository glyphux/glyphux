package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCSRFBlocksCookieRidingCrossOriginRequest proves the classic CSRF
// attack is closed: a state-changing request that carries only the ambient
// session cookie (exactly what a forged cross-site form/fetch would ride
// along automatically) is rejected unless it also mirrors the CSRF cookie's
// value in the X-CSRF-Token header — something a cross-origin attacker
// cannot read (same-origin policy), so it cannot forge.
func TestCSRFBlocksCookieRidingCrossOriginRequest(t *testing.T) {
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
	creds := sessionCookie(t, rec)
	if creds.csrf == nil {
		t.Fatal("no CSRF cookie set at login")
	}

	// Cookie only, no CSRF header — this is the forged cross-site request.
	req := httptest.NewRequest(http.MethodPost, "/api/v0/content/article", nil)
	req.AddCookie(creds.session)
	blocked := httptest.NewRecorder()
	h.ServeHTTP(blocked, req)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("cookie-only mutating request = %d, want 403", blocked.Code)
	}

	// Cookie + mirrored CSRF header — the real admin SPA's own request.
	allowed := doWithCookieBody(t, h, http.MethodPost, "/api/v0/content/article", creds, map[string]any{"title": "hi"})
	if allowed.Code != http.StatusCreated {
		t.Fatalf("cookie + CSRF header request = %d, want 201, body %s", allowed.Code, allowed.Body.String())
	}
}

// TestCSRFSkippedForBearerAuth proves bearer-token-authenticated requests
// are exempt from the CSRF check entirely — a stolen bearer token requires
// the attacker to already be able to read the response, unlike a cookie
// which rides along on a forged request automatically, so bearer requests
// aren't the CSRF threat model (PRD §1.9 decision).
func TestCSRFSkippedForBearerAuth(t *testing.T) {
	h, deps := testServerWithAuth(t)
	ctx := context.Background()
	_ = deps.identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery")
	u, _ := deps.identities.Authenticate(ctx, "admin@example.com", "correct horse battery")
	sess, _ := deps.sessions.Create(ctx, u.ID)

	body, _ := json.Marshal(map[string]any{"title": "hi"})
	req := httptest.NewRequest(http.MethodPost, "/api/v0/content/article", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+sess.Token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bearer-authenticated mutating request = %d, want 201, body %s", rec.Code, rec.Body.String())
	}
}
