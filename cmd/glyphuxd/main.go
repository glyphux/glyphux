// Command glyphuxd is the Glyphux server daemon — the primary entrypoint
// (§5.3). It boots the kernel, runs migrations, serves the first-run web
// wizard on first boot, then the running platform. Single static binary,
// embedded SQLite, no required cloud (Principle 9).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/glyphux/glyphux/internal/api"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/config"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/server"
	"github.com/glyphux/glyphux/internal/setup"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "glyphuxd:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to JSON config file (optional)")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	if cfg.Database.Driver != "sqlite" {
		return fmt.Errorf("only the sqlite driver is available in this release (got %q)", cfg.Database.Driver)
	}
	database, err := db.OpenSQLite(cfg.SQLitePath())
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Schema baseline: idempotent, ordered, recorded (slice 0.3).
	migrations := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	migrations = append(migrations, content.Migrations...)
	if err := database.Migrate(ctx, migrations); err != nil {
		return err
	}

	// Kernel + domain APIs.
	compositions := composition.NewStore(database)
	identities := identity.NewService(database)
	sessions := identity.NewSessions(database)
	contentAPI := content.NewAPI(compositions, content.NewStore(database))

	// Clients of the contract: the API transport and the first-run wizard.
	wizard, err := setup.New(ctx, compositions, identities, log)
	if err != nil {
		return err
	}
	apiServer := api.New(compositions, contentAPI, identities, sessions, log)

	srv := server.New(cfg.Addr, apiServer, wizard, log)
	return srv.Run(ctx, cfg.ShutdownTimeout)
}
