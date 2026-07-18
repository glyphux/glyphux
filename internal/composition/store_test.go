package composition_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/contract"
)

func newTestStore(t *testing.T) *composition.Store {
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

var admin = &permission.Principal{Role: permission.RoleAdmin}

func TestSaveAllowsNilPrincipalOnFirstWriteOnly(t *testing.T) {
	store := newTestStore(t)
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
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.Save(ctx, nil, testComposition()); err != nil {
		t.Fatal(err)
	}

	editor := &permission.Principal{Role: permission.RoleEditor}
	if err := store.Save(ctx, editor, testComposition()); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("editor Save: got %v, want permission.ErrDenied", err)
	}

	if err := store.Save(ctx, admin, testComposition()); err != nil {
		t.Errorf("admin Save rejected: %v", err)
	}
}

func TestLoadAndExistsRemainPrincipalFree(t *testing.T) {
	store := newTestStore(t)
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

func TestDefineContentTypeCreatesAndUpdates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.Save(ctx, nil, testComposition()); err != nil {
		t.Fatal(err)
	}

	comp, err := s.DefineContentType(ctx, admin, "article", contract.ContentType{
		Fields: map[string]contract.Field{
			"title": {Type: contract.FieldString, Required: true},
		},
	})
	if err != nil {
		t.Fatalf("DefineContentType (create): %v", err)
	}
	if _, ok := comp.ContentTypes["article"]; !ok {
		t.Fatal("DefineContentType (create): article not present in returned composition")
	}

	reloaded, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.ContentTypes["article"].Fields) != 1 {
		t.Fatalf("reloaded article fields = %v, want 1", reloaded.ContentTypes["article"].Fields)
	}

	// Update: replace the field set entirely.
	comp, err = s.DefineContentType(ctx, admin, "article", contract.ContentType{
		Fields: map[string]contract.Field{
			"title": {Type: contract.FieldString, Required: true},
			"body":  {Type: contract.FieldRichText},
		},
	})
	if err != nil {
		t.Fatalf("DefineContentType (update): %v", err)
	}
	if len(comp.ContentTypes["article"].Fields) != 2 {
		t.Fatalf("updated article fields = %v, want 2", comp.ContentTypes["article"].Fields)
	}
}

func TestDefineContentTypeRejectsUnderPrivilegedPrincipal(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.Save(ctx, nil, testComposition()); err != nil {
		t.Fatal(err)
	}

	editor := &permission.Principal{Role: permission.RoleEditor}
	_, err := s.DefineContentType(ctx, editor, "article", contract.ContentType{
		Fields: map[string]contract.Field{"title": {Type: contract.FieldString, Required: true}},
	})
	if !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("editor DefineContentType = %v, want permission.ErrDenied", err)
	}
	if _, err := s.DefineContentType(ctx, nil, "article", contract.ContentType{
		Fields: map[string]contract.Field{"title": {Type: contract.FieldString, Required: true}},
	}); !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("anonymous DefineContentType = %v, want permission.ErrDenied", err)
	}
}

func TestDefineContentTypeRejectsInvalidShape(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.Save(ctx, nil, testComposition()); err != nil {
		t.Fatal(err)
	}

	_, err := s.DefineContentType(ctx, admin, "article", contract.ContentType{
		Fields: map[string]contract.Field{
			"linked": {Type: contract.FieldRelation, To: "does-not-exist"},
		},
	})
	var verrs contract.ValidationErrors
	if !errors.As(err, &verrs) {
		t.Fatalf("DefineContentType with dangling relation error = %v (%T), want contract.ValidationErrors", err, err)
	}
}

func TestRemoveContentType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	comp := testComposition()
	comp.ContentTypes = map[string]contract.ContentType{
		"article": {Fields: map[string]contract.Field{"title": {Type: contract.FieldString, Required: true}}},
	}
	if err := s.Save(ctx, nil, comp); err != nil {
		t.Fatal(err)
	}

	updated, err := s.RemoveContentType(ctx, admin, "article")
	if err != nil {
		t.Fatalf("RemoveContentType: %v", err)
	}
	if _, ok := updated.ContentTypes["article"]; ok {
		t.Fatal("RemoveContentType: article still present in returned composition")
	}

	reloaded, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.ContentTypes["article"]; ok {
		t.Fatal("RemoveContentType: article still present after reload")
	}
}

func TestRemoveContentTypeRejectsUnderPrivilegedPrincipal(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	comp := testComposition()
	comp.ContentTypes = map[string]contract.ContentType{
		"article": {Fields: map[string]contract.Field{"title": {Type: contract.FieldString, Required: true}}},
	}
	if err := s.Save(ctx, nil, comp); err != nil {
		t.Fatal(err)
	}

	editor := &permission.Principal{Role: permission.RoleEditor}
	if _, err := s.RemoveContentType(ctx, editor, "article"); !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("editor RemoveContentType = %v, want permission.ErrDenied", err)
	}
}

func TestRemoveContentTypeUnknownReturnsErrContentTypeNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.Save(ctx, nil, testComposition()); err != nil {
		t.Fatal(err)
	}

	_, err := s.RemoveContentType(ctx, admin, "does-not-exist")
	if !errors.Is(err, composition.ErrContentTypeNotFound) {
		t.Fatalf("RemoveContentType unknown = %v, want ErrContentTypeNotFound", err)
	}
}

// TestRemoveContentTypeGuardedAbortsOnGuardError proves the guard passed to
// RemoveContentTypeGuarded is consulted immediately before the write and can
// veto the removal — the seam internal/api and internal/graphql use to
// re-check "no items of this type exist" as close to the actual delete as
// possible, narrowing (not eliminating, since it's still a separate table
// with no shared transaction) the window in which an item created after an
// earlier count check could otherwise survive under an orphaned type.
func TestRemoveContentTypeGuardedAbortsOnGuardError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	comp := testComposition()
	comp.ContentTypes = map[string]contract.ContentType{
		"article": {Fields: map[string]contract.Field{"title": {Type: contract.FieldString, Required: true}}},
	}
	if err := s.Save(ctx, nil, comp); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("an item appeared")
	_, err := s.RemoveContentTypeGuarded(ctx, admin, "article", func(context.Context) error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("RemoveContentTypeGuarded = %v, want the guard's error", err)
	}

	// The type must still be declared — the guard vetoed before any write.
	reloaded, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.ContentTypes["article"]; !ok {
		t.Fatal("RemoveContentTypeGuarded: article removed despite guard veto")
	}
}

// TestConcurrentDefineContentTypeDoesNotLoseUpdates proves N concurrent
// DefineContentType calls, each declaring a distinct type, all survive —
// a naive load-modify-save with no compare-and-swap would let a later
// writer's save silently clobber an earlier writer's still-uncommitted-to-
// its-view edit, since both read the same starting document.
func TestConcurrentDefineContentTypeDoesNotLoseUpdates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.Save(ctx, nil, testComposition()); err != nil {
		t.Fatal(err)
	}

	const n = 20
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			_, err := s.DefineContentType(ctx, admin, fmt.Sprintf("type%d", i), contract.ContentType{
				Fields: map[string]contract.Field{
					"title": {Type: contract.FieldString, Required: true},
				},
			})
			errCh <- err
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("concurrent DefineContentType: %v", err)
		}
	}

	comp, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(comp.ContentTypes) != n {
		t.Fatalf("content types after %d concurrent defines = %d, want %d (%v)", n, len(comp.ContentTypes), n, comp.ContentTypes)
	}
}
