package server_test

// Integration test for the Phase-0 walking skeleton: first boot serves the
// wizard, completing it writes the initial composition and locks the route,
// and the contract-driven endpoints survive a daemon restart on the same
// database (§16 Phase 0 definition of done).

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glyphux/glyphux/internal/api"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/server"
	"github.com/glyphux/glyphux/internal/setup"
)

// boot assembles the daemon's handler on the given database file, exactly as
// cmd/glyphuxd does.
func boot(t *testing.T, dbPath string) http.Handler {
	t.Helper()
	ctx := context.Background()
	database, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	migrations := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	migrations = append(migrations, content.Migrations...)
	if err := database.Migrate(ctx, migrations); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	compositions := composition.NewStore(database)
	identities := identity.NewService(database)
	sessions := identity.NewSessions(database)
	wizard, err := setup.New(ctx, compositions, identities, log)
	if err != nil {
		t.Fatal(err)
	}
	apiServer := api.New(compositions, content.NewAPI(compositions, content.NewStore(database)), identities, sessions, log)
	return server.Handler(apiServer, wizard)
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func postForm(t *testing.T, h http.Handler, path string, form url.Values, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestFirstRunLifecycle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "glyphux.db")
	h := boot(t, dbPath)

	// Pre-setup: health works, contract endpoints refuse, root redirects to wizard.
	if rec := get(t, h, "/healthz"); rec.Code != http.StatusOK {
		t.Fatalf("/healthz = %d", rec.Code)
	}
	if rec := get(t, h, "/api/v0/composition"); rec.Code != http.StatusConflict {
		t.Fatalf("pre-setup /api/v0/composition = %d, want 409", rec.Code)
	}
	if rec := get(t, h, "/"); rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != "/setup" {
		t.Fatalf("pre-setup / = %d → %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := get(t, h, "/setup"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Welcome to Glyphux") {
		t.Fatalf("/setup form: %d", rec.Code)
	}

	// Complete the wizard from localhost (no token required).
	form := url.Values{
		"site_name":      {"Phase Zero"},
		"admin_email":    {"admin@example.com"},
		"admin_password": {"strong password"},
		"database":       {"sqlite"},
	}
	if rec := postForm(t, h, "/setup", form, "127.0.0.1:9"); rec.Code != http.StatusOK {
		t.Fatalf("wizard submit = %d: %s", rec.Code, rec.Body.String())
	}

	// Post-setup: the route is permanently locked.
	if rec := get(t, h, "/setup"); rec.Code != http.StatusGone {
		t.Fatalf("post-setup GET /setup = %d, want 410", rec.Code)
	}
	if rec := postForm(t, h, "/setup", form, "127.0.0.1:9"); rec.Code != http.StatusGone {
		t.Fatalf("post-setup POST /setup = %d, want 410", rec.Code)
	}

	// The contract-driven domain API now serves the composition it wrote.
	rec := get(t, h, "/api/v0/content/ping")
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/v0/content/ping = %d", rec.Code)
	}
	var ping struct {
		ContractVersion string `json:"contract_version"`
		Site            string `json:"site"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ping); err != nil {
		t.Fatal(err)
	}
	if ping.Site != "Phase Zero" || ping.ContractVersion != "content-composition/v0" {
		t.Errorf("ping = %+v", ping)
	}

	// Restart: a fresh boot on the same database keeps state and the lock.
	h2 := boot(t, dbPath)
	if rec := get(t, h2, "/setup"); rec.Code != http.StatusGone {
		t.Fatalf("after restart GET /setup = %d, want 410", rec.Code)
	}
	if rec := get(t, h2, "/api/v0/composition"); rec.Code != http.StatusOK {
		t.Fatalf("after restart /api/v0/composition = %d", rec.Code)
	}
}

func TestWizardRequiresTokenForRemoteAccess(t *testing.T) {
	h := boot(t, filepath.Join(t.TempDir(), "glyphux.db"))

	form := url.Values{
		"site_name":      {"Intruder Site"},
		"admin_email":    {"evil@example.com"},
		"admin_password": {"evil password"},
	}
	// Remote submit without (or with a wrong) token must be rejected.
	rec := postForm(t, h, "/setup", form, "203.0.113.7:4444")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("remote submit without token = %d, want 422", rec.Code)
	}
	form.Set("setup_token", "wrong-token")
	rec = postForm(t, h, "/setup", form, "203.0.113.7:4444")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("remote submit with wrong token = %d, want 422", rec.Code)
	}
	// Setup must still be pending afterward.
	if rec := get(t, h, "/api/v0/composition"); rec.Code != http.StatusConflict {
		t.Fatalf("composition after failed remote setup = %d, want 409", rec.Code)
	}
}

func TestWizardRejectsPostgresInPhaseZero(t *testing.T) {
	h := boot(t, filepath.Join(t.TempDir(), "glyphux.db"))
	form := url.Values{
		"site_name":      {"PG Site"},
		"admin_email":    {"admin@example.com"},
		"admin_password": {"strong password"},
		"database":       {"postgres"},
	}
	rec := postForm(t, h, "/setup", form, "127.0.0.1:9")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("postgres choice = %d, want 422", rec.Code)
	}
}
