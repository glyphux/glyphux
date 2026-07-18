package composition_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/db"
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

func TestDefineContentTypeCreatesAndUpdates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.Save(ctx, &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
	}); err != nil {
		t.Fatal(err)
	}

	comp, err := s.DefineContentType(ctx, "article", contract.ContentType{
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
	comp, err = s.DefineContentType(ctx, "article", contract.ContentType{
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

func TestDefineContentTypeRejectsInvalidShape(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.Save(ctx, &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := s.DefineContentType(ctx, "article", contract.ContentType{
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
	if err := s.Save(ctx, &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
		ContentTypes: map[string]contract.ContentType{
			"article": {Fields: map[string]contract.Field{"title": {Type: contract.FieldString, Required: true}}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	comp, err := s.RemoveContentType(ctx, "article")
	if err != nil {
		t.Fatalf("RemoveContentType: %v", err)
	}
	if _, ok := comp.ContentTypes["article"]; ok {
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

func TestRemoveContentTypeUnknownReturnsErrContentTypeNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.Save(ctx, &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := s.RemoveContentType(ctx, "does-not-exist")
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
	if err := s.Save(ctx, &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
		ContentTypes: map[string]contract.ContentType{
			"article": {Fields: map[string]contract.Field{"title": {Type: contract.FieldString, Required: true}}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("an item appeared")
	_, err := s.RemoveContentTypeGuarded(ctx, "article", func(context.Context) error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("RemoveContentTypeGuarded = %v, want the guard's error", err)
	}

	// The type must still be declared — the guard vetoed before any write.
	comp, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := comp.ContentTypes["article"]; !ok {
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
	if err := s.Save(ctx, &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
	}); err != nil {
		t.Fatal(err)
	}

	const n = 20
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			_, err := s.DefineContentType(ctx, fmt.Sprintf("type%d", i), contract.ContentType{
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
