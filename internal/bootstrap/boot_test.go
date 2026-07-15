package bootstrap_test

// Integration coverage for the boot decision itself: a virgin install must
// serve the wizard without ever opening a database the operator didn't ask
// for, a sqlite submission must behave exactly as before, and — the actual
// fix for finding #1 — a Postgres submission must take effect immediately,
// with the running process switched to serve from it, no restart.

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
	"github.com/glyphux/glyphux/internal/bootstrap"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/config"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/internal/server"
	"github.com/glyphux/glyphux/internal/setup"
)

func allMigrations() []db.Migration {
	m := append([]db.Migration{}, composition.Migrations...)
	m = append(m, identity.Migrations...)
	m = append(m, content.Migrations...)
	m = append(m, media.Migrations...)
	return m
}

func buildFullHandler(t *testing.T, log *slog.Logger) bootstrap.BuildFullHandlerFunc {
	return func(database *db.DB, compositions *composition.Store, identities *identity.Service, wizard *setup.Wizard) (http.Handler, error) {
		sessions := identity.NewSessions(database)
		mediaAPI := media.NewAPI(media.NewStore(database), filepath.Join(t.TempDir(), "media"))
		contentAPI := content.NewAPI(compositions, content.NewStore(database))
		apiServer := api.New(compositions, contentAPI, mediaAPI, identities, sessions, log)
		return server.Handler(apiServer, wizard), nil
	}
}

func bootOptions(t *testing.T, dataDir string) bootstrap.Options {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return bootstrap.Options{
		DataDir:           dataDir,
		SQLitePath:        filepath.Join(dataDir, "glyphux.db"),
		Migrations:        allMigrations(),
		Log:               log,
		BuildFullHandler:  buildFullHandler(t, log),
		TrustProxyHeaders: false,
	}
}

