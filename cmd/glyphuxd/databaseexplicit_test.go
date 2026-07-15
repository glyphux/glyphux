package main

// Caught by an end-to-end smoke test against the real binary: after a
// Postgres wizard setup, restarting with only GLYPHUX_DB_DSN set (as the
// operator is told to do — never GLYPHUX_DB_DRIVER, which was never asked
// for) must fall through to bootstrap's persisted-config path, not be
// treated as an explicit override that silently reopens SQLite.

import (
	"testing"

	"github.com/glyphux/glyphux/internal/config"
)

func TestDatabaseExplicitFalseForDefaultDriverEvenWithDSNSet(t *testing.T) {
	cfg := config.Default()
	cfg.Database.DSN = "postgres://user:pass@host:5432/db" // e.g. GLYPHUX_DB_DSN alone, per the done-page instruction
	if databaseExplicit(cfg) {
		t.Fatal("databaseExplicit = true with only a DSN set; must be false so the persisted driver choice is used")
	}
}

func TestDatabaseExplicitTrueWhenDriverIsSet(t *testing.T) {
	cfg := config.Default()
	cfg.Database.Driver = "postgres"
	cfg.Database.DSN = "postgres://user:pass@host:5432/db"
	if !databaseExplicit(cfg) {
		t.Fatal("databaseExplicit = false with an explicit non-default driver set; must be true")
	}
}

func TestDatabaseExplicitFalseForPureDefaults(t *testing.T) {
	if databaseExplicit(config.Default()) {
		t.Fatal("databaseExplicit = true for untouched defaults")
	}
}
