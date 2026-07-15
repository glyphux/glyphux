package client_test

import (
	"context"
	"testing"

	"github.com/glyphux/glyphux/pkg/client"
)

func TestLoginAuthenticatesSubsequentRequests(t *testing.T) {
	ts, identities, _ := newTestServer(t)
	ctx := context.Background()
	if err := identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}

	c := client.New(ts.URL)
	user, err := c.Auth.Login(ctx, "admin@example.com", "correct horse battery")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if user.Email != "admin@example.com" {
		t.Errorf("Login user.Email = %q, want admin@example.com", user.Email)
	}

	me, err := c.Auth.Me(ctx)
	if err != nil {
		t.Fatalf("Me after Login: %v", err)
	}
	if me.Email != "admin@example.com" || me.Role != "admin" {
		t.Errorf("Me = %+v, want admin@example.com/admin", me)
	}
}

func TestLoginWithWrongPasswordFails(t *testing.T) {
	ts, identities, _ := newTestServer(t)
	ctx := context.Background()
	if err := identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}

	c := client.New(ts.URL)
	_, err := c.Auth.Login(ctx, "admin@example.com", "wrong password")
	if err == nil {
		t.Fatal("Login with wrong password: want error, got nil")
	}
	var apiErr *client.APIError
	if !client.AsAPIError(err, &apiErr) {
		t.Fatalf("Login error = %v (%T), want *client.APIError", err, err)
	}
	if apiErr.StatusCode != 401 {
		t.Errorf("Login error status = %d, want 401", apiErr.StatusCode)
	}
}
