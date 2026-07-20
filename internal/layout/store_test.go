package layout_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/contract"
)

func testStore(t *testing.T) (*layout.Store, *blocks.Registry) {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(context.Background(), layout.Migrations); err != nil {
		t.Fatal(err)
	}
	reg := blocks.New()
	if err := reg.Register(blocks.Definition{Name: "heading", DisplayName: "Heading"}); err != nil {
		t.Fatal(err)
	}
	return layout.NewStore(d), reg
}

var admin = &permission.Principal{Role: permission.RoleAdmin}

func validLayout() *contract.Layout {
	return &contract.Layout{
		ContractVersion: contract.LayoutCompositionV1,
		Regions: map[string]contract.Region{
			"main": {Blocks: []contract.Block{{Type: "heading"}}},
		},
	}
}

func TestLoadReturnsErrNotFoundBeforeAnySave(t *testing.T) {
	s, _ := testStore(t)
	_, err := s.Load(context.Background(), "home")
	if !errors.Is(err, layout.ErrNotFound) {
		t.Fatalf("Load unsaved route error = %v, want ErrNotFound", err)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	s, reg := testStore(t)
	l := validLayout()
	if err := s.Save(context.Background(), admin, reg, "home", l); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load(context.Background(), "home")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Regions["main"].Blocks) != 1 || got.Regions["main"].Blocks[0].Type != "heading" {
		t.Fatalf("Load result = %+v, want the saved layout back", got)
	}
}

func TestSaveOverwritesExistingRoute(t *testing.T) {
	s, reg := testStore(t)
	ctx := context.Background()
	if err := s.Save(ctx, admin, reg, "home", validLayout()); err != nil {
		t.Fatal(err)
	}
	updated := validLayout()
	updated.Regions["main"] = contract.Region{Blocks: []contract.Block{{Type: "heading"}, {Type: "heading"}}}
	if err := s.Save(ctx, admin, reg, "home", updated); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load(ctx, "home")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Regions["main"].Blocks) != 2 {
		t.Fatalf("Load after overwrite = %d blocks, want 2", len(got.Regions["main"].Blocks))
	}
}

func TestSaveRejectsStructurallyInvalidLayout(t *testing.T) {
	s, reg := testStore(t)
	bad := &contract.Layout{ContractVersion: "wrong-version"}
	var verrs contract.ValidationErrors
	if err := s.Save(context.Background(), admin, reg, "home", bad); err == nil || !errors.As(err, &verrs) {
		t.Fatalf("Save with bad contract version err = %v, want ValidationErrors", err)
	}
}

func TestSaveRejectsUnregisteredBlockType(t *testing.T) {
	s, reg := testStore(t)
	l := &contract.Layout{
		ContractVersion: contract.LayoutCompositionV1,
		Regions: map[string]contract.Region{
			"main": {Blocks: []contract.Block{{Type: "does-not-exist"}}},
		},
	}
	var verrs contract.ValidationErrors
	if err := s.Save(context.Background(), admin, reg, "home", l); err == nil || !errors.As(err, &verrs) {
		t.Fatalf("Save with unregistered block type err = %v, want ValidationErrors", err)
	}
}

func TestSaveRejectsInvalidRouteFormat(t *testing.T) {
	s, reg := testStore(t)
	for _, route := range []string{"", "/leading-slash", "trailing-slash/", "Has-Upper", "double//slash", "bad char"} {
		if err := s.Save(context.Background(), admin, reg, route, validLayout()); err == nil {
			t.Errorf("Save(route=%q) = nil error, want rejection", route)
		}
	}
}

func TestSaveAcceptsMultiSegmentRoute(t *testing.T) {
	s, reg := testStore(t)
	if err := s.Save(context.Background(), admin, reg, "blog/index", validLayout()); err != nil {
		t.Fatalf("Save(blog/index): %v", err)
	}
	if _, err := s.Load(context.Background(), "blog/index"); err != nil {
		t.Fatalf("Load(blog/index): %v", err)
	}
}

func TestSaveRequiresLayoutsManageCapability(t *testing.T) {
	s, reg := testStore(t)
	editor := &permission.Principal{Role: permission.RoleEditor}
	if err := s.Save(context.Background(), editor, reg, "home", validLayout()); !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("Save as editor err = %v, want ErrDenied", err)
	}
	if err := s.Save(context.Background(), nil, reg, "home", validLayout()); !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("Save as anonymous err = %v, want ErrDenied", err)
	}
}
