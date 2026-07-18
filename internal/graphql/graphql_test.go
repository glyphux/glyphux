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
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/contract"
)

// testDeps exposes the domain services a graphql test server was built
// from, so tests can seed data or mint sessions directly.
// adminPrincipal is used by tests that seed data directly through the
// domain APIs (bypassing HTTP), since those APIs now enforce their own
// capability checks (PRD §10.5) independent of the transport layer.
var adminPrincipal = &permission.Principal{Role: permission.RoleAdmin}

type testDeps struct {
	identities *identity.Service
	sessions   *identity.Sessions
	content    *content.API
	media      *media.API
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
	if err := comps.Save(context.Background(), nil, &contract.Composition{
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
	return mux, testDeps{identities: identities, sessions: sessions, content: contentAPI, media: mediaAPI}
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

// doGraphQLCookie is doGraphQL but authenticates via the session cookie
// browser clients use, instead of a bearer header — proving GraphQL
// recognizes the same session REST does.
func doGraphQLCookie(t *testing.T, h http.Handler, token, query string, variables map[string]any) (*httptest.ResponseRecorder, gqlResponse) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "glyphux_session", Value: token})
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
	item, err := deps.content.Create(context.Background(), adminPrincipal, "article", map[string]any{"title": "Hello", "body": "World"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.content.Publish(context.Background(), adminPrincipal, "article", item.ID); err != nil {
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
	item, err := deps.content.Create(context.Background(), adminPrincipal, "article", map[string]any{"title": "Draft", "body": "shh"})
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
	item, err := deps.content.Create(ctx, adminPrincipal, "article", map[string]any{"title": "Draft", "body": "shh"})
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
	published, err := deps.content.Create(ctx, adminPrincipal, "article", map[string]any{"title": "Pub", "body": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.content.Publish(ctx, adminPrincipal, "article", published.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.content.Create(ctx, adminPrincipal, "article", map[string]any{"title": "Draft", "body": "x"}); err != nil {
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
	item, err := deps.content.Create(ctx, adminPrincipal, "article", map[string]any{"title": "V1", "body": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.content.Update(ctx, adminPrincipal, "article", item.ID, map[string]any{"title": "V2", "body": "x"}); err != nil {
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
	item, err := deps.content.Create(ctx, adminPrincipal, "article", map[string]any{"title": "Old", "body": "x"})
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
	item, err := deps.content.Create(ctx, adminPrincipal, "article", map[string]any{"title": "Gone", "body": "x"})
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
	if _, err := deps.content.Get(ctx, adminPrincipal, "article", item.ID); err == nil {
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
	item, err := deps.content.Create(ctx, adminPrincipal, "article", map[string]any{"title": "Draft", "body": "x"})
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
	item, err := deps.content.Create(ctx, adminPrincipal, "article", map[string]any{"title": "V1", "body": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.content.Update(ctx, adminPrincipal, "article", item.ID, map[string]any{"title": "V2", "body": "x"}); err != nil {
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
	if _, err := deps.content.Create(ctx, adminPrincipal, "article", map[string]any{"title": "Keeps type alive", "body": "x"}); err != nil {
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

func TestMediaQueriesArePublicAndDeleteRequiresMediaWrite(t *testing.T) {
	h, deps := testServer(t)
	ctx := context.Background()
	// A tiny valid PNG (1x1 transparent pixel).
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
		0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
		0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
	item, err := deps.media.Upload(ctx, adminPrincipal, "pixel.png", "image/png", png)
	if err != nil {
		t.Fatal(err)
	}

	const getQuery = `query($id: String!) { mediaItem(id: $id) { id filename mimeType } }`
	_, resp := doGraphQL(t, h, "", getQuery, map[string]any{"id": item.ID})
	if len(resp.Errors) != 0 {
		t.Fatalf("mediaItem errors = %v", resp.Errors)
	}
	got, ok := resp.Data["mediaItem"].(map[string]any)
	if !ok || got["id"] != item.ID {
		t.Fatalf("mediaItem = %v, want id=%s", resp.Data["mediaItem"], item.ID)
	}

	const listQuery = `{ mediaItems { id } }`
	_, resp = doGraphQL(t, h, "", listQuery, nil)
	if len(resp.Errors) != 0 {
		t.Fatalf("mediaItems errors = %v", resp.Errors)
	}
	items, _ := resp.Data["mediaItems"].([]any)
	if len(items) != 1 {
		t.Fatalf("mediaItems = %v, want 1", items)
	}

	// Delete: viewer is rejected (no media:write), admin succeeds.
	const deleteMutation = `mutation($id: String!) { deleteMediaItem(id: $id) }`
	viewerToken := loginRole(t, deps, "viewer4@example.com", "viewer")
	_, resp = doGraphQL(t, h, viewerToken, deleteMutation, map[string]any{"id": item.ID})
	if len(resp.Errors) != 1 || resp.Errors[0].Extensions["code"] != "FORBIDDEN" {
		t.Fatalf("viewer delete errors = %v, want 1 FORBIDDEN", resp.Errors)
	}

	adminToken := loginAdmin(t, deps)
	_, resp = doGraphQL(t, h, adminToken, deleteMutation, map[string]any{"id": item.ID})
	if len(resp.Errors) != 0 {
		t.Fatalf("admin delete errors = %v", resp.Errors)
	}
	if resp.Data["deleteMediaItem"] != true {
		t.Fatalf("deleteMediaItem = %v, want true", resp.Data["deleteMediaItem"])
	}
}

func TestUsersQueryAndCreateUserMutationAreAdminOnly(t *testing.T) {
	h, deps := testServer(t)
	adminToken := loginAdmin(t, deps)
	editorToken := loginRole(t, deps, "editor5@example.com", "editor")

	const usersQuery = `{ users { email role } }`

	// Editor is rejected — users:manage is admin-only.
	_, resp := doGraphQL(t, h, editorToken, usersQuery, nil)
	if len(resp.Errors) != 1 || resp.Errors[0].Extensions["code"] != "FORBIDDEN" {
		t.Fatalf("editor users query errors = %v, want 1 FORBIDDEN", resp.Errors)
	}

	// Anonymous is rejected with UNAUTHENTICATED.
	_, resp = doGraphQL(t, h, "", usersQuery, nil)
	if len(resp.Errors) != 1 || resp.Errors[0].Extensions["code"] != "UNAUTHENTICATED" {
		t.Fatalf("anonymous users query errors = %v, want 1 UNAUTHENTICATED", resp.Errors)
	}

	// Admin succeeds and sees both accounts created so far.
	_, resp = doGraphQL(t, h, adminToken, usersQuery, nil)
	if len(resp.Errors) != 0 {
		t.Fatalf("admin users query errors = %v", resp.Errors)
	}
	users, ok := resp.Data["users"].([]any)
	if !ok || len(users) != 2 {
		t.Fatalf("users = %v, want 2 accounts", resp.Data["users"])
	}

	// createUser: editor rejected, admin succeeds.
	const createMutation = `mutation($email: String!, $password: String!, $role: String!) {
		createUser(email: $email, password: $password, role: $role) { email role }
	}`
	variables := map[string]any{"email": "newbie@example.com", "password": "correct horse battery", "role": "viewer"}

	_, resp = doGraphQL(t, h, editorToken, createMutation, variables)
	if len(resp.Errors) != 1 || resp.Errors[0].Extensions["code"] != "FORBIDDEN" {
		t.Fatalf("editor createUser errors = %v, want 1 FORBIDDEN", resp.Errors)
	}

	_, resp = doGraphQL(t, h, adminToken, createMutation, variables)
	if len(resp.Errors) != 0 {
		t.Fatalf("admin createUser errors = %v", resp.Errors)
	}
	created, ok := resp.Data["createUser"].(map[string]any)
	if !ok || created["email"] != "newbie@example.com" || created["role"] != "viewer" {
		t.Fatalf("createUser = %v, want newbie@example.com/viewer", resp.Data["createUser"])
	}
}

// TestSessionCookieAuthenticatesGraphQLRequests proves a browser client
// authenticated via the glyphux_session cookie (no bearer header at all) is
// recognized by GraphQL exactly like REST recognizes it — the two transports
// must agree on what "authenticated" means, since a caller shouldn't be
// silently anonymous on one transport and privileged on the other.
func TestSessionCookieAuthenticatesGraphQLRequests(t *testing.T) {
	h, deps := testServer(t)
	adminToken := loginAdmin(t, deps)

	const usersQuery = `{ users { email role } }`

	_, resp := doGraphQLCookie(t, h, adminToken, usersQuery, nil)
	if len(resp.Errors) != 0 {
		t.Fatalf("cookie-authenticated admin users query errors = %v, want none", resp.Errors)
	}
	if _, ok := resp.Data["users"].([]any); !ok {
		t.Fatalf("cookie-authenticated users query data = %v, want users list", resp.Data)
	}

	// An unknown cookie value must not be treated as authenticated.
	_, resp = doGraphQLCookie(t, h, "not-a-real-token", usersQuery, nil)
	if len(resp.Errors) != 1 || resp.Errors[0].Extensions["code"] != "UNAUTHENTICATED" {
		t.Fatalf("bogus cookie users query errors = %v, want 1 UNAUTHENTICATED", resp.Errors)
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
