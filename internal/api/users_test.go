package api_test

import (
	"net/http"
	"testing"
)

// Only an admin (users:manage) may provision accounts; the created account
// can then authenticate with its assigned role.
func TestCreateUserOverHTTP(t *testing.T) {
	h, cookie := authedServer(t)

	// Anonymous create is rejected.
	if rec := do(t, h, http.MethodPost, "/api/v0/users", map[string]any{
		"email": "editor@example.com", "password": "correct horse battery", "role": "editor",
	}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon create user = %d, want 401", rec.Code)
	}

	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/users", cookie, map[string]any{
		"email": "editor@example.com", "password": "correct horse battery", "role": "editor",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user = %d, body %s", rec.Code, rec.Body.String())
	}
	created := decode(t, rec)
	if created["role"] != "editor" {
		t.Errorf("role = %v, want editor", created["role"])
	}
	if _, hasPassword := created["password"]; hasPassword {
		t.Error("response leaked a password field")
	}

	// Unknown role is rejected.
	rec = doWithCookieBody(t, h, http.MethodPost, "/api/v0/users", cookie, map[string]any{
		"email": "x@example.com", "password": "correct horse battery", "role": "superuser",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown role = %d, want 422", rec.Code)
	}

	// List includes both the seed admin and the new editor.
	rec = do(t, h, http.MethodGet, "/api/v0/users", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon list users = %d, want 401", rec.Code)
	}
	rec = doWithCookieBody(t, h, http.MethodGet, "/api/v0/users", cookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list users = %d", rec.Code)
	}
	list := decode(t, rec)
	users, _ := list["users"].([]any)
	if len(users) != 2 {
		t.Fatalf("list returned %d users, want 2", len(users))
	}
}

// A non-admin (editor) authenticated caller cannot manage users even though
// it can write content — users:manage is admin-only.
func TestEditorCannotManageUsers(t *testing.T) {
	h, deps := testServerWithAuth(t)
	ctx := t.Context()
	if err := deps.identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.identities.CreateUser(ctx, "editor@example.com", "correct horse battery", "editor"); err != nil {
		t.Fatal(err)
	}
	loginRec := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "editor@example.com", "password": "correct horse battery",
	})
	cookie := sessionCookie(t, loginRec)

	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/users", cookie, map[string]any{
		"email": "another@example.com", "password": "correct horse battery", "role": "viewer",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor create user = %d, want 403", rec.Code)
	}
}
