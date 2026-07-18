package composition_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/contract"
)

func testStore(t *testing.T) *composition.Store {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(context.Background(), composition.Migrations); err != nil {
		t.Fatal(err)
	}
	return composition.NewStore(d)
}

func testComposition() *contract.Composition {
	return &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
	}
}

func TestSaveAllowsNilPrincipalOnFirstWriteOnly(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	// The first write (bootstrap/first-run setup) needs no principal — none
	// can exist yet at that point in the flow. This mirrors internal/setup's
	// pre-auth wizard, whose own token/localhost check is the real security
	// boundary for this specific write.
	if err := store.Save(ctx, nil, testComposition()); err != nil {
		t.Fatalf("first Save (nil principal): %v", err)
	}

	// Once a composition exists, Save is a content-type-management mutation
	// and requires content-types:manage — checked here at the domain-API
	// boundary (PRD §10.5), independent of any transport-layer check.
	if err := store.Save(ctx, nil, testComposition()); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("second Save (nil principal): got %v, want permission.ErrDenied", err)
	}
}

func TestSaveRejectsUnderPrivilegedPrincipalOnSubsequentWrites(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	if err := store.Save(ctx, nil, testComposition()); err != nil {
		t.Fatal(err)
	}

	editor := &permission.Principal{Role: permission.RoleEditor}
	if err := store.Save(ctx, editor, testComposition()); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("editor Save: got %v, want permission.ErrDenied", err)
	}

	admin := &permission.Principal{Role: permission.RoleAdmin}
	if err := store.Save(ctx, admin, testComposition()); err != nil {
		t.Errorf("admin Save rejected: %v", err)
	}
}

func TestLoadAndExistsRemainPrincipalFree(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	exists, err := store.Exists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("Exists = true before any Save")
	}
	if _, err := store.Load(ctx); !errors.Is(err, composition.ErrNotFound) {
		t.Errorf("Load before Save: got %v, want ErrNotFound", err)
	}

	if err := store.Save(ctx, nil, testComposition()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(ctx); err != nil {
		t.Errorf("Load after Save: %v", err)
	}
}
