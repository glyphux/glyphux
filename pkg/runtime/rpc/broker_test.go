package rpc_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/glyphux/glyphux/pkg/runtime/rpc"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// fixtureBinPath is built once, by TestMain, into a temp dir shared by every
// test in this package — a real, separately-compiled subprocess binary, not
// an in-process fake pretending to be one.
var fixtureBinPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "glyphux-rpc-fixture-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	fixtureBinPath = filepath.Join(dir, "fixtureplugin")
	build := exec.Command("go", "build", "-o", fixtureBinPath, "./testdata/fixtureplugin")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("building fixtureplugin test binary: " + err.Error())
	}

	os.Exit(m.Run())
}

// networkManifest is a manifest declaring the "network" permission
// allowlisting allowed-host.example, so AllowsNetworkHost has something real
// to say yes to. hostAPIWithScopes further customizes the API axis.
func testManifest(apiScopes ...sdk.APIScope) sdk.Manifest {
	return sdk.Manifest{
		Name:    "fixture-plugin",
		Version: "1.0.0",
		Runtime: sdk.RuntimeRPC,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
		API: apiScopes,
		Permissions: []sdk.Permission{
			{Name: "network", Args: []string{"allowed-host.example"}},
		},
	}
}

func newHostAPI(t *testing.T, m sdk.Manifest) sdk.HostAPI {
	t.Helper()
	api, err := sdk.NewHostAPI(m, sdk.KernelDeps{KV: sdk.NewMemoryKVBackend(), Bus: sdk.NewEventBus()})
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	return api
}

