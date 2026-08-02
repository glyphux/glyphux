// Package pluginstore is the durable, SQL-backed implementation of
// pkg/sdk's KVBackend (the scoped per-plugin persistence of PRD §8.3 made
// restart-safe — gap 6 / Ticket T1, replacing the process-lifetime
// MemoryKVBackend as the real backend a running daemon uses). It sits
// entirely behind internal/db.Queryer and never touches database/sql
// directly (Communication Law, §5.2: nothing above the kernel's database
// adapter may see a raw driver type).
package pluginstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// Migrations is the schema baseline for plugin key-value persistence.
//
// Version 19: the highest migration version in the repo at the time this
// slice was written is 18 (internal/bundle/store.go's "plugin bundles"
// migration). Migration version numbers are global across every package's
// combined migration slice (internal/db/migrate.go's Migrate rejects
// duplicates across the whole applied set) — re-verified by grepping
// `Version:` across internal/*/*.go immediately before writing this file.
var Migrations = []db.Migration{
	{
		Version: 19,
		Name:    "plugin kv",
		SQL: `
			CREATE TABLE plugin_kv (
				plugin_name TEXT NOT NULL,
				key         TEXT NOT NULL,
				value       BLOB NOT NULL,
				updated_at  TEXT NOT NULL,
				PRIMARY KEY (plugin_name, key)
			);
		`,
	},
}

// Store is a SQL-backed sdk.KVBackend over internal/db.Queryer — durable
// across process restarts, unlike MemoryKVBackend. Namespacing is enforced
// by the schema itself: the composite PRIMARY KEY (plugin_name, key) means
// two plugins can store the same key without colliding, exactly the
// per-plugin isolation HostAPI.Store() promises (PRD §8.3). Values are
// plain BLOBs: no TTL, no per-key versioning, no size caps (scope decision,
// see docs/implementation/active/0037-*.md).
type Store struct {
	q db.Queryer
}

// NewStore wires the backend to a Queryer. The caller must have already run
// Migrations (via db.Migrate) against the same database — the same
// boot-order contract every other internal store (content, media, consent,
// audit) has.
func NewStore(q db.Queryer) *Store {
	return &Store{q: q}
}

// Get returns the value plugin stored under key, or (nil, false, nil) when
// plugin has never written key — the same not-found contract
// MemoryKVBackend.get has, plus an error path for real storage failures.
func (s *Store) Get(ctx context.Context, namespace, key string) ([]byte, bool, error) {
	var value []byte
	err := s.q.QueryRow(ctx,
		`SELECT value FROM plugin_kv WHERE plugin_name = ? AND key = ?`,
		namespace, key).Scan(&value)
	if errors.Is(err, db.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("pluginstore: get %q/%q: %w", namespace, key, err)
	}
	return value, true, nil
}

// Set upserts plugin's key to value — an existing (plugin, key) row is
// updated in place so the latest write wins (never a second row).
func (s *Store) Set(ctx context.Context, namespace, key string, value []byte) error {
	_, err := s.q.Exec(ctx,
		`INSERT INTO plugin_kv (plugin_name, key, value, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (plugin_name, key) DO UPDATE SET
			value = excluded.value, updated_at = excluded.updated_at`,
		namespace, key, value, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("pluginstore: set %q/%q: %w", namespace, key, err)
	}
	return nil
}

// Delete removes plugin's key. Deleting a key that was never written is
// not an error — the same no-op contract MemoryKVBackend.delete has.
func (s *Store) Delete(ctx context.Context, namespace, key string) error {
	if _, err := s.q.Exec(ctx,
		`DELETE FROM plugin_kv WHERE plugin_name = ? AND key = ?`,
		namespace, key); err != nil {
		return fmt.Errorf("pluginstore: delete %q/%q: %w", namespace, key, err)
	}
	return nil
}

// compile-time assertion that Store satisfies the pkg/sdk backend contract.
var _ sdk.KVBackend = (*Store)(nil)
