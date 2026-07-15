package server_test

// A server with no read/write/idle timeouts lets a slow or hanging client
// hold a connection open indefinitely, exhausting file descriptors under
// load (§17 production readiness). New must configure all four timeouts,
// not just ReadHeaderTimeout.

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
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

func TestNewConfiguresProductionTimeouts(t *testing.T) {
	ctx := context.Background()
	database, err := db.OpenSQLite(filepath.Join(t.TempDir(), "glyphux.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

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
	apiServer := api.New(compositions, content.NewAPI(compositions, content.NewStore(database)), mediaAPI, identities, sessions, log)

	srv := server.New(":0", server.Handler(apiServer, wizard), log)

	got := srv.Timeouts()
	want := server.Timeouts{
		Read:       server.DefaultReadTimeout,
		Write:      server.DefaultWriteTimeout,
		Idle:       server.DefaultIdleTimeout,
		ReadHeader: server.DefaultReadHeaderTimeout,
	}
	if got != want {
		t.Errorf("Timeouts() = %+v, want %+v", got, want)
	}
	if got.Read == 0 || got.Write == 0 || got.Idle == 0 {
		t.Errorf("Timeouts() left a timeout unset: %+v", got)
	}
}
