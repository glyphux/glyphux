package client_test

import (
	"context"
	"testing"
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
