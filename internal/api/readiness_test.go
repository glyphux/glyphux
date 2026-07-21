package api_test

// /healthz only proves the process is running (liveness). /readyz proves it
// can actually serve requests (readiness) — the two are different claims: a
// daemon whose database connection has dropped is alive but not ready, and
// an orchestrator that only checks /healthz will keep routing traffic to it.

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/api"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
)

func TestReadyzReportsOKWhenDatabaseIsReachable(t *testing.T) {
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(context.Background(), composition.Migrations); err != nil {
		t.Fatal(err)
	}

	h := readinessTestServer(t, d)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/readyz = %d, want 200", rec.Code)
	}
}

func TestReadyzReportsUnavailableWhenDatabaseIsUnreachable(t *testing.T) {
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(context.Background(), composition.Migrations); err != nil {
		t.Fatal(err)
	}

	h := readinessTestServer(t, d)
	d.Close() // simulate a dropped/unreachable database connection

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz with unreachable DB = %d, want 503", rec.Code)
	}
}

func readinessTestServer(t *testing.T, d *db.DB) http.Handler {
	t.Helper()
	comps := composition.NewStore(d)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := api.New(comps, content.NewAPI(comps, content.NewStore(d)), newTestMediaAPI(t, d), identity.NewService(d), identity.NewSessions(d), log)
	mux := http.NewServeMux()
	srv.Routes(mux)
	return mux
}
