package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/glyphux/glyphux/internal/permission"
)

var adminPrincipal = &permission.Principal{Role: permission.RoleAdmin}
var editorPrincipal = &permission.Principal{Role: permission.RoleEditor}

func TestUpdateRoleChangesRoleAdminOnly(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	_ = svc.CreateAdmin(ctx, "admin@example.com", "correct horse battery")
	u, err := svc.CreateUser(ctx, "editor@example.com", "correct horse battery", "editor")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.UpdateRole(ctx, editorPrincipal, u.ID, "viewer"); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("editor UpdateRole: got %v, want ErrDenied", err)
	}

	got, err := svc.UpdateRole(ctx, adminPrincipal, u.ID, "viewer")
	if err != nil {
		t.Fatalf("admin UpdateRole: %v", err)
	}
	if got.Role != "viewer" {
		t.Errorf("role = %q, want viewer", got.Role)
	}
	if !got.Active {
		t.Error("UpdateRole should not report an active account as inactive — regression check for userByID")
	}

	if _, err := svc.UpdateRole(ctx, adminPrincipal, u.ID, "superuser"); err == nil {
		t.Error("accepted unknown role")
	}
}

func TestDeactivateBlocksFutureAuthAdminOnly(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	_ = svc.CreateAdmin(ctx, "admin@example.com", "correct horse battery")
	u, err := svc.CreateUser(ctx, "editor@example.com", "correct horse battery", "editor")
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Deactivate(ctx, editorPrincipal, u.ID); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("editor Deactivate: got %v, want ErrDenied", err)
	}

	if err := svc.Deactivate(ctx, adminPrincipal, u.ID); err != nil {
		t.Fatalf("admin Deactivate: %v", err)
	}

	if _, err := svc.Authenticate(ctx, "editor@example.com", "correct horse battery"); !errors.Is(err, ErrAccountDeactivated) {
		t.Errorf("Authenticate after deactivation: got %v, want ErrAccountDeactivated", err)
	}

	// Reactivate restores login.
	if err := svc.Reactivate(ctx, adminPrincipal, u.ID); err != nil {
		t.Fatalf("Reactivate: %v", err)
	}
	if _, err := svc.Authenticate(ctx, "editor@example.com", "correct horse battery"); err != nil {
		t.Errorf("Authenticate after reactivation: %v", err)
	}
}

func TestNewAccountsAreActiveByDefault(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	u, err := svc.CreateUser(ctx, "editor@example.com", "correct horse battery", "editor")
	if err != nil {
		t.Fatal(err)
	}
	if !u.Active {
		t.Error("new account should be active by default")
	}
}
