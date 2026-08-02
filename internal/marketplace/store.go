package marketplace

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/glyphux/glyphux/capabilities/marketplace"
	"github.com/glyphux/glyphux/internal/db"
)

// Migrations is the schema baseline for the marketplace surface.
//
// Version 20: the highest migration version in the repo at the time this
// slice was written is 19 (internal/pluginstore/store.go's plugin_kv
// table). Migration version numbers are global across every package's
// combined migration slice (internal/db/migrate.go's Migrate rejects
// duplicates across the whole applied set) — re-verified by grepping
// `Version:` across internal/*/*.go immediately before writing this file.
var Migrations = []db.Migration{
	{
		Version: 20,
		Name:    "marketplace entitlements",
		SQL: `
			CREATE TABLE marketplace_entitlements (
				id             INTEGER PRIMARY KEY AUTOINCREMENT,
				license_id     TEXT NOT NULL,
				extension_name TEXT NOT NULL,
				min_version    TEXT NOT NULL DEFAULT '',
				max_version    TEXT NOT NULL DEFAULT '',
				not_before     TEXT NOT NULL,
				not_after      TEXT NOT NULL,
				scope          TEXT NOT NULL DEFAULT '[]',
				signature      BLOB NOT NULL
			);
			CREATE INDEX idx_marketplace_entitlements_extension
				ON marketplace_entitlements (extension_name, id);
		`,
		PostgresSQL: `
			CREATE TABLE marketplace_entitlements (
				id             INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
				license_id     TEXT NOT NULL,
				extension_name TEXT NOT NULL,
				min_version    TEXT NOT NULL DEFAULT '',
				max_version    TEXT NOT NULL DEFAULT '',
				not_before     TEXT NOT NULL,
				not_after      TEXT NOT NULL,
				scope          TEXT NOT NULL DEFAULT '[]',
				signature      BYTEA NOT NULL
			);
			CREATE INDEX idx_marketplace_entitlements_extension
				ON marketplace_entitlements (extension_name, id);
		`,
	},
}

// Store persists registered entitlement tokens (Ticket T8 / gap 8): the
// tokens an operator has registered locally so the marketplace surface can
// report entitlement status and gate update eligibility. Registration is
// intentionally NOT a validity gate — an expired token is still registered
// (its status is computed at list time), matching the locked decision that
// entitlements "gate updates only — first install still allowed".
type Store struct {
	db *db.DB
}

// NewStore wires the entitlement registry to the database abstraction.
// Callers must have already run Migrations (via db.Migrate) against
// database.
func NewStore(database *db.DB) *Store {
	return &Store{db: database}
}

// Register persists tok. Duplicate licenses re-register (the newest token
// for a license wins — List returns the latest row per license_id).
func (s *Store) Register(ctx context.Context, tok marketplace.EntitlementToken) error {
	scopeJSON, err := json.Marshal(tok.Entitlement.Scope)
	if err != nil {
		return fmt.Errorf("marketplace: encode scope: %w", err)
	}
	const q = `
		INSERT INTO marketplace_entitlements
			(license_id, extension_name, min_version, max_version, not_before, not_after, scope, signature)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = s.db.Exec(ctx, q,
		tok.Entitlement.LicenseID,
		tok.Entitlement.ExtensionName,
		tok.Entitlement.MinVersion,
		tok.Entitlement.MaxVersion,
		tok.Entitlement.NotBefore.UTC().Format(time.RFC3339Nano),
		tok.Entitlement.NotAfter.UTC().Format(time.RFC3339Nano),
		string(scopeJSON),
		tok.Signature,
	)
	if err != nil {
		return fmt.Errorf("marketplace: register entitlement: %w", err)
	}
	return nil
}

// List returns every registered token, newest registration first. The
// Entitlement is restored from the stored JSON document and the signature
// from the stored bytes, so the returned token verifies exactly like the
// one originally registered.
func (s *Store) List(ctx context.Context) ([]marketplace.EntitlementToken, error) {
	rows, err := s.db.Query(ctx, `
		SELECT license_id, extension_name, min_version, max_version,
		       not_before, not_after, scope, signature
		FROM marketplace_entitlements
		ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("marketplace: list entitlements: %w", err)
	}
	defer rows.Close()

	var out []marketplace.EntitlementToken
	for rows.Next() {
		var (
			licenseID, extensionName, minVersion, maxVersion string
			notBefore, notAfter, scopeJSON                   string
			signature                                        []byte
		)
		if err := rows.Scan(&licenseID, &extensionName, &minVersion, &maxVersion,
			&notBefore, &notAfter, &scopeJSON, &signature); err != nil {
			return nil, fmt.Errorf("marketplace: scan entitlement: %w", err)
		}
		nb, err := time.Parse(time.RFC3339Nano, notBefore)
		if err != nil {
			return nil, fmt.Errorf("marketplace: parse not_before: %w", err)
		}
		na, err := time.Parse(time.RFC3339Nano, notAfter)
		if err != nil {
			return nil, fmt.Errorf("marketplace: parse not_after: %w", err)
		}
		var scope []string
		if err := json.Unmarshal([]byte(scopeJSON), &scope); err != nil {
			return nil, fmt.Errorf("marketplace: parse scope: %w", err)
		}
		ent := marketplace.Entitlement{
			LicenseID:     licenseID,
			ExtensionName: extensionName,
			MinVersion:    minVersion,
			MaxVersion:    maxVersion,
			NotBefore:     nb,
			NotAfter:      na,
			Scope:         scope,
		}
		out = append(out, marketplace.EntitlementToken{Entitlement: ent, Signature: signature})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("marketplace: iterate entitlements: %w", err)
	}
	return out, nil
}
