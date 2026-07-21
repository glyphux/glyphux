package client_test

import (
	"context"
	"testing"

	"github.com/glyphux/glyphux/pkg/client"
)

func TestUsersCreateAndList(t *testing.T) {
	c := loggedInClient(t)
	ctx := context.Background()

	created, err := c.Users.Create(ctx, "editor@example.com", "correct horse battery", "editor")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Email != "editor@example.com" || created.Role != "editor" {
		t.Errorf("Create = %+v, want editor@example.com/editor", created)
	}

	users, err := c.Users.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// The seeded admin plus the newly created editor.
	if len(users) != 2 {
		t.Fatalf("List = %+v, want 2 users", users)
	}
}

func TestUsersUpdateRoleAndDeactivateReactivate(t *testing.T) {
	ts, identities, _ := newTestServer(t)
	ctx := context.Background()
	if err := identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	c := client.New(ts.URL)
	if _, err := c.Auth.Login(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatalf("Login: %v", err)
	}

	created, err := c.Users.Create(ctx, "editor2@example.com", "correct horse battery", "editor")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := c.Users.UpdateRole(ctx, created.ID, "viewer")
	if err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	if updated.Role != "viewer" {
		t.Errorf("Role = %q, want viewer", updated.Role)
	}

	if _, err := c.Users.UpdateRole(ctx, created.ID, "superuser"); err == nil {
		t.Error("expected an error for an unknown role")
	}

	if err := c.Users.Deactivate(ctx, created.ID); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	// A deactivated account can no longer log in.
	fresh := client.New(ts.URL)
	if _, err := fresh.Auth.Login(ctx, "editor2@example.com", "correct horse battery"); err == nil {
		t.Error("expected login to fail for a deactivated account")
	}

	if err := c.Users.Reactivate(ctx, created.ID); err != nil {
		t.Fatalf("Reactivate: %v", err)
	}
	if _, err := fresh.Auth.Login(ctx, "editor2@example.com", "correct horse battery"); err != nil {
		t.Errorf("expected login to succeed after reactivation: %v", err)
	}
}