func mustBoot(t *testing.T, opts bootstrap.Options) *bootstrap.Result {
	t.Helper()
	res, err := bootstrap.Boot(context.Background(), opts)
	if err != nil {
		t.Fatalf("Boot: %v", err)
	}
	t.Cleanup(func() { res.Database.Close() })
	return res
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func postForm(t *testing.T, h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestBootVirginInstallServesWizardWithoutOpeningExtraDatabases(t *testing.T) {
	dataDir := t.TempDir()
	res := mustBoot(t, bootOptions(t, dataDir))

	if rec := get(t, res.Handler, "/setup"); rec.Code != http.StatusOK {
		t.Fatalf("/setup = %d", rec.Code)
	}
	// Only the bootstrap sqlite file should exist yet — no Postgres, no
	// bootstrap config (setup hasn't completed).
	if _, _, err := bootstrap.Load(dataDir); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := bootstrap.Load(dataDir); found {
		t.Fatal("bootstrap config persisted before setup completed")
	}
}

func TestBootSqliteSubmitCompletesInProcessAndPersists(t *testing.T) {
	dataDir := t.TempDir()
	res := mustBoot(t, bootOptions(t, dataDir))

	rec := postForm(t, res.Handler, "/setup", url.Values{
		"site_name":      {"Bootstrap Site"},
		"admin_email":    {"admin@example.com"},
		"admin_password": {"strong password"},
		"database":       {"sqlite"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("submit = %d: %s", rec.Code, rec.Body.String())
	}

	if rec := get(t, res.Handler, "/api/v0/content/ping"); rec.Code != http.StatusOK {
		t.Fatalf("ping after sqlite setup = %d", rec.Code)
	}

	cfg, found, err := bootstrap.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !found || cfg.Driver != "sqlite" {
		t.Fatalf("bootstrap config = %+v, found=%v, want driver=sqlite", cfg, found)
	}
}

func TestBootReopensPersistedSqliteConfigWithoutWizard(t *testing.T) {
	dataDir := t.TempDir()
	first := mustBoot(t, bootOptions(t, dataDir))
	if rec := postForm(t, first.Handler, "/setup", url.Values{
		"site_name":      {"Reboot Site"},
		"admin_email":    {"admin@example.com"},
		"admin_password": {"strong password"},
		"database":       {"sqlite"},
	}); rec.Code != http.StatusOK {
		t.Fatalf("initial submit = %d", rec.Code)
	}

	second := mustBoot(t, bootOptions(t, dataDir))
	if rec := get(t, second.Handler, "/setup"); rec.Code != http.StatusGone {
		t.Fatalf("second boot GET /setup = %d, want 410 (already configured)", rec.Code)
	}
	if rec := get(t, second.Handler, "/api/v0/composition"); rec.Code != http.StatusOK {
		t.Fatalf("second boot /api/v0/composition = %d", rec.Code)
	}
}

func TestBootSelfHealsMissingConfigFile(t *testing.T) {
	dataDir := t.TempDir()
	first := mustBoot(t, bootOptions(t, dataDir))
	if rec := postForm(t, first.Handler, "/setup", url.Values{
		"site_name":      {"Heal Site"},
		"admin_email":    {"admin@example.com"},
		"admin_password": {"strong password"},
		"database":       {"sqlite"},
	}); rec.Code != http.StatusOK {
		t.Fatalf("initial submit = %d", rec.Code)
	}

	// Simulate the config write having failed/been lost on the previous boot.
	if err := os.Remove(filepath.Join(dataDir, "glyphux.json")); err != nil {
		t.Fatal(err)
	}

	second := mustBoot(t, bootOptions(t, dataDir))
	if rec := get(t, second.Handler, "/setup"); rec.Code != http.StatusGone {
		t.Fatalf("GET /setup after self-heal = %d, want 410", rec.Code)
	}
	if _, found, err := bootstrap.Load(dataDir); err != nil || !found {
		t.Fatalf("bootstrap config not repaired: found=%v err=%v", found, err)
	}
}

func TestBootExplicitDriverAlwaysOpensThatDatabase(t *testing.T) {
	dataDir := t.TempDir()
	explicitPath := filepath.Join(dataDir, "explicit.db")
	opts := bootOptions(t, dataDir)
	opts.DatabaseExplicit = true
	opts.Database = config.DatabaseConfig{Driver: "sqlite"}
	opts.SQLitePath = explicitPath

	res := mustBoot(t, opts)
	if rec := get(t, res.Handler, "/setup"); rec.Code != http.StatusOK {
		t.Fatalf("/setup with explicit driver = %d", rec.Code)
	}
	if _, err := os.Stat(explicitPath); err != nil {
		t.Fatalf("explicit database file not created: %v", err)
	}
	// Explicit config is never recorded to the bootstrap file — it always
	// wins over whatever is persisted, so persisting it would be misleading.
	if _, found, _ := bootstrap.Load(dataDir); found {
		t.Fatal("bootstrap config was persisted for an explicit-driver boot")
	}
}

func TestBootPostgresSubmitSwitchesWithoutRestart(t *testing.T) {
	dsn := os.Getenv("GLYPHUX_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("GLYPHUX_TEST_POSTGRES_DSN not set; skipping Postgres integration test")
	}
	t.Setenv("GLYPHUX_DB_DSN", dsn)

	// Clean slate: this test owns the schema in the shared test database.
	cleaner, err := db.OpenPostgres(dsn, db.PoolConfig{})
	if err != nil {
		t.Fatal(err)
	}
	const dropAll = `DROP TABLE IF EXISTS schema_migrations, composition, users, sessions, content_items, content_item_versions, media_items CASCADE`
	if _, err := cleaner.Exec(context.Background(), dropAll); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleaner.Exec(context.Background(), dropAll)
		cleaner.Close()
	})

	dataDir := t.TempDir()
	res := mustBoot(t, bootOptions(t, dataDir))

	rec := postForm(t, res.Handler, "/setup", url.Values{
		"site_name":      {"PG Bootstrap"},
		"admin_email":    {"admin@example.com"},
		"admin_password": {"strong password"},
		"database":       {"postgres"},
		"database_dsn":   {dsn},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("postgres submit = %d: %s", rec.Code, rec.Body.String())
	}

	// The SAME handler, same process, no restart — must now serve from
	// Postgres. Prove it by reading back the composition the submit wrote.
	rec = get(t, res.Handler, "/api/v0/content/ping")
	if rec.Code != http.StatusOK {
		t.Fatalf("ping after postgres switch = %d: %s", rec.Code, rec.Body.String())
	}
	var ping struct {
		Site string `json:"site"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ping); err != nil {
		t.Fatal(err)
	}
	if ping.Site != "PG Bootstrap" {
		t.Errorf("ping site = %q, want PG Bootstrap", ping.Site)
	}

	// The bootstrap sqlite file must NOT have received the composition —
	// selecting Postgres must actually redirect the write, not just label it.
	sqliteOnly, err := db.OpenSQLite(filepath.Join(dataDir, "glyphux.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteOnly.Close()
	sqliteComps := composition.NewStore(sqliteOnly)
	if exists, err := sqliteComps.Exists(context.Background()); err != nil {
		t.Fatal(err)
	} else if exists {
		t.Error("composition was written to the abandoned bootstrap sqlite store, not Postgres")
	}

	cfg, found, err := bootstrap.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !found || cfg.Driver != "postgres" || cfg.DSNEnvVar != "GLYPHUX_DB_DSN" {
		t.Fatalf("bootstrap config = %+v, found=%v, want driver=postgres dsn_env_var=GLYPHUX_DB_DSN", cfg, found)
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, "glyphux.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "://") {
		t.Errorf("persisted bootstrap config leaked the DSN: %s", raw)
	}
}
