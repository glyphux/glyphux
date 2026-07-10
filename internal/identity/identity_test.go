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

func TestCreateUserWithRole(t *testing.T) {
	s := testService(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "editor@example.com", "correct horse battery", "editor")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.Role != "editor" {
		t.Errorf("Role = %q, want editor", u.Role)
	}

	got, err := s.Authenticate(ctx, "editor@example.com", "correct horse battery")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.Role != "editor" {
		t.Errorf("authenticated role = %q, want editor", got.Role)
	}
}

func TestCreateUserRejectsUnknownRole(t *testing.T) {
	s := testService(t)
	if _, err := s.CreateUser(context.Background(), "x@example.com", "correct horse battery", "superuser"); err == nil {
		t.Error("accepted unknown role")
	}
}

func TestListUsersReturnsEveryAccount(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	if err := s.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, "editor@example.com", "correct horse battery", "editor"); err != nil {
		t.Fatal(err)
	}

	users, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("ListUsers returned %d, want 2", len(users))
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
