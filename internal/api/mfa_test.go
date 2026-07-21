package api_test

import (
	"net/http"
	"testing"
)

// End-to-end MFA login flow over HTTP: enroll, confirm with a real TOTP
// code computed from the returned secret, then log in and complete the
// second-step challenge with a fresh code.
func TestMFAEnrollConfirmAndLoginChallengeOverHTTP(t *testing.T) {
	h, cookie := authedServer(t)

	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/auth/mfa/enroll", cookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll = %d, body %s", rec.Code, rec.Body.String())
	}
	enrolled := decode(t, rec)
	secret, _ := enrolled["secret"].(string)
	if secret == "" {
		t.Fatal("no secret returned")
	}

	// Wrong code does not confirm.
	rec = doWithCookieBody(t, h, http.MethodPost, "/api/v0/auth/mfa/confirm", cookie, map[string]any{"code": "000000"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("confirm with wrong code = %d, want 401", rec.Code)
	}

	code := totpCodeForTest(t, secret)
	rec = doWithCookieBody(t, h, http.MethodPost, "/api/v0/auth/mfa/confirm", cookie, map[string]any{"code": code})
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm = %d, body %s", rec.Code, rec.Body.String())
	}
	confirmed := decode(t, rec)
	recoveryCodes, _ := confirmed["recoveryCodes"].([]any)
	if len(recoveryCodes) == 0 {
		t.Fatal("expected recovery codes")
	}

	// Now log in again: password succeeds but a session is withheld pending
	// the MFA challenge.
	rec = do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "admin@example.com", "password": "correct horse battery",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d, body %s", rec.Code, rec.Body.String())
	}
	loginBody := decode(t, rec)
	if loginBody["mfaRequired"] != true {
		t.Fatalf("expected mfaRequired=true, got %v", loginBody)
	}
	mfaToken, _ := loginBody["mfaToken"].(string)
	if mfaToken == "" {
		t.Fatal("no mfaToken returned")
	}
	if _, hasSession := loginBody["token"]; hasSession {
		t.Error("a session token leaked before MFA verification")
	}

	// Wrong code does not resolve the challenge.
	rec = do(t, h, http.MethodPost, "/api/v0/auth/mfa/verify", map[string]any{"mfaToken": mfaToken, "code": "000000"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("verify with wrong code = %d, want 401", rec.Code)
	}

	// Right code issues a session.
	freshCode := totpCodeForTest(t, secret)
	rec = do(t, h, http.MethodPost, "/api/v0/auth/mfa/verify", map[string]any{"mfaToken": mfaToken, "code": freshCode})
	if rec.Code != http.StatusOK {
		t.Fatalf("verify = %d, body %s", rec.Code, rec.Body.String())
	}
	verified := decode(t, rec)
	if verified["token"] == nil || verified["token"] == "" {
		t.Fatal("expected a session token after MFA verification")
	}

	// Regression: MFA-verify must set a CSRF cookie exactly like a normal
	// login does, or this session could never pass requireCSRF on any
	// mutation route — an MFA-protected account would be silently locked
	// out of every write.
	var sawCSRFCookie bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == "glyphux_csrf" && c.Value != "" {
			sawCSRFCookie = true
		}
	}
	if !sawCSRFCookie {
		t.Error("MFA verify response set no CSRF cookie")
	}
}

func TestMFAEnrollRequiresAuthentication(t *testing.T) {
	h := testServer(t)
	rec := do(t, h, http.MethodPost, "/api/v0/auth/mfa/enroll", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon enroll = %d, want 401", rec.Code)
	}
}
