package composition_test

import (
	"context"
	"errors"
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
