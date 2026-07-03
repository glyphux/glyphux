package identity

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/db"
)

func testService(t *testing.T) *Service {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(context.Background(), Migrations); err != nil {
		t.Fatal(err)
	}
	return NewService(d)
}

func TestCreateAndVerifyAdmin(t *testing.T) {
	s := testService(t)
	ctx := context.Background()

	if err := s.CreateAdmin(ctx, "Admin@Example.com", "correct horse battery"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}

	// Verification is case-insensitive on email.
	if err := s.Verify(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Errorf("Verify with correct password: %v", err)
	}
	if err := s.Verify(ctx, "admin@example.com", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("Verify with wrong password: got %v, want ErrInvalidCredentials", err)
	}
	if err := s.Verify(ctx, "ghost@example.com", "whatever"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("Verify unknown user: got %v, want ErrInvalidCredentials", err)
	}

	n, err := s.UserCount(ctx)
	if err != nil || n != 1 {
		t.Errorf("UserCount = %d, %v; want 1, nil", n, err)
	}
}

func TestCreateAdminRejectsWeakInput(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	if err := s.CreateAdmin(ctx, "not-an-email", "long enough password"); err == nil {
		t.Error("accepted invalid email")
	}
	if err := s.CreateAdmin(ctx, "a@b.co", "short"); err == nil {
		t.Error("accepted short password")
	}
}
