package db

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// Migration is one idempotent, ordered schema step. Versions are unique and
// applied in ascending order exactly once; applied versions are recorded in
// schema_migrations so re-running is a no-op.
//
// SQL is the SQLite-dialect statement, used by default. PostgresSQL is an
// optional dialect-specific override for statements that aren't portable
// (e.g. AUTOINCREMENT vs. GENERATED ALWAYS AS IDENTITY) — most migrations
// need no override, since plain CREATE TABLE/INDEX/ALTER TABLE ADD COLUMN
// with TEXT/INTEGER columns runs unchanged on both.
type Migration struct {
	Version     int
	Name        string
	SQL         string
	PostgresSQL string
}

// sql returns the statement for this migration on driver.
func (m Migration) sql(driver string) string {
	if driver == "postgres" && m.PostgresSQL != "" {
		return m.PostgresSQL
	}
	return m.SQL
}

// Migrate applies all unapplied migrations in order.
func (d *DB) Migrate(ctx context.Context, migrations []Migration) error {
	if _, err := d.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	sorted := make([]Migration, len(migrations))
	copy(sorted, migrations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version < sorted[j].Version })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Version == sorted[i-1].Version {
			return fmt.Errorf("duplicate migration version %d", sorted[i].Version)
		}
	}

	for _, m := range sorted {
		var applied int
		err := d.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.Version).Scan(&applied)
		if err != nil {
			return fmt.Errorf("check migration %d: %w", m.Version, err)
		}
		if applied > 0 {
			continue
		}
		err = d.Tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, m.sql(d.driver)); err != nil {
				return fmt.Errorf("apply migration %d (%s): %w", m.Version, m.Name, err)
			}
			if _, err := tx.ExecContext(ctx, d.rebind(`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`),
				m.Version, m.Name, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
				return fmt.Errorf("record migration %d: %w", m.Version, err)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
