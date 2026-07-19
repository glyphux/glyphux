package consent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/glyphux/glyphux/internal/db"
)

// Migrations is the schema baseline for the install-time consent engine
// (PRD §10.2 mechanism #2).
//
// Version 14: the highest migration version in the repo at the time this
// slice was written is 13 (internal/identity/mfa.go's "mfa challenges"
// migration — itself bumped from a collision with internal/media/store.go's
// independently-numbered 8, per that file's own comment). Migration version
// numbers are global across every package's combined migration slice
// (internal/db/migrate.go's Migrate rejects duplicates across the whole
// applied set), so 14 is the next free integer above the current highest:
// identity (2, 4, 10, 11, 12, 13), content (3, 5), media (6, 8), composition
// (7). This has bitten the project twice before (0009/0010, and again inside
// 0009 itself with mfa.go's own 8->12 fix) — re-verified by grepping
// `Version:` across internal/*/*.go immediately before writing this file.
var Migrations = []db.Migration{
	{
		Version: 14,
		Name:    "consent decisions",
		SQL: `
			CREATE TABLE consent_decisions (
				id                    INTEGER PRIMARY KEY AUTOINCREMENT,
				plugin_name           TEXT NOT NULL,
				plugin_version        TEXT NOT NULL,
				fingerprint           TEXT NOT NULL,
				status                TEXT NOT NULL,
				requested_api         TEXT NOT NULL,
				requested_permissions TEXT NOT NULL,
				granted_api           TEXT NOT NULL,
				granted_permissions   TEXT NOT NULL,
				decided_by            INTEGER NOT NULL,
				decided_at            TEXT NOT NULL
			);
			CREATE INDEX idx_consent_decisions_lookup
				ON consent_decisions (plugin_name, plugin_version, fingerprint, id);
		`,
		PostgresSQL: `
			CREATE TABLE consent_decisions (
				id                    INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
				plugin_name           TEXT NOT NULL,
				plugin_version        TEXT NOT NULL,
				fingerprint           TEXT NOT NULL,
				status                TEXT NOT NULL,
				requested_api         TEXT NOT NULL,
				requested_permissions TEXT NOT NULL,
				granted_api           TEXT NOT NULL,
				granted_permissions   TEXT NOT NULL,
				decided_by            INTEGER NOT NULL,
				decided_at            TEXT NOT NULL
			);
			CREATE INDEX idx_consent_decisions_lookup
				ON consent_decisions (plugin_name, plugin_version, fingerprint, id);
		`,
	},
}

// ErrNotFound reports that no consent decision exists for the given lookup.
var ErrNotFound = errors.New("consent: no decision found")

// insert persists d and returns its assigned row ID.
func insert(ctx context.Context, database *db.DB, d Decision) (int64, error) {
	reqAPI, err := json.Marshal(d.RequestedAPI)
	if err != nil {
		return 0, fmt.Errorf("encode requested api: %w", err)
	}
	reqPerms, err := json.Marshal(d.RequestedPermissions)
	if err != nil {
		return 0, fmt.Errorf("encode requested permissions: %w", err)
	}
	grantedAPI, err := json.Marshal(d.GrantedAPI)
	if err != nil {
		return 0, fmt.Errorf("encode granted api: %w", err)
	}
	grantedPerms, err := json.Marshal(d.GrantedPermissions)
	if err != nil {
		return 0, fmt.Errorf("encode granted permissions: %w", err)
	}

	result, err := database.Exec(ctx,
		`INSERT INTO consent_decisions
			(plugin_name, plugin_version, fingerprint, status, requested_api, requested_permissions, granted_api, granted_permissions, decided_by, decided_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.PluginName, d.PluginVersion, string(d.Fingerprint), string(d.Status),
		string(reqAPI), string(reqPerms), string(grantedAPI), string(grantedPerms),
		d.DecidedBy, d.DecidedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return 0, fmt.Errorf("insert consent decision: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read inserted consent decision id: %w", err)
	}
	return id, nil
}

// latest returns the most recently decided consent decision for the exact
// (pluginName, pluginVersion, fingerprint) triple, or ErrNotFound if no
// decision has ever been recorded against that exact shape. Because
// fingerprint is part of the lookup key, a manifest whose API/Permissions
// axes changed (a new fingerprint) never matches an old decision — that is
// the re-consent-on-manifest-change mechanism (see consent.go's Fingerprint
// doc comment for how the hash is computed).
func latest(ctx context.Context, database *db.DB, pluginName, pluginVersion string, fingerprint Fingerprint) (Decision, error) {
	row := database.QueryRow(ctx,
		`SELECT id, plugin_name, plugin_version, fingerprint, status, requested_api, requested_permissions, granted_api, granted_permissions, decided_by, decided_at
		 FROM consent_decisions
		 WHERE plugin_name = ? AND plugin_version = ? AND fingerprint = ?
		 ORDER BY id DESC LIMIT 1`,
		pluginName, pluginVersion, string(fingerprint),
	)

	var (
		d                                          Decision
		fp, status, reqAPI, reqPerms               string
		grantedAPI, grantedPerms, decidedAtEncoded  string
	)
	err := row.Scan(&d.ID, &d.PluginName, &d.PluginVersion, &fp, &status,
		&reqAPI, &reqPerms, &grantedAPI, &grantedPerms, &d.DecidedBy, &decidedAtEncoded)
	if errors.Is(err, db.ErrNoRows) {
		return Decision{}, ErrNotFound
	}
	if err != nil {
		return Decision{}, fmt.Errorf("query consent decision: %w", err)
	}

	d.Fingerprint = Fingerprint(fp)
	d.Status = Status(status)
	if err := json.Unmarshal([]byte(reqAPI), &d.RequestedAPI); err != nil {
		return Decision{}, fmt.Errorf("decode requested api: %w", err)
	}
	if err := json.Unmarshal([]byte(reqPerms), &d.RequestedPermissions); err != nil {
		return Decision{}, fmt.Errorf("decode requested permissions: %w", err)
	}
	if err := json.Unmarshal([]byte(grantedAPI), &d.GrantedAPI); err != nil {
		return Decision{}, fmt.Errorf("decode granted api: %w", err)
	}
	if err := json.Unmarshal([]byte(grantedPerms), &d.GrantedPermissions); err != nil {
		return Decision{}, fmt.Errorf("decode granted permissions: %w", err)
	}
	d.DecidedAt, err = time.Parse(time.RFC3339Nano, decidedAtEncoded)
	if err != nil {
		return Decision{}, fmt.Errorf("decode decided_at: %w", err)
	}

	return d, nil
}
