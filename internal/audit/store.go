package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/glyphux/glyphux/internal/db"
)

// Migrations is the schema baseline for the audit subsystem (PRD §10.5:
// "every sensitive grant and every cross-boundary call is recorded").
//
// Version 15: the highest migration version in the repo at the time this
// slice was written is 14 (internal/consent/store.go's consent_decisions
// table). Migration version numbers are global across every package's
// combined migration slice (internal/db/migrate.go's Migrate rejects
// duplicates across the whole applied set) — re-verified by grepping
// `Version:` across internal/*/*.go immediately before writing this file.
var Migrations = []db.Migration{
	{
		Version: 15,
		Name:    "audit records",
		SQL: `
			CREATE TABLE audit_records (
				id           INTEGER PRIMARY KEY AUTOINCREMENT,
				plugin_name  TEXT NOT NULL,
				action       TEXT NOT NULL,
				allowed      INTEGER NOT NULL,
				detail       TEXT NOT NULL,
				occurred_at  TEXT NOT NULL
			);
			CREATE INDEX idx_audit_records_plugin ON audit_records (plugin_name, id);
		`,
		PostgresSQL: `
			CREATE TABLE audit_records (
				id           INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
				plugin_name  TEXT NOT NULL,
				action       TEXT NOT NULL,
				allowed      BOOLEAN NOT NULL,
				detail       TEXT NOT NULL,
				occurred_at  TEXT NOT NULL
			);
			CREATE INDEX idx_audit_records_plugin ON audit_records (plugin_name, id);
		`,
	},
}

// insert persists r (with OccurredAt already defaulted by the caller) and
// returns its assigned row ID.
func insert(ctx context.Context, database *db.DB, r Record) (int64, error) {
	allowed := 0
	if r.Allowed {
		allowed = 1
	}
	result, err := database.Exec(ctx,
		`INSERT INTO audit_records (plugin_name, action, allowed, detail, occurred_at)
		 VALUES (?, ?, ?, ?, ?)`,
		r.PluginName, r.Action, allowed, r.Detail, r.OccurredAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return 0, fmt.Errorf("insert audit record: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read inserted audit record id: %w", err)
	}
	return id, nil
}

// listByPlugin returns every record for pluginName in insertion order
// (oldest first).
func listByPlugin(ctx context.Context, database *db.DB, pluginName string) ([]Record, error) {
	rows, err := database.Query(ctx,
		`SELECT id, plugin_name, action, allowed, detail, occurred_at
		 FROM audit_records WHERE plugin_name = ? ORDER BY id ASC`,
		pluginName,
	)
	if err != nil {
		return nil, fmt.Errorf("query audit records: %w", err)
	}
	defer rows.Close()

	var records []Record
	for rows.Next() {
		var (
			r             Record
			allowed       int
			occurredAtEnc string
		)
		if err := rows.Scan(&r.ID, &r.PluginName, &r.Action, &allowed, &r.Detail, &occurredAtEnc); err != nil {
			return nil, fmt.Errorf("scan audit record: %w", err)
		}
		r.Allowed = allowed != 0
		r.OccurredAt, err = time.Parse(time.RFC3339Nano, occurredAtEnc)
		if err != nil {
			return nil, fmt.Errorf("decode occurred_at: %w", err)
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit records: %w", err)
	}
	return records, nil
}
