package graphql_test

import (
	"context"
	"strconv"
	"testing"
)

// TestUpdateUserRoleMutationAdminOnly proves updateUserRole changes the
// target account's role and is rejected for anyone without users:manage,
// mirroring TestUpdateUserRoleOverHTTPAdminOnly in internal/api.
func TestUpdateUserRoleMutationAdminOnly(t *testing.T) {
	h, deps := testServer(t)
	adminToken := loginAdmin(t, deps)
	editorToken := loginRole(t, deps, "editor-role-target@example.com", "editor")

	u, err := deps.identities.Authenticate(context.Background(), "editor-role-target@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(u.ID, 10)

	const mutation = `mutation($id: ID!, $role: String!) { updateUserRole(id: $id, role: $role) { role } }`
	variables := map[string]any{"id": id, "role": "viewer"}

	_, resp := doGraphQL(t, h, editorToken, mutation, variables)
	if len(resp.Errors) != 1 || resp.Errors[0].Extensions["code"] != "FORBIDDEN" {
		t.Fatalf("editor updateUserRole errors = %v, want 1 FORBIDDEN", resp.Errors)
	}

	_, resp = doGraphQL(t, h, adminToken, mutation, variables)
	if len(resp.Errors) != 0 {
		t.Fatalf("admin updateUserRole errors = %v", resp.Errors)
	}
	updated, ok := resp.Data["updateUserRole"].(map[string]any)
	if !ok || updated["role"] != "viewer" {
		t.Fatalf("updateUserRole = %v, want role viewer", resp.Data["updateUserRole"])
	}

	// Unknown role is a VALIDATION error.
	_, resp = doGraphQL(t, h, adminToken, mutation, map[string]any{"id": id, "role": "superuser"})
	if len(resp.Errors) != 1 || resp.Errors[0].Extensions["code"] != "VALIDATION" {
		t.Fatalf("unknown role errors = %v, want 1 VALIDATION", resp.Errors)
	}
}

// TestDeactivateAndReactivateUserMutations proves deactivateUser blocks the
// account's future logins and revokes its live sessions, and
// reactivateUser restores it — mirroring
// TestDeactivateUserRevokesSessionsAndBlocksLogin in internal/api.
func TestDeactivateAndReactivateUserMutations(t *testing.T) {
	h, deps := testServer(t)
	adminToken := loginAdmin(t, deps)
	editorToken := loginRole(t, deps, "editor-deactivate-target@example.com", "editor")

	u, err := deps.identities.Authenticate(context.Background(), "editor-deactivate-target@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(u.ID, 10)

	const deactivateMutation = `mutation($id: ID!) { deactivateUser(id: $id) }`
	variables := map[string]any{"id": id}

	// Non-admin cannot deactivate.
	_, resp := doGraphQL(t, h, editorToken, deactivateMutation, variables)
	if len(resp.Errors) != 1 || resp.Errors[0].Extensions["code"] != "FORBIDDEN" {
		t.Fatalf("editor deactivateUser errors = %v, want 1 FORBIDDEN", resp.Errors)
	}

	_, resp = doGraphQL(t, h, adminToken, deactivateMutation, variables)
	if len(resp.Errors) != 0 {
		t.Fatalf("admin deactivateUser errors = %v", resp.Errors)
	}
	if resp.Data["deactivateUser"] != true {
		t.Fatalf("deactivateUser = %v, want true", resp.Data["deactivateUser"])
	}

	// The editor's previously-issued session token is now dead.
	if _, err := deps.sessions.Lookup(context.Background(), editorToken); err == nil {
		t.Error("expected the deactivated account's session to be revoked")
	}

	// A fresh login attempt is blocked.
	if _, err := deps.identities.Authenticate(context.Background(), "editor-deactivate-target@example.com", "correct horse battery"); err == nil {
		t.Error("expected Authenticate to reject a deactivated account")
	}

	// Reactivate restores login.
	const reactivateMutation = `mutation($id: ID!) { reactivateUser(id: $id) }`
	_, resp = doGraphQL(t, h, adminToken, reactivateMutation, variables)
	if len(resp.Errors) != 0 {
		t.Fatalf("admin reactivateUser errors = %v", resp.Errors)
	}
	if resp.Data["reactivateUser"] != true {
		t.Fatalf("reactivateUser = %v, want true", resp.Data["reactivateUser"])
	}
	if _, err := deps.identities.Authenticate(context.Background(), "editor-deactivate-target@example.com", "correct horse battery"); err != nil {
		t.Errorf("expected Authenticate to succeed after reactivation: %v", err)
	}
}
