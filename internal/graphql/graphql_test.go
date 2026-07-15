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
