package identity

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/glyphux/glyphux/internal/audit"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/permission"
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

// ---- Ticket T7 (gap 4): item-level CRUD auditing via the WithAudit option ----

func testAuditService(t *testing.T) (*Service, *db.DB, *audit.Logger) {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, Migrations...), audit.Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}
	logger := audit.NewLogger(d)
	return NewService(d, WithAudit(logger)), d, logger
}

func listIdentityAuditRows(t *testing.T, logger *audit.Logger) []audit.Record {
	t.Helper()
	rows, err := logger.ListByPlugin(context.Background(), "identity")
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func parseAuditDetail(t *testing.T, r audit.Record) map[string]string {
	t.Helper()
	var out map[string]string
	if err := json.Unmarshal([]byte(r.Detail), &out); err != nil {
		t.Fatalf("parse detail %q: %v", r.Detail, err)
	}
	return out
}

// TestAuditRecordsUserWritesAndSkipsReads: GIVEN an identity service wired
// with a live audit logger, WHEN CreateUser/UpdateRole/Deactivate/Reactivate
// run, THEN one row per write lands stamped plugin "identity" with the
// pinned action and a Detail carrying the actor role and the subject user
// id; reads (ListUsers/UserCount) write nothing.
func TestAuditRecordsUserWritesAndSkipsReads(t *testing.T) {
	ctx := context.Background()
	s, d, logger := testAuditService(t)
	admin := &permission.Principal{Role: permission.RoleAdmin}

	u, err := s.CreateUser(ctx, "u@example.com", "correct horse battery", "editor")
	if err != nil {
		t.Fatal(err)
	}
	rows := listIdentityAuditRows(t, logger)
	if len(rows) != 1 || rows[0].Action != audit.ActionUserCreated {
		t.Fatalf("after CreateUser: rows = %+v, want one %s", rows, audit.ActionUserCreated)
	}
	createDetail := parseAuditDetail(t, rows[0])
	if createDetail["item_id"] != strconv.FormatInt(u.ID, 10) || createDetail["type"] != "user" {
		t.Errorf("create detail = %v, want item_id=%d type=user", createDetail, u.ID)
	}

	// Reads write nothing.
	if _, err := s.ListUsers(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserCount(ctx); err != nil {
		t.Fatal(err)
	}
	if len(listIdentityAuditRows(t, logger)) != 1 {
		t.Error("reads must not write audit rows")
	}

	if _, err := s.UpdateRole(ctx, admin, u.ID, "viewer"); err != nil {
		t.Fatal(err)
	}
	if err := s.Deactivate(ctx, admin, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Reactivate(ctx, admin, u.ID); err != nil {
		t.Fatal(err)
	}

	want := []string{
		audit.ActionUserCreated,
		audit.ActionUserRoleChanged,
		audit.ActionUserDeactivated,
		audit.ActionUserReactivated,
	}
	rows = listIdentityAuditRows(t, logger)
	if len(rows) != len(want) {
		t.Fatalf("rows = %d, want %d: %+v", len(rows), len(want), rows)
	}
	for i, w := range want {
		if rows[i].Action != w {
			t.Errorf("row %d action = %q, want %q", i, rows[i].Action, w)
		}
	}
	_ = d
}

// TestAuditNilIdentityLoggerIsANoOp: GIVEN WithAudit(nil), WHEN any write
// runs, THEN no audit rows appear and behavior is unchanged (no panic).
func TestAuditNilIdentityLoggerIsANoOp(t *testing.T) {
	ctx := context.Background()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, Migrations...), audit.Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}
	s := NewService(d, WithAudit(nil))

	if _, err := s.CreateUser(ctx, "u@example.com", "correct horse battery", "editor"); err != nil {
		t.Fatal(err)
	}
	if rows := listIdentityAuditRows(t, audit.NewLogger(d)); len(rows) != 0 {
		t.Errorf("WithAudit(nil) must write no rows, got %d", len(rows))
	}
}
