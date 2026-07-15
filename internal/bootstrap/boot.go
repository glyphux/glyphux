package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/config"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/setup"
)

// BuildFullHandlerFunc assembles the daemon's complete route table (API
// transport + wizard) once Boot knows which database to serve requests
// from. The caller (cmd/glyphuxd) owns this because it alone knows the full
// set of domain APIs (content, media, sessions, ...) bootstrap doesn't need
// to know about for its own job of deciding *which* database to use.
type BuildFullHandlerFunc func(database *db.DB, compositions *composition.Store, identities *identity.Service, wizard *setup.Wizard) (http.Handler, error)

// Options configures Boot.
type Options struct {
	DataDir    string
	SQLitePath string

	// DatabaseExplicit is true when the operator set the database driver via
	// GLYPHUX_DB_DRIVER (or a config file) rather than relying on defaults.
	// Explicit configuration always wins over a persisted bootstrap choice
	// or a wizard submission (Principle 10: configuration over magic) — it
	// is opened directly, exactly as every Phase-0 release before this one.
	DatabaseExplicit bool
	Database         config.DatabaseConfig

	TrustProxyHeaders bool
	Migrations        []db.Migration
	Log               *slog.Logger
	BuildFullHandler  BuildFullHandlerFunc
}

// Result is what Boot hands back to the caller for real network serving.
type Result struct {
	// Handler is the daemon's full route table. In the common case it is
	// ready immediately. On a virgin install it starts as a wizard-only
	// gateway and switches in-process, with no restart, once the wizard's
	// database choice is committed (§6.2).
	Handler http.Handler
	// Database is the database opened for this boot. The caller owns its
	// lifetime (defer Close()) — note that a virgin install choosing
	// Postgres opens a *second*, later database; that one is owned by the
	// committer and is not exposed here, since the caller never held it.
	Database *db.DB
}

// Boot resolves which database the daemon should serve from and returns the
// handler to serve. Three cases, in priority order:
//
//  1. The operator explicitly configured the driver (env or config file):
//     open exactly that, unconditionally — today's Phase-0 behavior,
//     unchanged.
//  2. A previous setup already completed and persisted its choice: open
//     that (resolving a Postgres DSN from the recorded environment
//     variable — never from disk, since bootstrap config never carries a
//     secret).
//  3. Neither: a virgin install. Open the default SQLite store and serve
//     the wizard from it. If the wizard's own submission picks Postgres,
//     the committer opens a fresh connection, migrates it, commits the
//     admin account and composition atomically, and switches the returned
//     handler over to it — no restart required.
func Boot(ctx context.Context, opts Options) (*Result, error) {
	if opts.DatabaseExplicit {
		database, err := openConfigured(opts.Database, opts.SQLitePath)
		if err != nil {
			return nil, err
		}
		return finishBoot(ctx, opts, database, "", "")
	}

	cfg, found, err := Load(opts.DataDir)
	if err != nil {
		return nil, fmt.Errorf("load bootstrap config: %w", err)
	}
	if found {
		dc := config.DatabaseConfig{Driver: cfg.Driver}
		if cfg.Driver == "postgres" {
			dsn := os.Getenv(cfg.DSNEnvVar)
			if dsn == "" {
				return nil, fmt.Errorf(
					"%s is required: this instance was previously configured for Postgres (see %s)",
					cfg.DSNEnvVar, configPath(opts.DataDir))
			}
			dc.DSN = dsn
		}
		database, err := openConfigured(dc, opts.SQLitePath)
		if err != nil {
			return nil, err
		}
		return finishBoot(ctx, opts, database, cfg.Driver, cfg.DSNEnvVar)
	}

	// Virgin install.
	database, err := db.OpenSQLite(opts.SQLitePath)
	if err != nil {
		return nil, err
	}
	if err := database.Migrate(ctx, opts.Migrations); err != nil {
		database.Close()
		return nil, err
	}

	compositions := composition.NewStore(database)
	identities := identity.NewService(database)
	wizard, err := setup.New(ctx, compositions, identities, database, opts.Log, opts.TrustProxyHeaders)
	if err != nil {
		database.Close()
		return nil, err
	}

	if wizard.Complete() {
		// A previous sqlite setup already succeeded but no bootstrap config
		// was found — the file write must have failed or been lost. Self-heal
		// rather than serving the wizard again (§17: recovery must be
		// idempotent, never a duplicate-admin error).
		if err := Save(opts.DataDir, &Config{Driver: "sqlite"}); err != nil {
			opts.Log.Error("failed to persist bootstrap config", "error", err)
		}
		full, err := opts.BuildFullHandler(database, compositions, identities, wizard)
		if err != nil {
			database.Close()
			return nil, err
		}
		return &Result{Handler: full, Database: database}, nil
	}

	full, err := opts.BuildFullHandler(database, compositions, identities, wizard)
	if err != nil {
		database.Close()
		return nil, err
	}
	gateway := NewGateway(full)
	wizard.SetCommitter(&committer{
		dataDir:              opts.DataDir,
		migrations:           opts.Migrations,
		gateway:              gateway,
		buildFullHandler:     opts.BuildFullHandler,
		fallbackDatabase:     database,
		fallbackCompositions: compositions,
		fallbackIdentities:   identities,
		fallbackWizard:       wizard,
		log:                  opts.Log,
	})
	return &Result{Handler: gateway, Database: database}, nil
}

func finishBoot(ctx context.Context, opts Options, database *db.DB, driver, dsnEnvVar string) (*Result, error) {
	if err := database.Migrate(ctx, opts.Migrations); err != nil {
		database.Close()
		return nil, err
	}
	compositions := composition.NewStore(database)
	identities := identity.NewService(database)
	wizard, err := setup.New(ctx, compositions, identities, database, opts.Log, opts.TrustProxyHeaders)
	if err != nil {
		database.Close()
		return nil, err
	}
	if driver != "" && wizard.Complete() {
		// Keep the persisted config in sync (also self-heals a missing file
		// for the persisted-config case).
		if err := Save(opts.DataDir, &Config{Driver: driver, DSNEnvVar: dsnEnvVar}); err != nil {
			opts.Log.Error("failed to persist bootstrap config", "error", err)
		}
	}
	full, err := opts.BuildFullHandler(database, compositions, identities, wizard)
	if err != nil {
		database.Close()
		return nil, err
	}
	return &Result{Handler: full, Database: database}, nil
}

func openConfigured(dc config.DatabaseConfig, sqlitePath string) (*db.DB, error) {
	if dc.Driver == "postgres" {
		return db.OpenPostgres(dc.DSN, db.PoolConfig{
			MaxOpenConns:    dc.MaxOpenConns,
			MaxIdleConns:    dc.MaxIdleConns,
			ConnMaxLifetime: dc.ConnMaxLifetime,
		})
	}
	return db.OpenSQLite(sqlitePath)
}
