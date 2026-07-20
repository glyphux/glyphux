package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/setup"
	"github.com/glyphux/glyphux/pkg/contract"
)

// committer implements setup.Committer for the virgin-install boot path: a
// "sqlite" submission writes to the already-open bootstrap database — no
// different from the wizard's own default behavior. A "postgres"
// submission opens a fresh connection, migrates it, and — only once the
// admin account and composition are durably committed there — switches
// gateway to serve the application from it. That switch is what makes a
// Postgres choice take effect immediately, with no daemon restart (§6.2,
// the fix for finding #1).
type committer struct {
	dataDir          string
	migrations       []db.Migration
	gateway          *Gateway
	buildFullHandler BuildFullHandlerFunc

	fallbackDatabase     *db.DB
	fallbackCompositions *composition.Store
	fallbackIdentities   *identity.Service
	fallbackWizard       *setup.Wizard

	log *slog.Logger
}

func (c *committer) Commit(ctx context.Context, in setup.Input) error {
	driver := in.Driver
	if driver == "" {
		driver = "sqlite"
	}

	database, compositions, identities := c.fallbackDatabase, c.fallbackCompositions, c.fallbackIdentities
	dsnEnvVar := ""
	opened := false

	switch driver {
	case "sqlite":
		// Use the fallback bootstrap store as-is: it's already open and
		// already migrated, and for sqlite it always was the final store.
	case "postgres":
		pg, err := db.OpenPostgres(in.DSN, db.PoolConfig{})
		if err != nil {
			return fmt.Errorf("connect to postgres: %w", err)
		}
		if err := pg.Migrate(ctx, c.migrations); err != nil {
			pg.Close()
			return fmt.Errorf("migrate postgres: %w", err)
		}
		database, compositions, identities = pg, composition.NewStore(pg), identity.NewService(pg)
		dsnEnvVar = "GLYPHUX_DB_DSN"
		opened = true
	default:
		return fmt.Errorf("unknown database %q (want sqlite or postgres)", driver)
	}

	comp := &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: in.SiteName},
	}
	if err := database.WithTx(ctx, func(q db.Queryer) error {
		if err := identities.CreateAdminWith(ctx, q, in.AdminEmail, in.AdminPassword); err != nil {
			return fmt.Errorf("admin account: %w", err)
		}
		if err := compositions.SaveWith(ctx, q, comp); err != nil {
			return fmt.Errorf("composition: %w", err)
		}
		return nil
	}); err != nil {
		if opened {
			database.Close()
		}
		return err
	}

	full, err := c.buildFullHandler(database, compositions, identities, c.fallbackWizard)
	if err != nil {
		if opened {
			database.Close()
		}
		return fmt.Errorf("build application: %w", err)
	}

	// Config persistence is best-effort by design: the database write above
	// already committed durably, which is what matters. A failure here is
	// recovered on the next boot by Boot's self-heal path (it finds the
	// composition already exists), never by failing this request after the
	// admin account and composition have already been created.
	if err := Save(c.dataDir, &Config{Driver: driver, DSNEnvVar: dsnEnvVar}); err != nil {
		c.log.Error("failed to persist bootstrap config; will self-heal on next boot", "error", err)
	}

	c.gateway.Switch(full)
	return nil
}

var _ setup.Committer = (*committer)(nil)
