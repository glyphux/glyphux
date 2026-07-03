package db

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// Migration is one idempotent, ordered schema step. Versions are unique and
// applied in ascending order exactly once; applied versions are recorded in
// schema_migrations so re-running is a no-op.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Migrate applies all unapplied migrations in order.
func (d *DB) Migrate(ctx context.Context, migrations []Migration) error {
	if _, err := d.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
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
			if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
				return fmt.Errorf("apply migration %d (%s): %w", m.Version, m.Name, err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, name) VALUES (?, ?)`, m.Version, m.Name); err != nil {
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
