package wasm_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/pluginstore"
	"github.com/glyphux/glyphux/pkg/runtime/wasm"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// openKVDB opens a fresh SQLite database at path with the plugin_kv
// migration applied — the durable KernelDeps.KV a running daemon hands
// every loaded plugin (gap 6 / Ticket T1), no mocks.
func openKVDB(t *testing.T, path string) *db.DB {
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

// dbBackedHostAPI returns a real sdk.HostAPI (not a fake) whose Store() is
// backed by a SQL-backed pluginstore.Store over d, instead of the
// process-lifetime MemoryKVBackend every other test in this package uses.
func dbBackedHostAPI(t *testing.T, name string, d *db.DB) sdk.HostAPI {
	t.Helper()
	host, err := sdk.NewHostAPI(testManifest(name), sdk.KernelDeps{
		KV:  pluginstore.NewStore(d),
		Bus: sdk.NewEventBus(),
	})
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	return host
}

func TestKVGuestRoundTripsThroughDBBackend(t *testing.T) {
	// The committed kv_guest.wasm fixture, run against a host whose Store()
	// is the SQL-backed backend: the guest's kv_set/kv_get/kv_delete host
	// calls must reach real SQLite through the unchanged host-func bridge.
	ctx := context.Background()
	d := openKVDB(t, filepath.Join(t.TempDir(), "kv.db"))
	defer d.Close()
	host := dbBackedHostAPI(t, "kv-plugin", d)

	rt := wasm.New(ctx, wasm.AlwaysConsent{})
	defer rt.Close(ctx)

	inst, err := rt.Load(ctx, testManifest("kv-plugin"), host, readFixture(t, "kv_guest.wasm"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer inst.Close(ctx)

	res, err := inst.Call(ctx, "run_kv_roundtrip")
	if err != nil {
		t.Fatalf("Call run_kv_roundtrip: %v", err)
	}
	if got := int32(res[0]); got != 1 {
		t.Fatalf("run_kv_roundtrip returned %d, want 1 (see testdata/kv_guest.rs for failure code meanings)", got)
	}

	// Prove the round trip really landed in the SQL table, not just the
	// guest's own state: the guest's own final delete step must have
	// removed the row from plugin_kv.
	var n int
	if err := d.QueryRow(ctx,
		`SELECT COUNT(*) FROM plugin_kv WHERE plugin_name = ? AND key = ?`,
		"kv-plugin", "greeting").Scan(&n); err != nil {
		t.Fatalf("count plugin_kv rows: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected the guest's delete to have removed the plugin_kv row, but %d rows remain", n)
	}
}

func TestKVGuestValueSurvivesReloadWithDBBackend(t *testing.T) {
	// The gap-6 end-to-end proof through the real host bridge: a wasm
	// guest writes via kv_set; the instance and the database are closed; a
	// fresh instance is loaded against the same SQLite file; the guest's
	// kv_get returns the same value. testdata/kv_persist_guest.wasm
	// (persist_set/persist_get) is the committed fixture whose write half
	// leaves the key in place — unlike kv_guest.wasm's run_kv_roundtrip,
	// which deletes what it reads.
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "kv.db")

	// Phase A — first "boot": the guest writes through the host funcs into
	// SQLite and reads its own value back within the same boot.
	d1 := openKVDB(t, dbPath)
	host1 := dbBackedHostAPI(t, "kv-persist-plugin", d1)
	rt1 := wasm.New(ctx, wasm.AlwaysConsent{})
	inst1, err := rt1.Load(ctx, testManifest("kv-persist-plugin"), host1, readFixture(t, "kv_persist_guest.wasm"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if res, err := inst1.Call(ctx, "persist_set"); err != nil {
		t.Fatalf("Call persist_set: %v", err)
	} else if got := int32(res[0]); got != 0 {
		t.Fatalf("persist_set returned %d, want 0", got)
	}
	if res, err := inst1.Call(ctx, "persist_get"); err != nil {
		t.Fatalf("Call persist_get: %v", err)
	} else if got := int32(res[0]); got != 1 {
		t.Fatalf("persist_get (same boot) returned %d, want 1 (see testdata/kv_persist_guest.rs)", got)
	}
	if err := inst1.Close(ctx); err != nil {
		t.Fatalf("close instance: %v", err)
	}
	if err := rt1.Close(ctx); err != nil {
		t.Fatalf("close runtime: %v", err)
	}
	if err := d1.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	// Phase B — "restart": reopen the same file, fresh Runtime and fresh
	// HostAPI over the same DB backend, reload the guest. persist_get
	// returns -2 (not found) if the value did not survive the restart.
	d2 := openKVDB(t, dbPath)
	defer d2.Close()
	host2 := dbBackedHostAPI(t, "kv-persist-plugin", d2)
	rt2 := wasm.New(ctx, wasm.AlwaysConsent{})
	defer rt2.Close(ctx)
	inst2, err := rt2.Load(ctx, testManifest("kv-persist-plugin"), host2, readFixture(t, "kv_persist_guest.wasm"))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	defer inst2.Close(ctx)

	res, err := inst2.Call(ctx, "persist_get")
	if err != nil {
		t.Fatalf("Call persist_get after reload: %v", err)
	}
	if got := int32(res[0]); got != 1 {
		t.Fatalf("persist_get after reload returned %d, want 1 — the guest's value must survive a daemon restart (see testdata/kv_persist_guest.rs: -2 = not found)", got)
	}
}
