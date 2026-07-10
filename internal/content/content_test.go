package content

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/pkg/contract"
)

// testAPI wires a content API over a fresh SQLite DB whose composition declares
// the given content types.
func testAPI(t *testing.T, types map[string]contract.ContentType) *API {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, composition.Migrations...), Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}
	comps := composition.NewStore(d)
	comp := &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
		ContentTypes:    types,
	}
	if err := comps.Save(context.Background(), comp); err != nil {
		t.Fatal(err)
	}
	return NewAPI(comps, NewStore(d))
}

func articleTypes() map[string]contract.ContentType {
	return map[string]contract.ContentType{
		"article": {Fields: map[string]contract.Field{
			"title": {Type: contract.FieldString, Required: true},
			"body":  {Type: contract.FieldRichText},
		}},
	}
}

func TestCreateRejectsUndeclaredType(t *testing.T) {
	api := testAPI(t, articleTypes())
	_, err := api.Create(context.Background(), "widget", map[string]any{"title": "x"})
	if !errors.Is(err, ErrUnknownType) {
		t.Errorf("got %v, want ErrUnknownType", err)
	}
}

func TestCreateRejectsUnknownField(t *testing.T) {
	api := testAPI(t, articleTypes())
	_, err := api.Create(context.Background(), "article", map[string]any{"title": "x", "bogus": 1})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("got %v, want ErrValidation", err)
	}
}

func TestCreateRejectsMissingRequiredField(t *testing.T) {
	api := testAPI(t, articleTypes())
	_, err := api.Create(context.Background(), "article", map[string]any{"body": "no title"})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("got %v, want ErrValidation", err)
	}
}

func TestCreateRejectsWrongValueType(t *testing.T) {
	api := testAPI(t, map[string]contract.ContentType{
		"product": {Fields: map[string]contract.Field{
			"name":  {Type: contract.FieldString, Required: true},
			"price": {Type: contract.FieldNumber},
			"sale":  {Type: contract.FieldBoolean},
		}},
	})
	_, err := api.Create(context.Background(), "product", map[string]any{"name": "Shoe", "price": "free"})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("string price: got %v, want ErrValidation", err)
	}
	if _, err := api.Create(context.Background(), "product", map[string]any{"name": "Shoe", "sale": "yes"}); !errors.Is(err, ErrValidation) {
		t.Errorf("string bool: got %v, want ErrValidation", err)
	}
	// Correct kinds pass.
	if _, err := api.Create(context.Background(), "product", map[string]any{"name": "Shoe", "price": 9.99, "sale": true}); err != nil {
		t.Errorf("valid product rejected: %v", err)
	}
}

