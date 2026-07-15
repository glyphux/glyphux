package graphql_test

// HTTP-level tests hitting the real GraphQL endpoint over httptest, wired to
// a real api-equivalent test server: real sqlite-backed domain stores, no
// mocked resolvers, no mocked domain layer — the seam confirmed in
// docs/implementation/active/0004-slice-1-12-graphql-transport.md, mirroring
// testServerWithAuth in internal/api/auth_test.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	glyphqlgraphql "github.com/glyphux/glyphux/internal/graphql"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/pkg/contract"
)

// testDeps exposes the domain services a graphql test server was built
// from, so tests can seed data or mint sessions directly.
type testDeps struct {
	identities *identity.Service
	sessions   *identity.Sessions
	content    *content.API
}

// testServer boots a real sqlite-backed server with the GraphQL endpoint
// mounted at POST /graphql, seeded with an "article" content type — the
// same shape testServerWithAuth uses in internal/api.
func testServer(t *testing.T) (http.Handler, testDeps) {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	migs = append(migs, content.Migrations...)
	migs = append(migs, media.Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}
	comps := composition.NewStore(d)
	if err := comps.Save(context.Background(), &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
		ContentTypes: map[string]contract.ContentType{
			"article": {Fields: map[string]contract.Field{
				"title": {Type: contract.FieldString, Required: true},
				"body":  {Type: contract.FieldRichText},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	identities := identity.NewService(d)
	sessions := identity.NewSessions(d)
	contentAPI := content.NewAPI(comps, content.NewStore(d))
	mediaAPI := media.NewAPI(media.NewStore(d), filepath.Join(t.TempDir(), "media"))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	resolver := glyphqlgraphql.NewResolver(comps, contentAPI, mediaAPI, identities, sessions, log)
	mux := http.NewServeMux()
	mux.Handle("POST /graphql", glyphqlgraphql.NewHandler(resolver))
	return mux, testDeps{identities: identities, sessions: sessions, content: contentAPI}
}

// loginAdmin creates (if needed) and authenticates the standard admin
// fixture, returning a bearer token for it.
func loginAdmin(t *testing.T, deps testDeps) string {
	t.Helper()
	ctx := context.Background()
	const email, password = "admin@example.com", "correct horse battery"
	if _, err := deps.identities.Authenticate(ctx, email, password); err != nil {
		if err := deps.identities.CreateAdmin(ctx, email, password); err != nil {
			t.Fatal(err)
		}
	}
	return loginRole(t, deps, email, "" /* role ignored; account already exists */)
}

// loginRole creates an account with the given role (if it doesn't already
// exist) and returns a bearer token for it. A blank role is used only for
// an account loginAdmin already created.
func loginRole(t *testing.T, deps testDeps, email, role string) string {
	t.Helper()
	ctx := context.Background()
	const password = "correct horse battery"
	u, err := deps.identities.Authenticate(ctx, email, password)
	if err != nil {
		if role == "" {
			t.Fatalf("expected account %s to already exist: %v", email, err)
		}
		u, err = deps.identities.CreateUser(ctx, email, password, role)
		if err != nil {
			t.Fatal(err)
		}
	}
	sess, err := deps.sessions.Create(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return sess.Token
}

// gqlResponse is the standard GraphQL response envelope.
type gqlResponse struct {
	Data   map[string]any `json:"data"`
	Errors []struct {
		Message    string         `json:"message"`
		Extensions map[string]any `json:"extensions"`
	} `json:"errors"`
}

// doGraphQL POSTs a GraphQL query/mutation to h, optionally bearing token as
// an Authorization: Bearer header, and decodes the response envelope.
func doGraphQL(t *testing.T, h http.Handler, token, query string, variables map[string]any) (*httptest.ResponseRecorder, gqlResponse) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp gqlResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode graphql response %q: %v", rec.Body.String(), err)
	}
	return rec, resp
}

// mustReadAll is a small helper kept for future streaming-response tests;
// unused today but documents the intended pattern for multipart/binary
// transports if ever needed.
var _ = io.ReadAll

func TestContentItemQueryReturnsPublishedItemAnonymously(t *testing.T) {
	h, deps := testServer(t)
	item, err := deps.content.Create(context.Background(), "article", map[string]any{"title": "Hello", "body": "World"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.content.Publish(context.Background(), "article", item.ID); err != nil {
		t.Fatal(err)
	}

	const query = `query($id: String!) {
		contentItem(type: "article", id: $id) { id type status data version }
	}`
	_, resp := doGraphQL(t, h, "", query, map[string]any{"id": item.ID})
	if len(resp.Errors) != 0 {
		t.Fatalf("errors = %v", resp.Errors)
	}
	got, ok := resp.Data["contentItem"].(map[string]any)
	if !ok {
		t.Fatalf("contentItem = %v, want an object", resp.Data["contentItem"])
	}
	if got["id"] != item.ID || got["status"] != "published" {
		t.Errorf("contentItem = %v, want id=%s status=published", got, item.ID)
	}
	data, _ := got["data"].(map[string]any)
	if data["title"] != "Hello" {
		t.Errorf("contentItem.data = %v, want title=Hello", data)
	}
}

func TestContentItemQueryHidesDraftsFromAnonymousCallers(t *testing.T) {
	h, deps := testServer(t)
	item, err := deps.content.Create(context.Background(), "article", map[string]any{"title": "Draft", "body": "shh"})
	if err != nil {
		t.Fatal(err)
	}

	const query = `query($id: String!) { contentItem(type: "article", id: $id) { id } }`
	_, resp := doGraphQL(t, h, "", query, map[string]any{"id": item.ID})
	if len(resp.Errors) != 1 {
		t.Fatalf("errors = %v, want 1 NOT_FOUND error", resp.Errors)
	}
	if code := resp.Errors[0].Extensions["code"]; code != "NOT_FOUND" {
		t.Errorf("error code = %v, want NOT_FOUND", code)
	}
}

func TestContentItemQueryShowsDraftsToPrivilegedCallers(t *testing.T) {
	h, deps := testServer(t)
	ctx := context.Background()
	if err := deps.identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	admin, err := deps.identities.Authenticate(ctx, "admin@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := deps.sessions.Create(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	item, err := deps.content.Create(ctx, "article", map[string]any{"title": "Draft", "body": "shh"})
	if err != nil {
		t.Fatal(err)
	}

	const query = `query($id: String!) { contentItem(type: "article", id: $id) { id status } }`
	_, resp := doGraphQL(t, h, sess.Token, query, map[string]any{"id": item.ID})
	if len(resp.Errors) != 0 {
		t.Fatalf("errors = %v", resp.Errors)
	}
	got, ok := resp.Data["contentItem"].(map[string]any)
	if !ok || got["status"] != "draft" {
		t.Fatalf("contentItem = %v, want status=draft", resp.Data["contentItem"])
	}
}

func TestContentItemsQueryListsPublishedItemsOnly(t *testing.T) {
	h, deps := testServer(t)
	ctx := context.Background()
	published, err := deps.content.Create(ctx, "article", map[string]any{"title": "Pub", "body": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.content.Publish(ctx, "article", published.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.content.Create(ctx, "article", map[string]any{"title": "Draft", "body": "x"}); err != nil {
		t.Fatal(err)
	}

	const query = `{ contentItems(type: "article") { id status } }`
	_, resp := doGraphQL(t, h, "", query, nil)
	if len(resp.Errors) != 0 {
		t.Fatalf("errors = %v", resp.Errors)
	}
	items, ok := resp.Data["contentItems"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("contentItems = %v, want 1 published item", resp.Data["contentItems"])
	}
	got := items[0].(map[string]any)
	if got["id"] != published.ID {
		t.Errorf("contentItems[0].id = %v, want %s", got["id"], published.ID)
	}
}

func TestContentVersionsQueryReturnsHistory(t *testing.T) {
	h, deps := testServer(t)
	ctx := context.Background()
	item, err := deps.content.Create(ctx, "article", map[string]any{"title": "V1", "body": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.content.Update(ctx, "article", item.ID, map[string]any{"title": "V2", "body": "x"}); err != nil {
		t.Fatal(err)
	}

	const query = `query($id: String!) { contentVersions(type: "article", id: $id) { version status } }`
	_, resp := doGraphQL(t, h, "", query, map[string]any{"id": item.ID})
	if len(resp.Errors) != 0 {
		t.Fatalf("errors = %v", resp.Errors)
	}
	versions, ok := resp.Data["contentVersions"].([]any)
	if !ok || len(versions) != 2 {
		t.Fatalf("contentVersions = %v, want 2 entries", resp.Data["contentVersions"])
	}
}

func TestCreateContentItemMutationRequiresContentWrite(t *testing.T) {
	h, deps := testServer(t)
	ctx := context.Background()
	if err := deps.identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	admin, _ := deps.identities.Authenticate(ctx, "admin@example.com", "correct horse battery")
	sess, err := deps.sessions.Create(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	const mutation = `mutation($data: Map!) {
		createContentItem(type: "article", data: $data) { id status data }
	}`
	variables := map[string]any{"data": map[string]any{"title": "New", "body": "x"}}

	// Admin (holds content:write) succeeds.
	_, resp := doGraphQL(t, h, sess.Token, mutation, variables)
	if len(resp.Errors) != 0 {
		t.Fatalf("admin create errors = %v", resp.Errors)
	}
	created, ok := resp.Data["createContentItem"].(map[string]any)
	if !ok || created["status"] != "draft" {
		t.Fatalf("createContentItem = %v, want status=draft", resp.Data["createContentItem"])
	}

	// Anonymous is rejected with UNAUTHENTICATED.
	_, resp = doGraphQL(t, h, "", mutation, variables)
	if len(resp.Errors) != 1 || resp.Errors[0].Extensions["code"] != "UNAUTHENTICATED" {
		t.Fatalf("anonymous create errors = %v, want 1 UNAUTHENTICATED", resp.Errors)
	}

	// A viewer (holds content:read only, not content:write) is rejected
	// with FORBIDDEN — proves the capability gate, not just presence of a
	// valid session.
	if _, err := deps.identities.CreateUser(ctx, "viewer@example.com", "correct horse battery", "viewer"); err != nil {
		t.Fatal(err)
	}
	viewer, _ := deps.identities.Authenticate(ctx, "viewer@example.com", "correct horse battery")
	viewerSess, err := deps.sessions.Create(ctx, viewer.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, resp = doGraphQL(t, h, viewerSess.Token, mutation, variables)
	if len(resp.Errors) != 1 || resp.Errors[0].Extensions["code"] != "FORBIDDEN" {
		t.Fatalf("viewer create errors = %v, want 1 FORBIDDEN", resp.Errors)
	}
}

func TestUpdateContentItemMutation(t *testing.T) {
	h, deps := testServer(t)
	ctx := context.Background()
	item, err := deps.content.Create(ctx, "article", map[string]any{"title": "Old", "body": "x"})
	if err != nil {
		t.Fatal(err)
	}
	adminToken := loginAdmin(t, deps)

	const mutation = `mutation($id: String!, $data: Map!) {
		updateContentItem(type: "article", id: $id, data: $data) { id version data }
	}`
	variables := map[string]any{"id": item.ID, "data": map[string]any{"title": "New", "body": "x"}}

	_, resp := doGraphQL(t, h, adminToken, mutation, variables)
	if len(resp.Errors) != 0 {
		t.Fatalf("errors = %v", resp.Errors)
	}
	updated, ok := resp.Data["updateContentItem"].(map[string]any)
	if !ok {
		t.Fatalf("updateContentItem = %v", resp.Data["updateContentItem"])
	}
	data, _ := updated["data"].(map[string]any)
	if data["title"] != "New" {
		t.Errorf("updated data = %v, want title=New", data)
	}

	// Editor lacks content:write? No — editor holds content:write. Use
	// viewer instead to prove the gate.
	viewerToken := loginRole(t, deps, "viewer2@example.com", "viewer")
	_, resp = doGraphQL(t, h, viewerToken, mutation, variables)
	if len(resp.Errors) != 1 || resp.Errors[0].Extensions["code"] != "FORBIDDEN" {
		t.Fatalf("viewer update errors = %v, want 1 FORBIDDEN", resp.Errors)
	}
}

func TestDeleteContentItemMutation(t *testing.T) {
	h, deps := testServer(t)
	ctx := context.Background()
	item, err := deps.content.Create(ctx, "article", map[string]any{"title": "Gone", "body": "x"})
	if err != nil {
		t.Fatal(err)
	}
	adminToken := loginAdmin(t, deps)

	const mutation = `mutation($id: String!) { deleteContentItem(type: "article", id: $id) }`
	_, resp := doGraphQL(t, h, adminToken, mutation, map[string]any{"id": item.ID})
	if len(resp.Errors) != 0 {
		t.Fatalf("errors = %v", resp.Errors)
	}
	if resp.Data["deleteContentItem"] != true {
		t.Fatalf("deleteContentItem = %v, want true", resp.Data["deleteContentItem"])
	}
	if _, err := deps.content.Get(ctx, "article", item.ID); err == nil {
		t.Error("item still exists after delete")
	}
}

// TestPublishContentItemMutationRequiresContentPublish proves the
// publish/unpublish capability boundary the task explicitly calls out: an
// editor holds content:write but NOT content:publish (see
// internal/permission/permission.go's roleCapabilities matrix), so an
// editor attempting to publish must be rejected even though the same editor
// can create/update/delete content freely.
func TestPublishContentItemMutationRequiresContentPublish(t *testing.T) {
	h, deps := testServer(t)
	ctx := context.Background()
	item, err := deps.content.Create(ctx, "article", map[string]any{"title": "Draft", "body": "x"})
	if err != nil {
		t.Fatal(err)
	}
	adminToken := loginAdmin(t, deps)
	editorToken := loginRole(t, deps, "editor@example.com", "editor")

	const publishMutation = `mutation($id: String!) { publishContentItem(type: "article", id: $id) { status } }`

	// Editor is rejected — holds content:write but not content:publish.
	_, resp := doGraphQL(t, h, editorToken, publishMutation, map[string]any{"id": item.ID})
	if len(resp.Errors) != 1 || resp.Errors[0].Extensions["code"] != "FORBIDDEN" {
		t.Fatalf("editor publish errors = %v, want 1 FORBIDDEN", resp.Errors)
	}

	// Admin succeeds.
	_, resp = doGraphQL(t, h, adminToken, publishMutation, map[string]any{"id": item.ID})
	if len(resp.Errors) != 0 {
		t.Fatalf("admin publish errors = %v", resp.Errors)
	}
	published, _ := resp.Data["publishContentItem"].(map[string]any)
	if published["status"] != "published" {
		t.Fatalf("status after publish = %v, want published", published["status"])
	}

	// Unpublish reverts it, same capability gate.
	const unpublishMutation = `mutation($id: String!) { unpublishContentItem(type: "article", id: $id) { status } }`
	_, resp = doGraphQL(t, h, editorToken, unpublishMutation, map[string]any{"id": item.ID})
	if len(resp.Errors) != 1 || resp.Errors[0].Extensions["code"] != "FORBIDDEN" {
		t.Fatalf("editor unpublish errors = %v, want 1 FORBIDDEN", resp.Errors)
	}
	_, resp = doGraphQL(t, h, adminToken, unpublishMutation, map[string]any{"id": item.ID})
	if len(resp.Errors) != 0 {
		t.Fatalf("admin unpublish errors = %v", resp.Errors)
	}
	unpublished, _ := resp.Data["unpublishContentItem"].(map[string]any)
	if unpublished["status"] != "draft" {
		t.Fatalf("status after unpublish = %v, want draft", unpublished["status"])
	}
}

func TestRollbackContentItemMutation(t *testing.T) {
	h, deps := testServer(t)
	ctx := context.Background()
	item, err := deps.content.Create(ctx, "article", map[string]any{"title": "V1", "body": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.content.Update(ctx, "article", item.ID, map[string]any{"title": "V2", "body": "x"}); err != nil {
		t.Fatal(err)
	}
	adminToken := loginAdmin(t, deps)

	const mutation = `mutation($id: String!, $version: Int!) {
		rollbackContentItem(type: "article", id: $id, version: $version) { data }
	}`
	_, resp := doGraphQL(t, h, adminToken, mutation, map[string]any{"id": item.ID, "version": 1})
	if len(resp.Errors) != 0 {
		t.Fatalf("errors = %v", resp.Errors)
	}
	rolled, _ := resp.Data["rollbackContentItem"].(map[string]any)
	data, _ := rolled["data"].(map[string]any)
	if data["title"] != "V1" {
		t.Fatalf("rollback data = %v, want title=V1", data)
	}
}

func TestDefineContentTypeMutationRequiresAdmin(t *testing.T) {
	h, deps := testServer(t)
	adminToken := loginAdmin(t, deps)
	editorToken := loginRole(t, deps, "editor3@example.com", "editor")

	const mutation = `mutation($fields: [FieldInput!]!) {
		defineContentType(name: "product", fields: $fields) { name fields { name type required } }
	}`
	variables := map[string]any{"fields": []map[string]any{
		{"name": "sku", "type": "string", "required": true},
	}}

	// Editor holds content:write but not content_types:manage — rejected.
	_, resp := doGraphQL(t, h, editorToken, mutation, variables)
	if len(resp.Errors) != 1 || resp.Errors[0].Extensions["code"] != "FORBIDDEN" {
		t.Fatalf("editor defineContentType errors = %v, want 1 FORBIDDEN", resp.Errors)
	}

	// Admin succeeds.
	_, resp = doGraphQL(t, h, adminToken, mutation, variables)
	if len(resp.Errors) != 0 {
		t.Fatalf("admin defineContentType errors = %v", resp.Errors)
	}
	defined, ok := resp.Data["defineContentType"].(map[string]any)
	if !ok || defined["name"] != "product" {
		t.Fatalf("defineContentType = %v, want name=product", resp.Data["defineContentType"])
	}
	fields, _ := defined["fields"].([]any)
	if len(fields) != 1 {
		t.Fatalf("defined fields = %v, want 1", fields)
	}

	// It shows up in a subsequent contentTypes query.
	_, resp = doGraphQL(t, h, "", `{ contentTypes { name } }`, nil)
	types, _ := resp.Data["contentTypes"].([]any)
	found := false
	for _, ty := range types {
		if m, ok := ty.(map[string]any); ok && m["name"] == "product" {
			found = true
		}
	}
	if !found {
		t.Errorf("contentTypes after define = %v, want product present", types)
	}
}

func TestRemoveContentTypeMutationBlockedWhenItemsExist(t *testing.T) {
	h, deps := testServer(t)
	ctx := context.Background()
	adminToken := loginAdmin(t, deps)
	if _, err := deps.content.Create(ctx, "article", map[string]any{"title": "Keeps type alive", "body": "x"}); err != nil {
		t.Fatal(err)
	}

	const mutation = `mutation { removeContentType(name: "article") }`
	_, resp := doGraphQL(t, h, adminToken, mutation, nil)
	if len(resp.Errors) != 1 || resp.Errors[0].Extensions["code"] != "CONFLICT" {
		t.Fatalf("removeContentType with items errors = %v, want 1 CONFLICT", resp.Errors)
	}

	// The type must still be usable — the guard did not partially apply.
	_, resp = doGraphQL(t, h, "", `{ contentTypes { name } }`, nil)
	types, _ := resp.Data["contentTypes"].([]any)
	found := false
	for _, ty := range types {
		if m, ok := ty.(map[string]any); ok && m["name"] == "article" {
			found = true
		}
	}
	if !found {
		t.Errorf("contentTypes after blocked remove = %v, want article still present", types)
	}
}

func TestRemoveContentTypeMutationRemovesEmptyType(t *testing.T) {
	h, deps := testServer(t)
	adminToken := loginAdmin(t, deps)
	defineMutation := `mutation { defineContentType(name: "tag", fields: [{name: "name", type: "string", required: true}]) { name } }`
	if _, resp := doGraphQL(t, h, adminToken, defineMutation, nil); len(resp.Errors) != 0 {
		t.Fatalf("define errors = %v", resp.Errors)
	}

	_, resp := doGraphQL(t, h, adminToken, `mutation { removeContentType(name: "tag") }`, nil)
	if len(resp.Errors) != 0 {
		t.Fatalf("errors = %v", resp.Errors)
	}
	if resp.Data["removeContentType"] != true {
		t.Fatalf("removeContentType = %v, want true", resp.Data["removeContentType"])
	}
}

func TestContentTypesQueryIsPublic(t *testing.T) {
	h, _ := testServer(t)
	_, resp := doGraphQL(t, h, "", `{ contentTypes { name fields { name type required localized to } } }`, nil)
	if len(resp.Errors) != 0 {
		t.Fatalf("errors = %v", resp.Errors)
	}
	types, ok := resp.Data["contentTypes"].([]any)
	if !ok || len(types) != 1 {
		t.Fatalf("contentTypes = %v, want 1 entry", resp.Data["contentTypes"])
	}
	article, ok := types[0].(map[string]any)
	if !ok || article["name"] != "article" {
		t.Fatalf("contentTypes[0] = %v, want name=article", types[0])
	}
	fields, ok := article["fields"].([]any)
	if !ok || len(fields) != 2 {
		t.Fatalf("article fields = %v, want 2", article["fields"])
	}
}
