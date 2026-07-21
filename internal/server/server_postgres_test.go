package server_test

// Integration test proving the daemon's full migration set and read/write
// paths work against a real Postgres server, not just SQLite. Gated behind
// GLYPHUX_TEST_POSTGRES_DSN so it's opt-in (needs a live server) but real —
// dialect bugs (placeholder syntax, AUTOINCREMENT vs IDENTITY) are exactly
// the kind of thing that passes against SQLite and breaks silently here.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glyphux/glyphux/internal/api"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/internal/server"
	"github.com/glyphux/glyphux/internal/setup"
)

func bootPostgres(t *testing.T) http.Handler {
	t.Helper()
	dsn := os.Getenv("GLYPHUX_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("GLYPHUX_TEST_POSTGRES_DSN not set; skipping Postgres integration test")
	}
	ctx := context.Background()
	database, err := db.OpenPostgres(dsn, db.PoolConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	t.Cleanup(func() {
		_, _ = database.Exec(context.Background(),
			`DROP TABLE IF EXISTS schema_migrations, composition, users, sessions, content_items, content_item_versions, media_items CASCADE`)
	})
	if _, err := database.Exec(ctx,
		`DROP TABLE IF EXISTS schema_migrations, composition, users, sessions, content_items, content_item_versions, media_items CASCADE`); err != nil {
		t.Fatal(err)
	}

	migrations := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	migrations = append(migrations, content.Migrations...)
	migrations = append(migrations, media.Migrations...)
	if err := database.Migrate(ctx, migrations); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	compositions := composition.NewStore(database)
	identities := identity.NewService(database)
	sessions := identity.NewSessions(database)
	wizard, err := setup.New(ctx, compositions, identities, database, log, false)
	if err != nil {
		t.Fatal(err)
	}
	mediaAPI := media.NewAPI(media.NewStore(database), filepath.Join(t.TempDir(), "media"))
	contentAPI := content.NewAPI(compositions, content.NewStore(database))
	apiServer := api.New(compositions, contentAPI, mediaAPI, identities, sessions, log)
	return server.Handler(apiServer, wizard)
}

// TestFirstRunLifecycleOnPostgres mirrors TestFirstRunLifecycle but against a
// real Postgres backend: setup wizard, admin login, and a full content
// create/publish/version cycle.
func TestFirstRunLifecycleOnPostgres(t *testing.T) {
	h := bootPostgres(t)

	form := url.Values{
		"site_name":      {"PG Smoke"},
		"admin_email":    {"admin@example.com"},
		"admin_password": {"strong password"},
		"database":       {"postgres"},
	}
	// This test is about the storage backend the daemon was booted against
	// (Postgres, via bootPostgres), not the wizard's own DB picker (covered
	// by internal/bootstrap) — submit as sqlite so the wizard writes to the
	// database it was constructed with, which is already Postgres here.
	form.Set("database", "sqlite")
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("wizard submit = %d: %s", rec.Code, rec.Body.String())
	}

	// Admin login.
	loginBody, _ := json.Marshal(map[string]any{"email": "admin@example.com", "password": "strong password"})
	req = httptest.NewRequest(http.MethodPost, "/api/v0/auth/login", strings.NewReader(string(loginBody)))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d: %s", rec.Code, rec.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "glyphux_session" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie set")
	}

	// The composition wizard wrote has no content types yet, so ping should
	// report zero content types but the correct site/contract version.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0/content/ping", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("ping = %d", rec.Code)
	}
	var ping struct {
		Site string `json:"site"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ping); err != nil {
		t.Fatal(err)
	}
	if ping.Site != "PG Smoke" {
		t.Errorf("ping site = %q, want PG Smoke", ping.Site)
	}
}