func TestGetMissingReturnsNotFound(t *testing.T) {
	api := testAPI(t, articleTypes())
	if _, err := api.Get(context.Background(), "article", "deadbeef"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestListReturnsOnlyItemsOfType(t *testing.T) {
	api := testAPI(t, map[string]contract.ContentType{
		"article": {Fields: map[string]contract.Field{"title": {Type: contract.FieldString, Required: true}}},
		"note":    {Fields: map[string]contract.Field{"title": {Type: contract.FieldString, Required: true}}},
	})
	ctx := context.Background()
	mustCreate(t, api, "article", map[string]any{"title": "A1"})
	mustCreate(t, api, "article", map[string]any{"title": "A2"})
	mustCreate(t, api, "note", map[string]any{"title": "N1"})

	items, err := api.List(ctx, "article")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("List returned %d items, want 2", len(items))
	}
	for _, it := range items {
		if it.Type != "article" {
			t.Errorf("List returned foreign type %q", it.Type)
		}
	}
}

func TestListUndeclaredTypeErrors(t *testing.T) {
	api := testAPI(t, articleTypes())
	if _, err := api.List(context.Background(), "widget"); !errors.Is(err, ErrUnknownType) {
		t.Errorf("got %v, want ErrUnknownType", err)
	}
}

func TestUpdateMutatesAndValidates(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()
	created := mustCreate(t, api, "article", map[string]any{"title": "Before"})

	updated, err := api.Update(ctx, "article", created.ID, map[string]any{"title": "After", "body": "added"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Data["title"] != "After" || updated.Data["body"] != "added" {
		t.Errorf("Update data = %v", updated.Data)
	}
	got, _ := api.Get(ctx, "article", created.ID)
	if got.Data["title"] != "After" {
		t.Errorf("persisted title = %v, want After", got.Data["title"])
	}

	// Update is validated too: a missing required field is rejected.
	if _, err := api.Update(ctx, "article", created.ID, map[string]any{"body": "no title"}); !errors.Is(err, ErrValidation) {
		t.Errorf("invalid update: got %v, want ErrValidation", err)
	}
	// Updating a missing item is a not-found.
	if _, err := api.Update(ctx, "article", "nope", map[string]any{"title": "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update missing: got %v, want ErrNotFound", err)
	}
}

func TestDeleteRemoves(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()
	created := mustCreate(t, api, "article", map[string]any{"title": "Doomed"})

	if err := api.Delete(ctx, "article", created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := api.Get(ctx, "article", created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete, Get got %v, want ErrNotFound", err)
	}
	// Deleting a missing item is a not-found.
	if err := api.Delete(ctx, "article", created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete missing: got %v, want ErrNotFound", err)
	}
}

func relationTypes() map[string]contract.ContentType {
	return map[string]contract.ContentType{
		"author": {Fields: map[string]contract.Field{
			"name": {Type: contract.FieldString, Required: true},
		}},
		"article": {Fields: map[string]contract.Field{
			"title":  {Type: contract.FieldString, Required: true},
			"author": {Type: contract.FieldRelation, To: "author"},
		}},
	}
}

func TestRelationMustReferenceExistingTarget(t *testing.T) {
	api := testAPI(t, relationTypes())
	ctx := context.Background()
	author := mustCreate(t, api, "author", map[string]any{"name": "Ada"})

	// Valid reference to an existing author.
	if _, err := api.Create(ctx, "article", map[string]any{"title": "T", "author": author.ID}); err != nil {
		t.Errorf("valid relation rejected: %v", err)
	}
	// Dangling reference is a validation failure.
	if _, err := api.Create(ctx, "article", map[string]any{"title": "T", "author": "does-not-exist"}); !errors.Is(err, ErrValidation) {
		t.Errorf("dangling relation: got %v, want ErrValidation", err)
	}
	// Update is enforced too.
	art := mustCreate(t, api, "article", map[string]any{"title": "T"})
	if _, err := api.Update(ctx, "article", art.ID, map[string]any{"title": "T", "author": "nope"}); !errors.Is(err, ErrValidation) {
		t.Errorf("dangling relation on update: got %v, want ErrValidation", err)
	}
}

func mustCreate(t *testing.T, api *API, typeName string, data map[string]any) *Item {
	t.Helper()
	it, err := api.Create(context.Background(), typeName, data)
	if err != nil {
		t.Fatalf("Create(%s): %v", typeName, err)
	}
	return it
}

func TestCreateStartsAsDraftAtVersionOne(t *testing.T) {
	api := testAPI(t, articleTypes())
	created := mustCreate(t, api, "article", map[string]any{"title": "Hello"})
	if created.Status != "draft" {
		t.Errorf("Status = %q, want draft", created.Status)
	}
	if created.Version != 1 {
		t.Errorf("Version = %d, want 1", created.Version)
	}
}

func TestPublishAndUnpublish(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()
	created := mustCreate(t, api, "article", map[string]any{"title": "Hello"})

	published, err := api.Publish(ctx, "article", created.ID)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if published.Status != "published" {
		t.Errorf("Status after Publish = %q, want published", published.Status)
	}
	got, _ := api.Get(ctx, "article", created.ID)
	if got.Status != "published" {
		t.Errorf("persisted status = %q, want published", got.Status)
	}

	unpublished, err := api.Unpublish(ctx, "article", created.ID)
	if err != nil {
		t.Fatalf("Unpublish: %v", err)
	}
	if unpublished.Status != "draft" {
		t.Errorf("Status after Unpublish = %q, want draft", unpublished.Status)
	}

	if _, err := api.Publish(ctx, "article", "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("publish missing item: got %v, want ErrNotFound", err)
	}
}

func TestUpdateBumpsVersionAndRecordsHistory(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()
	created := mustCreate(t, api, "article", map[string]any{"title": "V1"})

	updated, err := api.Update(ctx, "article", created.ID, map[string]any{"title": "V2"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Version != 2 {
		t.Errorf("Version after Update = %d, want 2", updated.Version)
	}

	versions, err := api.ListVersions(ctx, "article", created.ID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("ListVersions returned %d entries, want 2", len(versions))
	}
	if versions[0].Version != 1 || versions[0].Data["title"] != "V1" {
		t.Errorf("versions[0] = %+v, want version 1 with title V1", versions[0])
	}
	if versions[1].Version != 2 || versions[1].Data["title"] != "V2" {
		t.Errorf("versions[1] = %+v, want version 2 with title V2", versions[1])
	}

	if _, err := api.ListVersions(ctx, "article", "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ListVersions missing item: got %v, want ErrNotFound", err)
	}
}

func TestRollbackRestoresOlderVersion(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()
	created := mustCreate(t, api, "article", map[string]any{"title": "V1"})
	_, err := api.Update(ctx, "article", created.ID, map[string]any{"title": "V2"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	rolled, err := api.Rollback(ctx, "article", created.ID, 1)
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if rolled.Data["title"] != "V1" {
		t.Errorf("Rollback data = %v, want title V1", rolled.Data)
	}
	if rolled.Version != 3 {
		t.Errorf("Rollback Version = %d, want 3 (rollback is itself a new version)", rolled.Version)
	}

	if _, err := api.Rollback(ctx, "article", created.ID, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("rollback to missing version: got %v, want ErrNotFound", err)
	}
}

func TestCreateAndGet(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()

	created, err := api.Create(ctx, "article", map[string]any{"title": "Hello", "body": "World"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create returned empty ID")
	}
	if created.Type != "article" {
		t.Errorf("Type = %q, want article", created.Type)
	}

	got, err := api.Get(ctx, "article", created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Data["title"] != "Hello" {
		t.Errorf("title = %v, want Hello", got.Data["title"])
	}
	if got.Data["body"] != "World" {
		t.Errorf("body = %v, want World", got.Data["body"])
	}
}
