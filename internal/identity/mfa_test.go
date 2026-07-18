package identity

import (
	"context"
	"errors"
	"testing"
)

func TestMFAEnrollmentIsOptInAndStartsDisabled(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	if err := svc.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	u, err := svc.Authenticate(ctx, "admin@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if u.MFAEnabled {
		t.Error("MFA should not be enabled by default")
	}
}

func TestBeginAndConfirmMFAEnrollment(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	if err := svc.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	u, _ := svc.Authenticate(ctx, "admin@example.com", "correct horse battery")

	secret, uri, err := svc.BeginMFAEnrollment(ctx, u.ID)
	if err != nil {
		t.Fatalf("BeginMFAEnrollment: %v", err)
	}
	if secret == "" || uri == "" {
		t.Fatal("empty secret or otpauth URI")
	}

	// Wrong code does not confirm enrollment.
	if _, err := svc.ConfirmMFAEnrollment(ctx, u.ID, "000000"); !errors.Is(err, ErrInvalidMFACode) {
		t.Errorf("wrong code: got %v, want ErrInvalidMFACode", err)
	}
	got, _ := svc.Authenticate(ctx, "admin@example.com", "correct horse battery")
	if got.MFAEnabled {
		t.Error("MFA should still be disabled after a failed confirmation")
	}

	code := totpCodeAt(secret, svc.now())
	codes, err := svc.ConfirmMFAEnrollment(ctx, u.ID, code)
	if err != nil {
		t.Fatalf("ConfirmMFAEnrollment: %v", err)
	}
	if len(codes) == 0 {
		t.Error("expected recovery codes to be generated")
	}

	got, _ = svc.Authenticate(ctx, "admin@example.com", "correct horse battery")
	if !got.MFAEnabled {
		t.Error("MFA should be enabled after confirmation")
	}
}

func TestVerifyMFAAcceptsCodeRejectsWrongAndMissing(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	_ = svc.CreateAdmin(ctx, "admin@example.com", "correct horse battery")
	u, _ := svc.Authenticate(ctx, "admin@example.com", "correct horse battery")
	secret, _, _ := svc.BeginMFAEnrollment(ctx, u.ID)
	code := totpCodeAt(secret, svc.now())
	if _, err := svc.ConfirmMFAEnrollment(ctx, u.ID, code); err != nil {
		t.Fatal(err)
	}

	if err := svc.VerifyMFA(ctx, u.ID, ""); !errors.Is(err, ErrInvalidMFACode) {
		t.Errorf("missing code: got %v, want ErrInvalidMFACode", err)
	}
	if err := svc.VerifyMFA(ctx, u.ID, "000000"); !errors.Is(err, ErrInvalidMFACode) {
		t.Errorf("wrong code: got %v, want ErrInvalidMFACode", err)
	}

	freshCode := totpCodeAt(secret, svc.now())
	if err := svc.VerifyMFA(ctx, u.ID, freshCode); err != nil {
		t.Errorf("VerifyMFA with valid code: %v", err)
	}
}

func TestVerifyMFAAcceptsRecoveryCodeOnceOnly(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	_ = svc.CreateAdmin(ctx, "admin@example.com", "correct horse battery")
	u, _ := svc.Authenticate(ctx, "admin@example.com", "correct horse battery")
	secret, _, _ := svc.BeginMFAEnrollment(ctx, u.ID)
	code := totpCodeAt(secret, svc.now())
	recovery, err := svc.ConfirmMFAEnrollment(ctx, u.ID, code)
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.VerifyMFA(ctx, u.ID, recovery[0]); err != nil {
		t.Fatalf("VerifyMFA with recovery code: %v", err)
	}
	// The same recovery code cannot be reused.
	if err := svc.VerifyMFA(ctx, u.ID, recovery[0]); !errors.Is(err, ErrInvalidMFACode) {
		t.Errorf("reused recovery code: got %v, want ErrInvalidMFACode", err)
	}
}

// End-to-end login-shape flow: password step succeeds, MFA challenge is
// issued instead of a session, and only the right TOTP code resolves it.
func TestLoginStepThenMFAChallengeEndToEnd(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	_ = svc.CreateAdmin(ctx, "admin@example.com", "correct horse battery")
	u, _ := svc.Authenticate(ctx, "admin@example.com", "correct horse battery")
	secret, _, _ := svc.BeginMFAEnrollment(ctx, u.ID)
	code := totpCodeAt(secret, svc.now())
	if _, err := svc.ConfirmMFAEnrollment(ctx, u.ID, code); err != nil {
		t.Fatal(err)
	}

	// Step 1: password login succeeds but the caller must still complete MFA.
	loggedIn, err := svc.Authenticate(ctx, "admin@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !loggedIn.MFAEnabled {
		t.Fatal("expected MFA to be enabled")
	}

	challenge, err := svc.BeginMFAChallenge(ctx, loggedIn.ID)
	if err != nil {
		t.Fatalf("BeginMFAChallenge: %v", err)
	}
	if challenge == "" {
		t.Fatal("empty challenge token")
	}

	// Wrong code does not resolve the challenge.
	if _, err := svc.ResolveMFAChallenge(ctx, challenge, "000000"); !errors.Is(err, ErrInvalidMFACode) {
		t.Errorf("wrong code: got %v, want ErrInvalidMFACode", err)
	}

	// Step 2: the right code resolves the challenge to the user.
	final, err := svc.ResolveMFAChallenge(ctx, challenge, totpCodeAt(secret, svc.now()))
	if err != nil {
		t.Fatalf("ResolveMFAChallenge: %v", err)
	}
	if !final.Active {
		t.Error("resolved user should be active — regression check for userByID dropping the active column")
	}
	if final.ID != u.ID {
		t.Errorf("resolved user id = %d, want %d", final.ID, u.ID)
	}

	// The challenge token is single-use.
	if _, err := svc.ResolveMFAChallenge(ctx, challenge, totpCodeAt(secret, svc.now())); !errors.Is(err, ErrInvalidMFAChallenge) {
		t.Errorf("reused challenge: got %v, want ErrInvalidMFAChallenge", err)
	}
}

func TestUnknownMFAChallengeTokenIsRejected(t *testing.T) {
	svc := testService(t)
	if _, err := svc.ResolveMFAChallenge(context.Background(), "not-a-real-token", "123456"); !errors.Is(err, ErrInvalidMFAChallenge) {
		t.Errorf("got %v, want ErrInvalidMFAChallenge", err)
	}
}

func TestDisableMFA(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	_ = svc.CreateAdmin(ctx, "admin@example.com", "correct horse battery")
	u, _ := svc.Authenticate(ctx, "admin@example.com", "correct horse battery")
	secret, _, _ := svc.BeginMFAEnrollment(ctx, u.ID)
	code := totpCodeAt(secret, svc.now())
	if _, err := svc.ConfirmMFAEnrollment(ctx, u.ID, code); err != nil {
		t.Fatal(err)
	}

	if err := svc.DisableMFA(ctx, u.ID); err != nil {
		t.Fatalf("DisableMFA: %v", err)
	}
	got, _ := svc.Authenticate(ctx, "admin@example.com", "correct horse battery")
	if got.MFAEnabled {
		t.Error("MFA should be disabled")
	}
}
