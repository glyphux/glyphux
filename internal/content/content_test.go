package content

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glyphux/glyphux/internal/audit"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/contract"
)

// Principals used throughout: adminPrincipal for ordinary fixture setup
// (Create/Update/etc. need *some* privileged caller), editorPrincipal and
// viewerPrincipal to exercise under-privileged rejection, and a nil
// *permission.Principal to exercise the anonymous/unauthenticated case.
var (
	adminPrincipal  = &permission.Principal{Role: permission.RoleAdmin}
	editorPrincipal = &permission.Principal{Role: permission.RoleEditor}
	viewerPrincipal = &permission.Principal{Role: permission.RoleViewer}
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
	if err := comps.Save(context.Background(), nil, comp); err != nil {
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
	_, err := api.Create(context.Background(), adminPrincipal, "widget", map[string]any{"title": "x"})
	if !errors.Is(err, ErrUnknownType) {
		t.Errorf("got %v, want ErrUnknownType", err)
	}
}

func TestCreateRejectsUnknownField(t *testing.T) {
	api := testAPI(t, articleTypes())
	_, err := api.Create(context.Background(), adminPrincipal, "article", map[string]any{"title": "x", "bogus": 1})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("got %v, want ErrValidation", err)
	}
}

func TestCreateRejectsMissingRequiredField(t *testing.T) {
	api := testAPI(t, articleTypes())
	_, err := api.Create(context.Background(), adminPrincipal, "article", map[string]any{"body": "no title"})
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
	_, err := api.Create(context.Background(), adminPrincipal, "product", map[string]any{"name": "Shoe", "price": "free"})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("string price: got %v, want ErrValidation", err)
	}
	if _, err := api.Create(context.Background(), adminPrincipal, "product", map[string]any{"name": "Shoe", "sale": "yes"}); !errors.Is(err, ErrValidation) {
		t.Errorf("string bool: got %v, want ErrValidation", err)
	}
	// Correct kinds pass.
	if _, err := api.Create(context.Background(), adminPrincipal, "product", map[string]any{"name": "Shoe", "price": 9.99, "sale": true}); err != nil {
		t.Errorf("valid product rejected: %v", err)
	}
}