func launch(t *testing.T, api sdk.HostAPI, env ...string) *rpc.Broker {
	t.Helper()
	b, err := rpc.Launch(rpc.Config{
		Command:      fixtureBinPath,
		Env:          env,
		HostAPI:      api,
		ReadyTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Launch: %v (stderr: %s)", err, b.Stderr())
	}
	t.Cleanup(func() { b.Close() })
	return b
}

// --- Behavior 1: a real subprocess is spawned, a real gRPC call crosses the
// process boundary and reaches a real host-backed Store(). ---

func TestBrokerStoreRoundTripCrossesRealProcessBoundary(t *testing.T) {
	api := newHostAPI(t, testManifest())
	b := launch(t, api)

	if got := b.State(); got != rpc.StateRunning {
		t.Fatalf("State() = %v, want StateRunning", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := b.Register(ctx, "store-roundtrip")
	if err != nil {
		t.Fatalf("Register(store-roundtrip): %v", err)
	}
	if !resp.GetOk() {
		t.Fatalf("fixture reported failure: %s (log: %v)", resp.GetError(), resp.GetLog())
	}

	// Prove it really went through the host's KV backend, not just the
	// fixture's own local state: the same key is now visible directly
	// through the sdk.HostAPI on the host side.
	value, ok, err := api.Store().Get(ctx, "greeting")
	if err != nil {
		t.Fatalf("host-side Store().Get: %v", err)
	}
	if !ok || string(value) != "hello-from-plugin" {
		t.Fatalf("host-side Store().Get = %q, %v; want \"hello-from-plugin\", true", value, ok)
	}
}

// --- Behavior 2: deny-by-default. A plugin whose manifest does not declare
// events:emit gets a real gRPC error calling Emit; one that does declare it
// succeeds. Same fixture binary, same RPC, different manifest. ---

func TestBrokerEmitDeniedWithoutDeclaredScope(t *testing.T) {
	api := newHostAPI(t, testManifest()) // no "events" api scope declared
	b := launch(t, api)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := b.Register(ctx, "emit")
	if err != nil {
		t.Fatalf("Register(emit) transport error: %v", err)
	}
	if resp.GetOk() {
		t.Fatalf("expected Emit to be denied (no events:emit declared), fixture reported success")
	}
	if resp.GetError() == "" {
		t.Fatalf("expected a non-empty denial error from the fixture")
	}
	t.Logf("denied as expected: %s", resp.GetError())
}

func TestBrokerEmitAllowedWithDeclaredScope(t *testing.T) {
	api := newHostAPI(t, testManifest(sdk.APIScope{Capability: "events", Scopes: []string{"emit"}}))
	b := launch(t, api)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := b.Register(ctx, "emit")
	if err != nil {
		t.Fatalf("Register(emit) transport error: %v", err)
	}
	if !resp.GetOk() {
		t.Fatalf("expected Emit to succeed with events:emit declared, got error: %s", resp.GetError())
	}
}

// --- Behavior 3: allowlisted network — the plugin subprocess can query the
// host's AllowsNetworkHost decision primitive (PRD §10.4), a real gRPC round
// trip mirroring the manifest's declared allowlist. ---

func TestBrokerAllowsNetworkHostReflectsManifestAllowlist(t *testing.T) {
	api := newHostAPI(t, testManifest())
	b := launch(t, api)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	respAllowed, err := b.Register(ctx, "network-check", "allowed-host.example")
	if err != nil {
		t.Fatalf("Register(network-check, allowed): %v", err)
	}
	if !respAllowed.GetOk() {
		t.Fatalf("expected allowed-host.example to be allowed, fixture said: %v", respAllowed.GetLog())
	}

	respDenied, err := b.Register(ctx, "network-check", "evil.example")
	if err != nil {
		t.Fatalf("Register(network-check, denied): %v", err)
	}
	if respDenied.GetOk() {
		t.Fatalf("expected evil.example to be denied, fixture reported allowed")
	}
}

// --- Behavior 4: crash isolation — a subprocess that exits before it ever
// signals readiness is detected by Launch without hanging or taking down
// the host (this test process). ---

func TestBrokerDetectsCrashBeforeReady(t *testing.T) {
	api := newHostAPI(t, testManifest())
	b, err := rpc.Launch(rpc.Config{
		Command:      fixtureBinPath,
		Env:          []string{"GLYPHUX_FIXTURE_CRASH_BEFORE_READY=1"},
		HostAPI:      api,
		ReadyTimeout: 5 * time.Second,
	})
	t.Cleanup(func() { b.Close() })

	if err == nil {
		t.Fatalf("expected Launch to report the subprocess never became ready")
	}
	if !errors.Is(err, rpc.ErrPluginNotReady) {
		t.Fatalf("Launch error = %v, want wrapping ErrPluginNotReady", err)
	}
	if got := b.State(); got != rpc.StateDead {
		t.Fatalf("State() = %v, want StateDead", got)
	}

	waitErr := b.Wait()
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		t.Fatalf("Wait() error = %v (%T), want *exec.ExitError", waitErr, waitErr)
	}
	if exitErr.ExitCode() != 1 {
		t.Fatalf("subprocess exit code = %d, want 1", exitErr.ExitCode())
	}
}

// --- Behavior 5: crash isolation mid-session — a subprocess that crashes
// while handling an RPC is detected (State transitions to Dead, Wait()
// returns a real exit error) without the host test process going down. ---

func TestBrokerDetectsCrashDuringCall(t *testing.T) {
	api := newHostAPI(t, testManifest())
	b := launch(t, api)

	if got := b.State(); got != rpc.StateRunning {
		t.Fatalf("State() before crash = %v, want StateRunning", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, callErr := b.Register(ctx, "crash")
	if callErr == nil {
		t.Fatalf("expected the crash-triggering Register call to return a transport error")
	}
	t.Logf("Register(crash) returned expected transport error: %v", callErr)

	select {
	case <-b.Done():
	case <-time.After(5 * time.Second):
		t.Fatalf("broker did not observe subprocess exit within 5s of crash")
	}

	if got := b.State(); got != rpc.StateDead {
		t.Fatalf("State() after crash = %v, want StateDead", got)
	}

	waitErr := b.Wait()
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		t.Fatalf("Wait() error = %v (%T), want *exec.ExitError", waitErr, waitErr)
	}
	if exitErr.ExitCode() != 1 {
		t.Fatalf("subprocess exit code = %d, want 1", exitErr.ExitCode())
	}

	// The crash of one plugin subprocess must not have taken down the host —
	// demonstrated trivially by this test process still running to make
	// these assertions at all, and confirmed positively: a brand new,
	// unrelated Broker can still be launched successfully right after.
	api2 := newHostAPI(t, testManifest())
	b2 := launch(t, api2)
	if got := b2.State(); got != rpc.StateRunning {
		t.Fatalf("second broker State() = %v, want StateRunning (host must survive the first plugin's crash)", got)
	}
}

