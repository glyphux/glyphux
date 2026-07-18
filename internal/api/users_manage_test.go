package api_test

import (
	"net/http"
	"strconv"
	"testing"
)

func TestUpdateUserRoleOverHTTPAdminOnly(t *testing.T) {
	h, deps := testServerWithAuth(t)
	ctx := t.Context()
	if err := deps.identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	editor, err := deps.identities.CreateUser(ctx, "editor@example.com", "correct horse battery", "editor")
	if err != nil {
		t.Fatal(err)
	}
	adminLogin := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "admin@example.com", "password": "correct horse battery",
	})
	adminCookie := sessionCookie(t, adminLogin)

	// Anonymous is rejected.
	rec := do(t, h, http.MethodPatch, "/api/v0/users/1/role", map[string]any{"role": "viewer"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon role change = %d, want 401", rec.Code)
	}

	editorLogin := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "editor@example.com", "password": "correct horse battery",
	})
	editorCookie := sessionCookie(t, editorLogin)
	rec = doWithCookieBody(t, h, http.MethodPatch, "/api/v0/users/1/role", editorCookie, map[string]any{"role": "viewer"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor role change = %d, want 403", rec.Code)
	}

	path := "/api/v0/users/" + strconv.FormatInt(editor.ID, 10) + "/role"
	rec = doWithCookieBody(t, h, http.MethodPatch, path, adminCookie, map[string]any{"role": "viewer"})
	if rec.Code != http.StatusOK {
		t.Fatalf("admin role change = %d, body %s", rec.Code, rec.Body.String())
	}
	updated := decode(t, rec)
	if updated["role"] != "viewer" {
		t.Errorf("role = %v, want viewer", updated["role"])
	}
	if updated["active"] != true {
		t.Errorf("active = %v, want true — regression check for userByID dropping the active column", updated["active"])
	}

	// Unknown role rejected.
	rec = doWithCookieBody(t, h, http.MethodPatch, path, adminCookie, map[string]any{"role": "superuser"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown role = %d, want 422", rec.Code)
	}
}

func TestDeactivateUserRevokesSessionsAndBlocksLogin(t *testing.T) {
	h, deps := testServerWithAuth(t)
	ctx := t.Context()
	if err := deps.identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	editor, err := deps.identities.CreateUser(ctx, "editor@example.com", "correct horse battery", "editor")
	if err != nil {
		t.Fatal(err)
	}
	adminLogin := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "admin@example.com", "password": "correct horse battery",
	})
	adminCookie := sessionCookie(t, adminLogin)

	editorLogin := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "editor@example.com", "password": "correct horse battery",
	})
	editorCookie := sessionCookie(t, editorLogin)

	// Editor's session works before deactivation.
	rec := doWithCookieBody(t, h, http.MethodGet, "/api/v0/auth/me", editorCookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("pre-deactivation /me = %d", rec.Code)
	}

	path := "/api/v0/users/" + strconv.FormatInt(editor.ID, 10) + "/deactivate"
	rec = doWithCookieBody(t, h, http.MethodPost, path, adminCookie, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("deactivate = %d, body %s", rec.Code, rec.Body.String())
	}

	// The editor's existing session no longer works.
	rec = doWithCookieBody(t, h, http.MethodGet, "/api/v0/auth/me", editorCookie, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("post-deactivation /me = %d, want 401", rec.Code)
	}

	// And a fresh login attempt is blocked too.
	rec = do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "editor@example.com", "password": "correct horse battery",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("login after deactivation = %d, want 403", rec.Code)
	}

	// Reactivate restores login.
	rec = doWithCookieBody(t, h, http.MethodPost, "/api/v0/users/"+strconv.FormatInt(editor.ID, 10)+"/reactivate", adminCookie, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reactivate = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "editor@example.com", "password": "correct horse battery",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login after reactivation = %d", rec.Code)
	}
}

