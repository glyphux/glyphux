package rpc

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/pluginstore"
	"github.com/glyphux/glyphux/pkg/runtime/rpc/rpcpb"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// TestHostServerStoreRoundTripsThroughPluginstore mirrors the existing
// memory-backed broker KV test (TestBrokerStoreRoundTripCrossesRealProcessBoundary)
// at the hostServer layer, but with the durable SQL backend (gap 6 /
// Ticket T1): the RPC StoreSet/StoreGet/StoreDelete handlers must
// round-trip through a real pluginstore-backed HostAPI.Store() — and the
// value must survive a close/reopen of the SQLite file, exactly like the
// daemon-restart acceptance criterion.
func TestHostServerStoreRoundTripsThroughPluginstore(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "kv.db")

	m := sdk.Manifest{
		Name:    "fixture-plugin",
		Version: "1.0.0",
		Runtime: sdk.RuntimeRPC,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
	}

	// First "boot": migrate, back the HostAPI with pluginstore, round-trip
	// through the unexported hostServer exactly as Broker.Launch wires it
	// (rpcpb.RegisterHostAPIServer(hostSrv, newHostServer(cfg.HostAPI))).
	d1, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := d1.Migrate(ctx, pluginstore.Migrations); err != nil {
		t.Fatalf("migrate pluginstore: %v", err)
	}
	api1, err := sdk.NewHostAPI(m, sdk.KernelDeps{KV: pluginstore.NewStore(d1), Bus: sdk.NewEventBus()})
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	srv1 := newHostServer(api1)

	if _, err := srv1.StoreSet(ctx, &rpcpb.StoreSetRequest{Key: "token", Value: []byte("v")}); err != nil {
		t.Fatalf("StoreSet: %v", err)
	}
	got, err := srv1.StoreGet(ctx, &rpcpb.StoreGetRequest{Key: "token"})
	if err != nil {
		t.Fatalf("StoreGet: %v", err)
	}
	if !got.GetOk() || string(got.GetValue()) != "v" {
		t.Fatalf("StoreGet = (%q, ok=%v), want (\"v\", true)", got.GetValue(), got.GetOk())
	}
	if err := d1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// "Restart": reopen the same file, fresh hostServer over the same DB
	// backend — the RPC path must still see the value.
	d2, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	defer d2.Close()
	api2, err := sdk.NewHostAPI(m, sdk.KernelDeps{KV: pluginstore.NewStore(d2), Bus: sdk.NewEventBus()})
	if err != nil {
		t.Fatalf("NewHostAPI (reopen): %v", err)
	}
	srv2 := newHostServer(api2)

	got, err = srv2.StoreGet(ctx, &rpcpb.StoreGetRequest{Key: "token"})
	if err != nil {
		t.Fatalf("StoreGet after reopen: %v", err)
	}
	if !got.GetOk() || string(got.GetValue()) != "v" {
		t.Fatalf("StoreGet after reopen = (%q, ok=%v), want (\"v\", true) — value must survive a daemon restart", got.GetValue(), got.GetOk())
	}

	if _, err := srv2.StoreDelete(ctx, &rpcpb.StoreDeleteRequest{Key: "token"}); err != nil {
		t.Fatalf("StoreDelete: %v", err)
	}
	got, err = srv2.StoreGet(ctx, &rpcpb.StoreGetRequest{Key: "token"})
	if err != nil {
		t.Fatalf("StoreGet after delete: %v", err)
	}
	if got.GetOk() {
		t.Fatalf("StoreGet after StoreDelete reported ok=true, want false")
	}
}
