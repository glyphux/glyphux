package pluginstore_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glyphux/glyphux/internal/audit"
	"github.com/glyphux/glyphux/internal/bundle"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/consent"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/internal/pluginstore"
	"github.com/glyphux/glyphux/internal/preset"
)

// openDB opens a fresh SQLite database at path (creating it) and applies
// the plugin KV migration — the same boot shape a real glyphuxd uses
// (db.OpenSQLite + db.Migrate), no mocks.
func openDB(t *testing.T, path string) *db.DB {
	t.Helper()
	d, err := db.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := d.Migrate(context.Background(), pluginstore.Migrations); err != nil {
		t.Fatalf("migrate pluginstore: %v", err)
	}
	return d
}

func TestKVSurvivesCloseAndReopenOfDatabase(t *testing.T) {
	// The gap-6 proof itself: a plugin's Store().Set must outlive the
	// process. Real SQLite file, open -> write -> close -> reopen -> read.
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "kv.db")

	d1 := openDB(t, dbPath)
	store1 := pluginstore.NewStore(d1)
	if err := store1.Set(ctx, "forms", "token", []byte("v")); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := d1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	d2 := openDB(t, dbPath) // same file, fresh handle — a "restart"
	defer d2.Close()
	store2 := pluginstore.NewStore(d2)
	got, ok, err := store2.Get(ctx, "forms", "token")
	if err != nil {
		t.Fatalf("get after reopen: %v", err)
	}
	if !ok || string(got) != "v" {
		t.Fatalf("get after reopen = (%q, %v), want (\"v\", true)", got, ok)
	}
}

func TestKVIsNamespacedPerPlugin(t *testing.T) {
	// Two plugins writing the same key must each read only their own
	// value — the namespacing contract HostAPI.Store() promises (PRD
	// §8.3), made durable by the (plugin_name, key) primary key.
	ctx := context.Background()
	store := pluginstore.NewStore(openDB(t, filepath.Join(t.TempDir(), "kv.db")))

	if err := store.Set(ctx, "plugin-one", "key", []byte("value-one")); err != nil {
		t.Fatalf("set plugin-one: %v", err)
	}
	if err := store.Set(ctx, "plugin-two", "key", []byte("value-two")); err != nil {
		t.Fatalf("set plugin-two: %v", err)
	}

	got, ok, err := store.Get(ctx, "plugin-one", "key")
	if err != nil {
		t.Fatalf("get plugin-one: %v", err)
	}
	if !ok || string(got) != "value-one" {
		t.Fatalf("plugin-one get = (%q, %v), want (\"value-one\", true)", got, ok)
	}

	got, ok, err = store.Get(ctx, "plugin-two", "key")
	if err != nil {
		t.Fatalf("get plugin-two: %v", err)
	}
	if !ok || string(got) != "value-two" {
		t.Fatalf("plugin-two get = (%q, %v), want (\"value-two\", true)", got, ok)
	}

	// A plugin that never wrote this key sees nothing.
	if _, ok, err := store.Get(ctx, "plugin-three", "key"); err != nil || ok {
		t.Fatalf("plugin-three get = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
}

func TestKVDeleteRemovesAValue(t *testing.T) {
	ctx := context.Background()
	store := pluginstore.NewStore(openDB(t, filepath.Join(t.TempDir(), "kv.db")))

	if err := store.Set(ctx, "forms", "key", []byte("value")); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := store.Delete(ctx, "forms", "key"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, err := store.Get(ctx, "forms", "key"); err != nil || ok {
		t.Fatalf("get after delete = (ok=%v, err=%v), want (false, nil)", ok, err)
	}

	// Deleting a key that was never written is not an error — same
	// no-op contract MemoryKVBackend.delete has.
	if err := store.Delete(ctx, "forms", "never-written"); err != nil {
		t.Fatalf("delete missing key: %v", err)
	}
}

func TestKVSetUpsertsLatestWriteWins(t *testing.T) {
	ctx := context.Background()
	d := openDB(t, filepath.Join(t.TempDir(), "kv.db"))
	store := pluginstore.NewStore(d)

	if err := store.Set(ctx, "forms", "greeting", []byte("hello")); err != nil {
		t.Fatalf("set v1: %v", err)
	}
	if err := store.Set(ctx, "forms", "greeting", []byte("goodbye")); err != nil {
		t.Fatalf("set v2: %v", err)
	}
	got, ok, err := store.Get(ctx, "forms", "greeting")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok || string(got) != "goodbye" {
		t.Fatalf("get = (%q, %v), want (\"goodbye\", true) — second Set must win", got, ok)
	}

	// And only ONE row exists for the (plugin, key) pair — the upsert
	// must have updated in place, not appended.
	var n int
	if err := d.QueryRow(ctx,
		`SELECT COUNT(*) FROM plugin_kv WHERE plugin_name = ? AND key = ?`,
		"forms", "greeting").Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if n != 1 {
		t.Fatalf("rows for (forms, greeting) = %d, want 1 (upsert, not append)", n)
	}
}

// TestPluginKVMigrationAppliesInFullDaemonList mirrors cmd/glyphuxd's
// combined migration list (composition, identity, content, media, layout,
// preset, bundle) PLUS the consent/audit slices T4 will add and this
// package's own — the strongest available stand-in for the daemon-level
// "full list applies cleanly" proof until T9's central registry guard test
// lands: db.Migrate itself rejects any duplicate Version, so this test
// failing names a collision with migration 19.
func TestPluginKVMigrationAppliesInFullDaemonList(t *testing.T) {
	ctx := context.Background()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "full.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer d.Close()

	migrations := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	migrations = append(migrations, content.Migrations...)
	migrations = append(migrations, media.Migrations...)
	migrations = append(migrations, layout.Migrations...)
	migrations = append(migrations, preset.Migrations...)
	migrations = append(migrations, bundle.Migrations...)
	migrations = append(migrations, consent.Migrations...)
	migrations = append(migrations, audit.Migrations...)
	migrations = append(migrations, pluginstore.Migrations...)

	if err := d.Migrate(ctx, migrations); err != nil {
		t.Fatalf("migrate full daemon list: %v", err)
	}

	// The plugin_kv table exists with the composite primary key —
	// (plugin_name, key) is what makes the namespacing durable.
	var ddl string
	if err := d.QueryRow(ctx,
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'plugin_kv'`).Scan(&ddl); err != nil {
		t.Fatalf("plugin_kv missing from schema after full migrate: %v", err)
	}
	lower := strings.ToLower(ddl)
	if !strings.Contains(lower, "primary key") || !strings.Contains(lower, "plugin_name") || !strings.Contains(lower, "key") {
		t.Fatalf("plugin_kv DDL lacks composite primary key:\n%s", ddl)
	}
}
