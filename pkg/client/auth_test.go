package client_test

import (
	"context"
	"errors"
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

// TestMFAEnrollConfirmAndLoginChallenge drives a full TOTP MFA round trip
// through the SDK: enroll, confirm with a real code, log in (withheld
// pending MFA), then complete it with VerifyMFA.
func TestMFAEnrollConfirmAndLoginChallenge(t *testing.T) {
	ts, identities, _ := newTestServer(t)
	ctx := context.Background()
	if err := identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}

	c := client.New(ts.URL)
	if _, err := c.Auth.Login(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatalf("Login: %v", err)
	}

	secret, otpauthURL, err := c.Auth.BeginMFAEnrollment(ctx)
	if err != nil {
		t.Fatalf("BeginMFAEnrollment: %v", err)
	}
	if secret == "" || otpauthURL == "" {
		t.Fatal("empty secret or otpauth URL")
	}

	code := totpCodeForTest(t, secret)
	recoveryCodes, err := c.Auth.ConfirmMFAEnrollment(ctx, code)
	if err != nil {
		t.Fatalf("ConfirmMFAEnrollment: %v", err)
	}
	if len(recoveryCodes) == 0 {
		t.Fatal("expected recovery codes")
	}

	// A fresh (unauthenticated) client now hits the MFA challenge on login.
	fresh := client.New(ts.URL)
	_, err = fresh.Auth.Login(ctx, "admin@example.com", "correct horse battery")
	var challenge *client.MFARequiredError
	if !errors.As(err, &challenge) {
		t.Fatalf("Login error = %v (%T), want *client.MFARequiredError", err, err)
	}
	if challenge.Token == "" {
		t.Fatal("empty MFA challenge token")
	}

	user, err := fresh.Auth.VerifyMFA(ctx, challenge.Token, totpCodeForTest(t, secret))
	if err != nil {
		t.Fatalf("VerifyMFA: %v", err)
	}
	if user.Email != "admin@example.com" {
		t.Errorf("VerifyMFA user = %+v, want admin@example.com", user)
	}

	// The now-authenticated fresh client can make requests.
	if _, err := fresh.Auth.Me(ctx); err != nil {
		t.Errorf("Me after VerifyMFA: %v", err)
	}
}
