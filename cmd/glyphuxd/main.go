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
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/glyphux/glyphux/internal/api"
	"github.com/glyphux/glyphux/internal/bootstrap"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/config"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/graphql"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/media"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Schema baseline: idempotent, ordered, recorded (slice 0.3).
	migrations := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	migrations = append(migrations, content.Migrations...)
	migrations = append(migrations, media.Migrations...)

	boot, err := bootstrap.Boot(ctx, bootstrap.Options{
		DataDir:           cfg.DataDir,
		SQLitePath:        cfg.SQLitePath(),
		DatabaseExplicit:  databaseExplicit(cfg),
		Database:          cfg.Database,
		TrustProxyHeaders: cfg.TrustProxyHeaders,
		Migrations:        migrations,
		Log:               log,
		BuildFullHandler:  buildFullHandler(cfg, log),
	})
	if err != nil {
		return err
	}
	defer boot.Database.Close()

	srv := server.New(cfg.Addr, boot.Handler, log)
	if cfg.OpenBrowser {
		go func() {
			maybeOpenBrowser(true, "http://"+srv.Addr()+"/", openBrowser, log)
		}()
	}
	return srv.Run(ctx, cfg.ShutdownTimeout)
}

// databaseExplicit reports whether the operator explicitly chose a database
// driver via config file or GLYPHUX_DB_DRIVER, as opposed to relying on
// bootstrap's own decision (a persisted choice from a prior setup, or the
// wizard). It must key off the driver alone, not whether a DSN happens to
// be set: after a Postgres wizard setup, the operator is told to export
// only GLYPHUX_DB_DSN for the *persisted* driver choice to keep working —
// that DSN-only case must fall through to bootstrap's persisted-config
// path, which is what actually knows to combine the recorded driver with
// the env var. Treating DSN presence alone as "explicit" here would
// silently reopen the default SQLite store instead (Principle 10: explicit
// configuration wins, but an incomplete one must not masquerade as one).
func databaseExplicit(cfg config.Config) bool {
	return cfg.Database.Driver != config.Default().Database.Driver
}

// buildFullHandler assembles the daemon's complete route table (the API
// transport plus the wizard) once bootstrap has decided which database to
// serve from — the domain APIs bootstrap itself has no need to know about
// (content, media, sessions) are wired up here.
func buildFullHandler(cfg config.Config, log *slog.Logger) bootstrap.BuildFullHandlerFunc {
	return func(database *db.DB, compositions *composition.Store, identities *identity.Service, wizard *setup.Wizard) (http.Handler, error) {
		sessions := identity.NewSessions(database)
		contentAPI := content.NewAPI(compositions, content.NewStore(database))
		mediaAPI := media.NewAPI(media.NewStore(database), filepath.Join(cfg.DataDir, "media"))
		apiServer := api.New(compositions, contentAPI, mediaAPI, identities, sessions, log, api.TrustProxyHeaders(cfg.TrustProxyHeaders))
		// GraphQL (slice 1.12) is a second transport over the same domain
		// APIs the REST apiServer above was just built from — not a new
		// privileged path.
		graphqlResolver := graphql.NewResolver(compositions, contentAPI, mediaAPI, identities, sessions, log)
		graphqlHandler := graphql.NewHandler(graphqlResolver)
		return server.Handler(apiServer, wizard, graphqlHandler), nil
	}
}