func TestGetMissingReturnsNotFound(t *testing.T) {
	api := testAPI(t, articleTypes())
	if _, err := api.Get(context.Background(), adminPrincipal, "article", "deadbeef"); !errors.Is(err, ErrNotFound) {
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

	items, err := api.List(ctx, adminPrincipal, "article")
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
	if _, err := api.List(context.Background(), adminPrincipal, "widget"); !errors.Is(err, ErrUnknownType) {
		t.Errorf("got %v, want ErrUnknownType", err)
	}
}

func TestUpdateMutatesAndValidates(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()
	created := mustCreate(t, api, "article", map[string]any{"title": "Before"})

	updated, err := api.Update(ctx, adminPrincipal, "article", created.ID, map[string]any{"title": "After", "body": "added"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Data["title"] != "After" || updated.Data["body"] != "added" {
		t.Errorf("Update data = %v", updated.Data)
	}
	got, _ := api.Get(ctx, adminPrincipal, "article", created.ID)
	if got.Data["title"] != "After" {
		t.Errorf("persisted title = %v, want After", got.Data["title"])
	}

	// Update is validated too: a missing required field is rejected.
	if _, err := api.Update(ctx, adminPrincipal, "article", created.ID, map[string]any{"body": "no title"}); !errors.Is(err, ErrValidation) {
		t.Errorf("invalid update: got %v, want ErrValidation", err)
	}
	// Updating a missing item is a not-found.
	if _, err := api.Update(ctx, adminPrincipal, "article", "nope", map[string]any{"title": "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update missing: got %v, want ErrNotFound", err)
	}
}

func TestDeleteRemoves(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()
	created := mustCreate(t, api, "article", map[string]any{"title": "Doomed"})

	if err := api.Delete(ctx, adminPrincipal, "article", created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := api.Get(ctx, adminPrincipal, "article", created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete, Get got %v, want ErrNotFound", err)
	}
	// Deleting a missing item is a not-found.
	if err := api.Delete(ctx, adminPrincipal, "article", created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete missing: got %v, want ErrNotFound", err)
	}
}

func TestCountItemsReflectsCreatesAndDeletes(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()

	n, err := api.CountItems(ctx, "article")
	if err != nil {
		t.Fatalf("CountItems (empty): %v", err)
	}
	if n != 0 {
		t.Fatalf("CountItems (empty) = %d, want 0", n)
	}

	created := mustCreate(t, api, "article", map[string]any{"title": "One"})
	mustCreate(t, api, "article", map[string]any{"title": "Two"})

	n, err = api.CountItems(ctx, "article")
	if err != nil {
		t.Fatalf("CountItems: %v", err)
	}
	if n != 2 {
		t.Fatalf("CountItems = %d, want 2", n)
	}

	if err := api.Delete(ctx, adminPrincipal, "article", created.ID); err != nil {
		t.Fatal(err)
	}
	n, err = api.CountItems(ctx, "article")
	if err != nil {
		t.Fatalf("CountItems after delete: %v", err)
	}
	if n != 1 {
		t.Fatalf("CountItems after delete = %d, want 1", n)
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
	if _, err := api.Create(ctx, adminPrincipal, "article", map[string]any{"title": "T", "author": author.ID}); err != nil {
		t.Errorf("valid relation rejected: %v", err)
	}
	// Dangling reference is a validation failure.
	if _, err := api.Create(ctx, adminPrincipal, "article", map[string]any{"title": "T", "author": "does-not-exist"}); !errors.Is(err, ErrValidation) {
		t.Errorf("dangling relation: got %v, want ErrValidation", err)
	}
	// Update is enforced too.
	art := mustCreate(t, api, "article", map[string]any{"title": "T"})
	if _, err := api.Update(ctx, adminPrincipal, "article", art.ID, map[string]any{"title": "T", "author": "nope"}); !errors.Is(err, ErrValidation) {
		t.Errorf("dangling relation on update: got %v, want ErrValidation", err)
	}
}

func mustCreate(t *testing.T, api *API, typeName string, data map[string]any) *Item {
	t.Helper()
	it, err := api.Create(context.Background(), adminPrincipal, typeName, data)
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

	published, err := api.Publish(ctx, adminPrincipal, "article", created.ID)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if published.Status != "published" {
		t.Errorf("Status after Publish = %q, want published", published.Status)
	}
	got, _ := api.Get(ctx, adminPrincipal, "article", created.ID)
	if got.Status != "published" {
		t.Errorf("persisted status = %q, want published", got.Status)
	}

	unpublished, err := api.Unpublish(ctx, adminPrincipal, "article", created.ID)
	if err != nil {
		t.Fatalf("Unpublish: %v", err)
	}
	if unpublished.Status != "draft" {
		t.Errorf("Status after Unpublish = %q, want draft", unpublished.Status)
	}

	if _, err := api.Publish(ctx, adminPrincipal, "article", "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("publish missing item: got %v, want ErrNotFound", err)
	}
}

func TestUpdateBumpsVersionAndRecordsHistory(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()
	created := mustCreate(t, api, "article", map[string]any{"title": "V1"})

	updated, err := api.Update(ctx, adminPrincipal, "article", created.ID, map[string]any{"title": "V2"})
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
	_, err := api.Update(ctx, adminPrincipal, "article", created.ID, map[string]any{"title": "V2"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	rolled, err := api.Rollback(ctx, adminPrincipal, "article", created.ID, 1)
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if rolled.Data["title"] != "V1" {
		t.Errorf("Rollback data = %v, want title V1", rolled.Data)
	}
	if rolled.Version != 3 {
		t.Errorf("Rollback Version = %d, want 3 (rollback is itself a new version)", rolled.Version)
	}

	if _, err := api.Rollback(ctx, adminPrincipal, "article", created.ID, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("rollback to missing version: got %v, want ErrNotFound", err)
	}
}

func localizedArticleTypes() map[string]contract.ContentType {
	return map[string]contract.ContentType{
		"article": {Fields: map[string]contract.Field{
			"title": {Type: contract.FieldString, Required: true, Localized: true},
			"body":  {Type: contract.FieldRichText},
		}},
	}
}

func TestCreateAcceptsLocalizedFieldAsLocaleMap(t *testing.T) {
	api := testAPI(t, localizedArticleTypes())
	created, err := api.Create(context.Background(), adminPrincipal, "article", map[string]any{
		"title": map[string]any{"en": "Hello", "fr": "Bonjour"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	title, _ := created.Data["title"].(map[string]any)
	if title["en"] != "Hello" || title["fr"] != "Bonjour" {
		t.Errorf("title = %v", title)
	}
}

func TestCreateRejectsLocalizedFieldNotAMap(t *testing.T) {
	api := testAPI(t, localizedArticleTypes())
	_, err := api.Create(context.Background(), adminPrincipal, "article", map[string]any{"title": "not a locale map"})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("got %v, want ErrValidation", err)
	}
}

func TestCreateRejectsWrongKindWithinLocaleMap(t *testing.T) {
	api := testAPI(t, localizedArticleTypes())
	_, err := api.Create(context.Background(), adminPrincipal, "article", map[string]any{
		"title": map[string]any{"en": 42},
	})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("got %v, want ErrValidation", err)
	}
}

func TestCreateRejectsEmptyLocaleMapForRequiredField(t *testing.T) {
	api := testAPI(t, localizedArticleTypes())
	_, err := api.Create(context.Background(), adminPrincipal, "article", map[string]any{
		"title": map[string]any{},
	})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("got %v, want ErrValidation", err)
	}
}

func TestGetLocalizedResolvesRequestedLocaleWithFallback(t *testing.T) {
	api := testAPI(t, localizedArticleTypes())
	ctx := context.Background()
	created := mustCreate(t, api, "article", map[string]any{
		"title": map[string]any{"en": "Hello", "fr": "Bonjour"},
		"body":  "shared body",
	})

	fr, err := api.GetLocalized(ctx, adminPrincipal, "article", created.ID, "fr")
	if err != nil {
		t.Fatalf("GetLocalized(fr): %v", err)
	}
	if fr.Data["title"] != "Bonjour" {
		t.Errorf("fr title = %v, want Bonjour", fr.Data["title"])
	}
	if fr.Data["body"] != "shared body" {
		t.Errorf("fr body = %v, want shared body (non-localized field passes through)", fr.Data["body"])
	}

	// Missing locale falls back to any available value rather than erroring.
	de, err := api.GetLocalized(ctx, adminPrincipal, "article", created.ID, "de")
	if err != nil {
		t.Fatalf("GetLocalized(de): %v", err)
	}
	if de.Data["title"] != "Hello" && de.Data["title"] != "Bonjour" {
		t.Errorf("de title = %v, want a fallback value", de.Data["title"])
	}
}

func TestListLocalizedResolvesEachItem(t *testing.T) {
	api := testAPI(t, localizedArticleTypes())
	ctx := context.Background()
	mustCreate(t, api, "article", map[string]any{"title": map[string]any{"en": "A"}})
	mustCreate(t, api, "article", map[string]any{"title": map[string]any{"en": "B"}})

	items, err := api.ListLocalized(ctx, adminPrincipal, "article", "en")
	if err != nil {
		t.Fatalf("ListLocalized: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("ListLocalized returned %d items, want 2", len(items))
	}
	for _, it := range items {
		if it.Data["title"] != "A" && it.Data["title"] != "B" {
			t.Errorf("item title = %v, want resolved scalar", it.Data["title"])
		}
	}
}

func TestGetPublishedHidesDrafts(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()
	created := mustCreate(t, api, "article", map[string]any{"title": "Draft"})

	if _, err := api.GetPublished(ctx, "article", created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetPublished on draft: got %v, want ErrNotFound", err)
	}

	if _, err := api.Publish(ctx, adminPrincipal, "article", created.ID); err != nil {
		t.Fatal(err)
	}
	got, err := api.GetPublished(ctx, "article", created.ID)
	if err != nil {
		t.Fatalf("GetPublished after publish: %v", err)
	}
	if got.Data["title"] != "Draft" {
		t.Errorf("title = %v, want Draft", got.Data["title"])
	}

	// Admin Get still sees it regardless of status.
	if _, err := api.Get(ctx, adminPrincipal, "article", created.ID); err != nil {
		t.Errorf("Get (admin view): %v", err)
	}
}

func TestListPublishedExcludesDrafts(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()
	published := mustCreate(t, api, "article", map[string]any{"title": "Published"})
	mustCreate(t, api, "article", map[string]any{"title": "Draft"})
	if _, err := api.Publish(ctx, adminPrincipal, "article", published.ID); err != nil {
		t.Fatal(err)
	}

	items, err := api.ListPublished(ctx, "article")
	if err != nil {
		t.Fatalf("ListPublished: %v", err)
	}
	if len(items) != 1 || items[0].ID != published.ID {
		t.Fatalf("ListPublished = %+v, want only the published item", items)
	}

	// Admin List still sees both.
	all, err := api.List(ctx, adminPrincipal, "article")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("List (admin view) = %d items, want 2", len(all))
	}
}

func TestGetLocalizedPublishedHidesDrafts(t *testing.T) {
	api := testAPI(t, localizedArticleTypes())
	ctx := context.Background()
	created := mustCreate(t, api, "article", map[string]any{"title": map[string]any{"en": "Hi"}})

	if _, err := api.GetLocalizedPublished(ctx, "article", created.ID, "en"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetLocalizedPublished on draft: got %v, want ErrNotFound", err)
	}
	if _, err := api.Publish(ctx, adminPrincipal, "article", created.ID); err != nil {
		t.Fatal(err)
	}
	got, err := api.GetLocalizedPublished(ctx, "article", created.ID, "en")
	if err != nil {
		t.Fatalf("GetLocalizedPublished after publish: %v", err)
	}
	if got.Data["title"] != "Hi" {
		t.Errorf("title = %v, want Hi", got.Data["title"])
	}
}

func TestListLocalizedPublishedExcludesDrafts(t *testing.T) {
	api := testAPI(t, localizedArticleTypes())
	ctx := context.Background()
	published := mustCreate(t, api, "article", map[string]any{"title": map[string]any{"en": "Pub"}})
	mustCreate(t, api, "article", map[string]any{"title": map[string]any{"en": "Draft"}})
	if _, err := api.Publish(ctx, adminPrincipal, "article", published.ID); err != nil {
		t.Fatal(err)
	}

	items, err := api.ListLocalizedPublished(ctx, "article", "en")
	if err != nil {
		t.Fatalf("ListLocalizedPublished: %v", err)
	}
	if len(items) != 1 || items[0].Data["title"] != "Pub" {
		t.Fatalf("ListLocalizedPublished = %+v, want only the published item", items)
	}
}

func TestCreateAndGet(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()

	created, err := api.Create(ctx, adminPrincipal, "article", map[string]any{"title": "Hello", "body": "World"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create returned empty ID")
	}
	if created.Type != "article" {
		t.Errorf("Type = %q, want article", created.Type)
	}

	got, err := api.Get(ctx, adminPrincipal, "article", created.ID)
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

// TestCreateSanitizesRichTextField proves stored-XSS via a rich-text field
// is closed at the write boundary (slice 1.9): a malicious <script> payload
// is neutralized before it is ever persisted, so reading the item back never
// serves executable script to a theme/client that renders the field as
// HTML. Safe formatting markup survives untouched.
func TestCreateSanitizesRichTextField(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()

	created, err := api.Create(ctx, adminPrincipal, "article", map[string]any{
		"title": "Hello",
		"body":  `<p>safe</p><script>alert('xss')</script><img src=x onerror=alert(1)>`,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	body, _ := created.Data["body"].(string)
	if strings.Contains(body, "<script") {
		t.Errorf("body still contains <script> after Create: %q", body)
	}
	if strings.Contains(body, "onerror") {
		t.Errorf("body still contains an inline event handler after Create: %q", body)
	}
	if !strings.Contains(body, "<p>safe</p>") {
		t.Errorf("body lost safe markup: %q", body)
	}

	// Read-back (Get) serves the same neutralized value, not the raw input.
	got, err := api.Get(ctx, adminPrincipal, "article", created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	gotBody, _ := got.Data["body"].(string)
	if strings.Contains(gotBody, "<script") || strings.Contains(gotBody, "onerror") {
		t.Errorf("read-back body still carries the malicious payload: %q", gotBody)
	}
}

// TestUpdateSanitizesRichTextField proves Update (not just Create) sanitizes
// rich-text fields at the write boundary too.
func TestUpdateSanitizesRichTextField(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()

	created, err := api.Create(ctx, adminPrincipal, "article", map[string]any{"title": "Hello", "body": "clean"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	updated, err := api.Update(ctx, adminPrincipal, "article", created.ID, map[string]any{
		"title": "Hello",
		"body":  `<script>alert('xss')</script>updated`,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	body, _ := updated.Data["body"].(string)
	if strings.Contains(body, "<script") {
		t.Errorf("body still contains <script> after Update: %q", body)
	}
}

// --- Domain-API boundary capability enforcement (PRD §10.5) ---
//
// These tests call the content domain API directly — bypassing
// internal/api's HTTP transport and its requireCapability/canReadDrafts
// checks entirely — to prove the domain API rejects an under-privileged or
// anonymous caller on its own, independent of whether any transport layer
// already checked.

func TestCreateRejectsUnderPrivilegedAndAnonymousPrincipal(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()

	if _, err := api.Create(ctx, viewerPrincipal, "article", map[string]any{"title": "x"}); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("viewer Create: got %v, want permission.ErrDenied", err)
	}
	if _, err := api.Create(ctx, nil, "article", map[string]any{"title": "x"}); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("anonymous Create: got %v, want permission.ErrDenied", err)
	}
	// Editor holds content:write and should succeed.
	if _, err := api.Create(ctx, editorPrincipal, "article", map[string]any{"title": "x"}); err != nil {
		t.Errorf("editor Create rejected: %v", err)
	}
}

func TestUpdateRejectsUnderPrivilegedAndAnonymousPrincipal(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()
	created := mustCreate(t, api, "article", map[string]any{"title": "Before"})

	if _, err := api.Update(ctx, viewerPrincipal, "article", created.ID, map[string]any{"title": "After"}); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("viewer Update: got %v, want permission.ErrDenied", err)
	}
	if _, err := api.Update(ctx, nil, "article", created.ID, map[string]any{"title": "After"}); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("anonymous Update: got %v, want permission.ErrDenied", err)
	}
}

func TestDeleteRejectsUnderPrivilegedAndAnonymousPrincipal(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()
	created := mustCreate(t, api, "article", map[string]any{"title": "Doomed"})

	if err := api.Delete(ctx, viewerPrincipal, "article", created.ID); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("viewer Delete: got %v, want permission.ErrDenied", err)
	}
	if err := api.Delete(ctx, nil, "article", created.ID); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("anonymous Delete: got %v, want permission.ErrDenied", err)
	}
	// Item must still exist: neither rejected Delete call took effect.
	if _, err := api.Get(ctx, adminPrincipal, "article", created.ID); err != nil {
		t.Errorf("item should still exist after denied deletes: %v", err)
	}
}

func TestPublishAndUnpublishRequireContentPublishNotJustContentWrite(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()
	created := mustCreate(t, api, "article", map[string]any{"title": "Hello"})

	// Editor holds content:write but not content:publish.
	if _, err := api.Publish(ctx, editorPrincipal, "article", created.ID); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("editor Publish: got %v, want permission.ErrDenied", err)
	}
	if _, err := api.Publish(ctx, nil, "article", created.ID); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("anonymous Publish: got %v, want permission.ErrDenied", err)
	}
	published, err := api.Publish(ctx, adminPrincipal, "article", created.ID)
	if err != nil {
		t.Fatalf("admin Publish: %v", err)
	}
	if _, err := api.Unpublish(ctx, editorPrincipal, "article", published.ID); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("editor Unpublish: got %v, want permission.ErrDenied", err)
	}
}

func TestRollbackRejectsUnderPrivilegedAndAnonymousPrincipal(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()
	created := mustCreate(t, api, "article", map[string]any{"title": "V1"})
	if _, err := api.Update(ctx, adminPrincipal, "article", created.ID, map[string]any{"title": "V2"}); err != nil {
		t.Fatal(err)
	}

	if _, err := api.Rollback(ctx, viewerPrincipal, "article", created.ID, 1); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("viewer Rollback: got %v, want permission.ErrDenied", err)
	}
	if _, err := api.Rollback(ctx, nil, "article", created.ID, 1); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("anonymous Rollback: got %v, want permission.ErrDenied", err)
	}
}

func TestGetAndListRejectAnonymousAndViewerLacksReadDrafts(t *testing.T) {
	api := testAPI(t, articleTypes())
	ctx := context.Background()
	created := mustCreate(t, api, "article", map[string]any{"title": "Draft only"})

	// Viewer holds content:read but not content:read_drafts, so the
	// all-status admin view (Get/List) is denied even though the public,
	// capability-free GetPublished path exists separately.
	if _, err := api.Get(ctx, viewerPrincipal, "article", created.ID); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("viewer Get: got %v, want permission.ErrDenied", err)
	}
	if _, err := api.Get(ctx, nil, "article", created.ID); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("anonymous Get: got %v, want permission.ErrDenied", err)
	}
	if _, err := api.List(ctx, viewerPrincipal, "article"); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("viewer List: got %v, want permission.ErrDenied", err)
	}
	if _, err := api.List(ctx, nil, "article"); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("anonymous List: got %v, want permission.ErrDenied", err)
	}
	// Editor and admin both hold content:read_drafts.
	if _, err := api.Get(ctx, editorPrincipal, "article", created.ID); err != nil {
		t.Errorf("editor Get rejected: %v", err)
	}

	// The public, capability-free path is unaffected: anyone (including
	// anonymous) can still read published content — but this item is still
	// a draft, so it is correctly invisible there too, just for a different
	// reason (not published, not permission).
	if _, err := api.GetPublished(ctx, "article", created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetPublished on draft: got %v, want ErrNotFound", err)
	}
}

// ---- Ticket T7 (gap 4): item-level CRUD auditing via the WithAudit option ----

// testAuditAPI wires a content API over a fresh SQLite DB (audit migration
// included) with a live audit logger attached, returning the db and logger
// too so tests can query the rows the API should have written.
func testAuditAPI(t *testing.T, types map[string]contract.ContentType) (*API, *db.DB, *audit.Logger) {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, composition.Migrations...), Migrations...)
	migs = append(migs, audit.Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}
	comps := composition.NewStore(d)
	comp := &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
		ContentTypes:    types,
	}
	if err := comps.Save(context.Background(), nil, comp); err != nil {
		t.Fatal(err)
	}
	logger := audit.NewLogger(d)
	return NewAPI(comps, NewStore(d), WithAudit(logger)), d, logger
}

