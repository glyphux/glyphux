// RED tests for Ticket T7 (gap 4) — the admin-only GET /api/v0/audit
// endpoint. These pin the endpoint's behavior: anonymous requests get 401,
// authenticated-but-not-admin get 403, an admin gets the plugin-filtered
// records (the only accessor the spec names is ListByPlugin, so the plugin
// query parameter is required — pinned interpretation, flagged for owner).
package api_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/api"
	"github.com/glyphux/glyphux/internal/audit"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/pkg/contract"
)

// auditServer boots a server wired with a live audit logger through the
// api.WithAuditLogger option, with admin + editor accounts on file.
func auditServer(t *testing.T) (http.Handler, *audit.Logger) {
	t.Helper()
	ctx := context.Background()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	migs = append(migs, content.Migrations...)
	migs = append(migs, audit.Migrations...)
	if err := d.Migrate(ctx, migs); err != nil {
		t.Fatal(err)
	}
	comps := composition.NewStore(d)
	if err := comps.Save(ctx, nil, &contract.Composition{
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
	deps := authDeps{identities: identity.NewService(d), sessions: identity.NewSessions(d)}
	if err := deps.identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.identities.CreateUser(ctx, "editor@example.com", "correct horse battery", "editor"); err != nil {
		t.Fatal(err)
	}
	logger := audit.NewLogger(d)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := api.New(comps, content.NewAPI(comps, content.NewStore(d)), newTestMediaAPI(t, d), deps.identities, deps.sessions, log, api.WithAuditLogger(logger))
	mux := http.NewServeMux()
	srv.Routes(mux)
	return mux, logger
}

// TestAuditEndpointRequiresAdmin: GIVEN a live audit logger + endpoint,
// WHEN an anonymous caller, an editor, and an admin each GET /api/v0/audit,
// THEN the endpoint answers 401, 403 and 200 respectively.
func TestAuditEndpointRequiresAdmin(t *testing.T) {
	h, _ := auditServer(t)
	ctx := context.Background()

	// Anonymous: 401.
	rec := do(t, h, http.MethodGet, "/api/v0/audit?plugin=content", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}

	// Editor (authenticated, not admin): 403.
	edRec := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "editor@example.com", "password": "correct horse battery",
	})
	rec = doWithCookieBody(t, h, http.MethodGet, "/api/v0/audit?plugin=content", sessionCookie(t, edRec), nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("editor = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// Admin: 200.
	adRec := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "admin@example.com", "password": "correct horse battery",
	})
	rec = doWithCookieBody(t, h, http.MethodGet, "/api/v0/audit?plugin=content", sessionCookie(t, adRec), nil)
	if rec.Code != http.StatusOK {
		t.Errorf("admin = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	_ = ctx
}

// TestAuditEndpointListsPluginFilteredRecords: GIVEN one seeded audit row
// for plugin "content", WHEN an admin asks /api/v0/audit?plugin=content and
// /api/v0/audit?plugin=media, THEN the former returns the row (with the
// stable fields) and the latter returns an empty records list.
func TestAuditEndpointListsPluginFilteredRecords(t *testing.T) {
	h, logger := auditServer(t)
	ctx := context.Background()
	if err := logger.Log(ctx, audit.Record{
		PluginName: "content",
		Action:     "content.created",
		Allowed:    true,
		Detail:     `{"actor_id":"1","role":"admin","item_id":"item-1","type":"article"}`,
	}); err != nil {
		t.Fatal(err)
	}

	adRec := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "admin@example.com", "password": "correct horse battery",
	})
	cookie := sessionCookie(t, adRec)

	rec := doWithCookieBody(t, h, http.MethodGet, "/api/v0/audit?plugin=content", cookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("?plugin=content = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	body := map[string]any{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	records, ok := body["records"].([]any)
	if !ok {
		t.Fatalf("body = %v, want a records array", body)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	row, ok := records[0].(map[string]any)
	if !ok || row["action"] != "content.created" || row["plugin_name"] != "content" {
		t.Errorf("row = %v, want action=content.created plugin_name=content", records[0])
	}

	rec = doWithCookieBody(t, h, http.MethodGet, "/api/v0/audit?plugin=media", cookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("?plugin=media = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	body = map[string]any{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	if records, ok := body["records"].([]any); !ok || len(records) != 0 {
		t.Errorf("?plugin=media records = %v, want empty list", body["records"])
	}
}
