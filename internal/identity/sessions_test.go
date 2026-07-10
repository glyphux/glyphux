package identity

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/glyphux/glyphux/internal/db"
)

func testEnv(t *testing.T) (*Service, *Sessions) {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(context.Background(), Migrations); err != nil {
		t.Fatal(err)
	}
	return NewService(d), NewSessions(d)
}

func TestAuthenticateReturnsUser(t *testing.T) {
	svc, _ := testEnv(t)
	ctx := context.Background()
	if err := svc.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	u, err := svc.Authenticate(ctx, "admin@example.com", "correct horse battery")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if u.Email != "admin@example.com" || u.Role != "admin" || u.ID == 0 {
		t.Errorf("unexpected user %+v", u)
	}
	if _, err := svc.Authenticate(ctx, "admin@example.com", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password: got %v, want ErrInvalidCredentials", err)
	}
}

func TestSessionCreateLookupRevoke(t *testing.T) {
	svc, sessions := testEnv(t)
	ctx := context.Background()
	if err := svc.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	u, _ := svc.Authenticate(ctx, "admin@example.com", "correct horse battery")

	sess, err := sessions.Create(ctx, u.ID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sess.Token == "" {
		t.Fatal("empty session token")
	}

	got, err := sessions.Lookup(ctx, sess.Token)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.ID != u.ID || got.Role != "admin" {
		t.Errorf("Lookup returned %+v, want id %d admin", got, u.ID)
	}

	// Unknown token is rejected.
	if _, err := sessions.Lookup(ctx, "not-a-real-token"); !errors.Is(err, ErrInvalidSession) {
		t.Errorf("unknown token: got %v, want ErrInvalidSession", err)
	}

	// Revoked token is rejected.
	if err := sessions.Revoke(ctx, sess.Token); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := sessions.Lookup(ctx, sess.Token); !errors.Is(err, ErrInvalidSession) {
		t.Errorf("revoked token: got %v, want ErrInvalidSession", err)
	}
}

func TestSessionExpires(t *testing.T) {
	svc, sessions := testEnv(t)
	ctx := context.Background()
	_ = svc.CreateAdmin(ctx, "admin@example.com", "correct horse battery")
	u, _ := svc.Authenticate(ctx, "admin@example.com", "correct horse battery")

	// Freeze the clock in the past so the session is already expired at lookup.
	base := time.Now().Add(-100 * 24 * time.Hour)
	sessions.now = func() time.Time { return base }
	sess, _ := sessions.Create(ctx, u.ID)

	sessions.now = time.Now // clock advances past TTL
	if _, err := sessions.Lookup(ctx, sess.Token); !errors.Is(err, ErrInvalidSession) {
		t.Errorf("expired token: got %v, want ErrInvalidSession", err)
	}
}

// The raw token must never be persisted — only a hash of it.
func TestSessionTokenIsHashedAtRest(t *testing.T) {
	svc, sessions := testEnv(t)
	ctx := context.Background()
	_ = svc.CreateAdmin(ctx, "admin@example.com", "correct horse battery")
	u, _ := svc.Authenticate(ctx, "admin@example.com", "correct horse battery")
	sess, _ := sessions.Create(ctx, u.ID)

	var n int
	if err := sessions.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM sessions WHERE token_hash = ?`, sess.Token).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("raw token found stored verbatim; expected only its hash")
	}
}