// testAuditAPINil wires the same API with WithAudit(nil) — the no-op
// contract (nil-safe option, byte-identical behavior).
func testAuditAPINil(t *testing.T, types map[string]contract.ContentType) (*API, *db.DB) {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, composition.Migrations...), Migrations...)
	migs = append(migs, audit.Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}
	comps := composition.NewStore(d)
	comp := &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
		ContentTypes:    types,
	}
	if err := comps.Save(context.Background(), nil, comp); err != nil {
		t.Fatal(err)
	}
	return NewAPI(comps, NewStore(d), WithAudit(nil)), d
}

func listAuditRows(t *testing.T, d *db.DB, plugin string) []audit.Record {
	t.Helper()
	rows, err := audit.NewLogger(d).ListByPlugin(context.Background(), plugin)
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

// TestAuditRecordsContentWritesAndSkipsReads: GIVEN a content API wired
// with a live audit logger, WHEN each write path runs (Create/Update/
// Publish/Unpublish/Rollback/Delete), THEN one audit_records row per write
// lands stamped plugin "content" with the pinned action and a Detail
// carrying role+item_id+type (actor_id "" — the domain boundary sees only
// role-only principals); reads (Get/List/GetPublished) write nothing.
func TestAuditRecordsContentWritesAndSkipsReads(t *testing.T) {
	ctx := context.Background()
	api, d, _ := testAuditAPI(t, articleTypes())

	created, err := api.Create(ctx, adminPrincipal, "article", map[string]any{"title": "Hi", "body": "B"})
	if err != nil {
		t.Fatal(err)
	}
	rows := listAuditRows(t, d, "content")
	if len(rows) != 1 || rows[0].Action != audit.ActionContentCreated {
		t.Fatalf("after Create: rows = %+v, want one %s", rows, audit.ActionContentCreated)
	}
	createDetail := parseAuditDetail(t, rows[0])
	if createDetail["role"] != "admin" || createDetail["item_id"] != created.ID || createDetail["type"] != "article" {
		t.Errorf("create detail = %v, want role=admin item_id=%s type=article", createDetail, created.ID)
	}

	// Reads write nothing.
	if _, err := api.Get(ctx, adminPrincipal, "article", created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := api.List(ctx, adminPrincipal, "article"); err != nil {
		t.Fatal(err)
	}
	if _, err := api.GetPublished(ctx, "article", created.ID); err != nil && !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if len(listAuditRows(t, d, "content")) != 1 {
		t.Error("reads must not write audit rows")
	}

	// Remaining writes, in order.
	if _, err := api.Update(ctx, adminPrincipal, "article", created.ID, map[string]any{"title": "Hi 2", "body": "B"}); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Publish(ctx, adminPrincipal, "article", created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Unpublish(ctx, adminPrincipal, "article", created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Rollback(ctx, adminPrincipal, "article", created.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := api.Delete(ctx, adminPrincipal, "article", created.ID); err != nil {
		t.Fatal(err)
	}

	want := []string{
		audit.ActionContentCreated,
		audit.ActionContentUpdated,
		audit.ActionContentPublished,
		audit.ActionContentUnpublished,
		audit.ActionContentRolledBack,
		audit.ActionContentDeleted,
	}
	rows = listAuditRows(t, d, "content")
	if len(rows) != len(want) {
		t.Fatalf("rows = %d, want %d: %+v", len(rows), len(want), rows)
	}
	for i, w := range want {
		if rows[i].Action != w {
			t.Errorf("row %d action = %q, want %q", i, rows[i].Action, w)
		}
	}
}

// TestAuditNilLoggerIsANoOp: GIVEN WithAudit(nil), WHEN any write runs,
// THEN no audit rows appear and behavior is unchanged (no panic).
func TestAuditNilLoggerIsANoOp(t *testing.T) {
	ctx := context.Background()
	api, d := testAuditAPINil(t, articleTypes())

	if _, err := api.Create(ctx, adminPrincipal, "article", map[string]any{"title": "Hi", "body": "B"}); err != nil {
		t.Fatal(err)
	}
	if err := api.Delete(ctx, adminPrincipal, "article", "nope"); err == nil {
		t.Error("delete of missing item must still fail with nil logger")
	}
	if rows := listAuditRows(t, d, "content"); len(rows) != 0 {
		t.Errorf("WithAudit(nil) must write no rows, got %d", len(rows))
	}
}
