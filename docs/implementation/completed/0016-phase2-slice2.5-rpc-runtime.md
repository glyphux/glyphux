# Implementation: Phase 2 slice 2.5 — RPC runtime (Tier C)

## Goal

PRD §8.1 names Tier C as the "large, few, comparatively trusted, system-level
plugins" tier (commerce, booking, CRM, payments, analytics, AI integrations,
membership, email marketing) — out-of-process, own dependencies, outbound
network, long-lived state, own datastore, process isolation as a security
feature. The roadmap entry for this slice (§14) is explicit: "gRPC broker,
process supervision, crash isolation, allowlisted network." This slice
builds that: a `Broker` that launches a plugin as a real OS subprocess,
exposes a manifest-gated `pkg/sdk.HostAPI` to it over a real gRPC
connection, detects subprocess crash/exit, and wires `AllowsNetworkHost` in
as a callable decision primitive — proven against a genuine, separately
compiled subprocess binary, not a mock.

## Owning Contexts

- (no `CONTEXT.md` exists yet in this repo, per slice 2.6's precedent) — core
  kernel work, entirely new package `pkg/runtime/rpc`, no modification to
  `pkg/sdk`.

## Status

Complete for this slice's scope. `pkg/sdk` untouched (only imported).
`pkg/runtime/wasm` untouched (parallel slice 2.4's territory). Merged into
`dev`; independently re-verified via full repo build/vet/test -race after
reconciling with the parallel 2.4/2.7 slices (a clean merge — no file
overlap with either).

## Current Decisions

- **`google.golang.org/grpc` + `google.golang.org/protobuf` directly,
  not `hashicorp/go-plugin`.** go-plugin is the PRD's own named reference
  ("gRPC broker... the go-plugin model"), and was seriously considered. It
  was rejected for this slice because: (1) go-plugin's own handshake
  protocol prints a magic-cookie-gated line to stdout and negotiates a
  dynamic TCP port or Unix socket via its own `plugin.Serve`/`plugin.Client`
  API, which would mean the *plugin subprocess* has to import
  `hashicorp/go-plugin` too — reasonable for a Go-only plugin ecosystem, but
  this PRD's whole Tier-C pitch (§8.1, "own dependencies... language
  agnostic is Tier B's thing, but Tier C plugins are still meant to be
  independently developed, possibly non-Go, processes) is better served by
  a wire contract (the `.proto` file) than a Go-specific host/client
  library; (2) go-plugin's crash/negotiation semantics are additional
  surface to learn and audit beyond what this slice strictly needs; (3)
  building directly on `grpc-go` makes the "gRPC broker, process
  supervision, crash isolation" requirement each independently visible and
  testable in this package's own code, rather than delegated to a
  dependency's internals. The trade-off accepted: this slice's handshake
  (a single fixed stdout line, `GLYPHUX_RPC_PLUGIN_READY`) and process
  supervision (a `cmd.Wait()` goroutine) are hand-rolled and simpler than
  go-plugin's, which is fine for what this slice needs but is less
  battle-tested than adopting go-plugin wholesale. If a future slice wants
  go-plugin's fuller feature set (mTLS between host/plugin, richer
  multi-version handshake negotiation), swapping it in later is a contained
  change: the `.proto` contract and `hostServer`/fixture logic carry over,
  only `Broker.Launch`'s process/transport bootstrapping would change.
- **Transport: Unix domain sockets, not loopback TCP**, for both directions
  (host's `HostAPI` service and the plugin's `Plugin` service). A UDS path
  lives in a per-`Broker` `os.MkdirTemp` directory no other process can
  guess or collide with; there's no port-bind race; the socket file's
  filesystom permissions are a free extra access-control layer. Documented
  on `Broker`'s own doc comment. Accepted cost: no remote-host portability —
  judged fine, since PRD §8.1 says Tier C plugins are "own process," never
  "own machine."
- **The gRPC service split is directional, not symmetric**: `HostAPI`
  (server: host process, client: plugin subprocess) carries every RPC this
  slice exposes from `pkg/sdk.HostAPI` (`Store{Get,Set,Delete}`, `Emit`,
  `AllowsNetworkHost`); `Plugin` (server: plugin subprocess, client: host
  process) carries exactly one RPC, `Register(mode, arg) -> (log, ok,
  error)`, mirroring the conceptual `Plugin.Register(host HostAPI)` entry
  point from PRD §8.3. The host calls `Register` once it has confirmed (via
  the stdout handshake) that the subprocess's `Plugin` service is listening;
  the subprocess's `Register` handler is expected to (and, in the test
  fixture, does) call back into `HostAPI` during its own handling of that
  call — proving a real bidirectional round trip, not a one-directional
  demo.
- **`hostServer` (pkg/runtime/rpc/hostserver.go) adds ZERO gating logic of
  its own.** Every RPC method is a thin delegation to a real,
  already-constructed `sdk.HostAPI` value (built via `sdk.NewHostAPI` against
  the plugin's actual manifest, upstream of this package). This is
  deliberate and is what makes "expose to the subprocess ONLY the HostAPI
  methods corresponding to declared capabilities" true without
  reimplementing pkg/sdk's capability checks: `sdk.HostAPI.Emit` already
  returns an error when `events:emit` isn't declared (pkg/sdk/host.go's
  `hasScope` check) — `hostServer.Emit` just forwards that error as a gRPC
  status. The gRPC *method* is always technically callable (gRPC has no
  per-call, per-caller method-hiding primitive at the server-registration
  level) but *functionally* denied every time for an undeclared capability,
  which is the deny-by-default property the brief asks to prove for real —
  and is proven for real in `TestBrokerEmitDeniedWithoutDeclaredScope` vs.
  `TestBrokerEmitAllowedWithDeclaredScope` (same fixture binary, same RPC,
  two different manifests, two different real outcomes).
- **Error-mapping had to work around an actual pkg/sdk shape quirk**:
  `sdk.HostAPI`'s gating methods build their returned error via string
  concatenation (`errors.New("events:emit: " + ErrScopeNotDeclared.Error())`),
  not `fmt.Errorf("%w: ...", ErrScopeNotDeclared)` — so `errors.Is` does not
  find the sentinel in the chain. `hostserver.go`'s `toGRPCError` detects
  the denial via `strings.Contains(err.Error(),
  sdk.ErrScopeNotDeclared.Error())` instead, since this slice must not
  modify `pkg/sdk`. Documented in `toGRPCError`'s own comment so a future
  reader doesn't mistake this for an oversight if `errors.Is` is later tried
  and silently fails.
- **The consent-engine seam (slice 2.7) is `rpc.ConsentChecker`**, an
  interface (`Allowed(pluginName, capability, scope string) bool`) defined
  in `broker.go`, documented as unused by this slice — "declared in the
  manifest" is treated as "consented," exactly matching `pkg/sdk`'s own
  existing `NewHostAPI` behavior (slice 2.1/2.6), because the actual
  enforcement already lives one layer down in the `sdk.HostAPI` this
  package's `Broker` is handed. A future consent engine most naturally
  plugs in upstream of `Broker` (filtering/rewriting the manifest before
  `sdk.NewHostAPI` builds the `HostAPI` this package exposes), which is why
  `ConsentChecker` is defined but not wired into `Launch`/`hostServer` at
  all — wiring it in would mean building consent logic, explicitly out of
  scope for this slice.
- **Process supervision is crash/exit detection only — no restart,
  no backoff, no health-check polling**, exactly as the brief allows as a
  "reasonable stretch goal but not required." `Broker.State()` (an
  `atomic.Int32`-backed enum: `StateStarting` -> `StateRunning` ->
  `StateDead`, terminal) and `Broker.Done()`/`Wait()` are the observable
  surface; a `cmd.Wait()` goroutine is the sole detection mechanism, started
  immediately after `cmd.Start()` succeeds. No polling loop anywhere in this
  package — every synchronization point (readiness, exit) is a channel
  close driven by a real event (a stdout line read, or `cmd.Wait()`
  returning), which is also what keeps the crash tests fast and
  non-flaky under `-race` (typically ~1.2s total for the whole package).
- **Allowlisted network is wired as a callable decision primitive only,
  not outbound-traffic interception** — `hostServer.AllowsNetworkHost`
  forwards to `sdk.HostAPI.AllowsNetworkHost` (itself a pure decision
  function over the manifest's declared `network` permission, from slice
  2.6). This slice's fixture demonstrates the plugin subprocess actually
  querying it over a real gRPC call and getting real allow/deny answers
  back (`TestBrokerAllowsNetworkHostReflectsManifestAllowlist`). Actually
  intercepting the subprocess's own raw OS-level network syscalls (e.g. via
  a network namespace, an LD_PRELOAD shim, or an enforced HTTP-proxy
  environment variable convention) is explicitly out of scope, and is a
  materially different, much larger problem than Tier B's WASM sandbox
  (which denies network at the *runtime* level because Wazero mediates
  every host call the WASM module can make at all). Tier C's isolation is
  process-level, matching PRD §8.1's own framing ("comparatively trusted...
  own dependencies, outbound network... process isolation is a security
  feature"): a Tier-C plugin is trusted with its own process and its own
  network access; the allowlist is a declared-intent record the host can
  query and act on (e.g. for audit logging, or a future explicit egress
  proxy), not a sandboxing wall.
- **Test fixture is one reusable binary, mode-selected per RPC call**
  (`pkg/runtime/rpc/testdata/fixtureplugin`), not one binary per behavior.
  `TestMain` builds it once via a real `go build` invocation into a shared
  temp dir, and every test in the package launches a fresh `Broker` against
  the same binary. Startup-time behavior (crash before ever listening) is
  selected via `GLYPHUX_FIXTURE_CRASH_BEFORE_READY=1`; per-call behavior
  (`store-roundtrip`, `emit`, `network-check`, `crash`) is selected via the
  `Register` RPC's own `mode`/`arg` fields — so one process can be reused
  across several different real gRPC round trips.

## Open Questions — resolved

- **Should the Broker itself apply capability gating, duplicating
  pkg/sdk's checks?** Resolved: no — see "hostServer adds zero gating logic
  of its own" above. Duplicating the check here would create two sources
  of truth for the same policy and risk drifting from pkg/sdk's actual
  behavior; delegating means this slice automatically inherits any future
  pkg/sdk gating change (e.g. slice 2.7's consent engine) without this
  package needing to change at all, as long as `sdk.HostAPI`'s method
  behavior is what changes.
- **Loopback TCP vs. Unix domain socket?** Resolved: UDS (see Current
  Decisions) — no meaningful use case in this slice's scope needs the
  plugin subprocess to run on a different host, and UDS sidesteps an entire
  class of port-collision/loopback-exposure concerns for free.
- **Should this slice build the full Plugin.Register handshake (host calls
  plugin once to register content types/blocks/jobs, mirroring a real
  Tier-C plugin's actual startup), or is a test-only `Register(mode, arg)`
  RPC enough?** Resolved: the `Register` RPC exists and is exercised
  bidirectionally (fixture calls back into HostAPI during its handling),
  which is the structurally important thing this slice needs to prove (a
  real, bidirectional gRPC round trip across a real process boundary). A
  fuller Register contract that actually carries `RegisterContentType`/
  `RegisterBlock`/`RegisterAdminPage`/`RegisterJob` declarations over the
  wire is a natural next increment when a real Tier-C plugin author-facing
  SDK is built, but is not required to prove THIS slice's three named
  properties (gRPC broker, process supervision, crash isolation) plus
  deny-by-default and allowlisted-network wiring.

## Files/Modules Changed

- `pkg/runtime/rpc/proto/hostapi.proto` (new) — the wire contract: `HostAPI`
  service (`StoreGet`/`StoreSet`/`StoreDelete`/`Emit`/`AllowsNetworkHost`)
  and `Plugin` service (`Register`).
- `pkg/runtime/rpc/rpcpb/hostapi.pb.go`, `hostapi_grpc.pb.go` (generated,
  via `protoc --go_out --go-grpc_out`) — do not hand-edit.
- `pkg/runtime/rpc/hostserver.go` (new) — `hostServer`, the `HostAPI` gRPC
  server delegating every RPC to a real `sdk.HostAPI`; `toGRPCError`.
- `pkg/runtime/rpc/broker.go` (new) — `Broker`, `Config`, `State`
  enum, `Launch`, `Register`, `State`/`Done`/`Wait`/`Stdout`/`Stderr`/
  `Close`; `ConsentChecker` seam interface.
- `pkg/runtime/rpc/testdata/fixtureplugin/main.go` (new) — the real,
  separately-compiled test subprocess binary.
- `pkg/runtime/rpc/broker_test.go` (new) — 6 tests, `TestMain` builds the
  fixture binary once via `go build`.
- `go.mod`/`go.sum` — added `google.golang.org/grpc`,
  `google.golang.org/protobuf` (direct), `google.golang.org/genproto/googleapis/rpc`
  (indirect, grpc's status-code dependency). Ran `go mod tidy`; verified
  consistent.

## Acceptance Criteria

- [x] A real OS subprocess is spawned (`exec.Command` + `cmd.Start()`), not
      an in-process fake — `fixtureplugin` is compiled via a real `go build`
      in `TestMain` and launched as a genuine child process every test.
- [x] A real gRPC connection is established and a real gRPC call crosses the
      process boundary, backed by a real `sdk.HostAPI` value on the host
      side — `TestBrokerStoreRoundTripCrossesRealProcessBoundary`: the
      fixture's `Store.Set`/`Store.Get` round trip is independently
      re-verified by reading the SAME key directly through the host-side
      `sdk.HostAPI.Store()` after the RPC call returns, proving it went
      through the real shared KV backend, not a fixture-local stand-in.
- [x] Deny-by-default is proven for real: the identical RPC (`Emit`) against
      the identical fixture binary succeeds or fails purely based on
      whether the manifest backing the host-side `sdk.HostAPI` declared
      `events:emit` — `TestBrokerEmitDeniedWithoutDeclaredScope`,
      `TestBrokerEmitAllowedWithDeclaredScope`.
- [x] Allowlisted network is wired to `sdk.AllowsNetworkHost` and callable
      from the plugin subprocess over a real gRPC round trip, with a real
      allow and a real deny outcome —
      `TestBrokerAllowsNetworkHostReflectsManifestAllowlist`.
- [x] A subprocess crash BEFORE it ever becomes usable is detected without
      hanging `Launch` and without taking down the host test process —
      `TestBrokerDetectsCrashBeforeReady`: asserts `ErrPluginNotReady`,
      `StateDead`, and a real `*exec.ExitError` with exit code 1 from
      `Wait()`.
- [x] A subprocess crash DURING an in-flight RPC call is detected — the
      fixture calls `os.Exit(1)` from inside its own `Register` handler —
      and the Broker transitions to `StateDead` without the host test
      process going down — `TestBrokerDetectsCrashDuringCall`: additionally
      launches a brand-new, unrelated second `Broker` right after the crash
      and asserts IT reaches `StateRunning` successfully, as positive proof
      the host survived and remains fully functional.
- [x] `go build ./...`, `go vet ./...` clean across the whole repo.
- [x] `go test -race ./pkg/runtime/rpc/...` green, fast (~1.2s), no fixed
      sleeps (every synchronization point is a channel close driven by a
      real event: a stdout line, or `cmd.Wait()` returning).
- [x] `go mod tidy` run; `go.sum` consistent; only `grpc`/`protobuf`
      (+ their one transitive genproto dependency) added.
- [ ] Full-repo `go test -race ./...` — run and confirmed green as part of
      this slice's final verification pass (see PR/commit description for
      the actual run's result; not re-asserted redundantly here since it
      covers ~20 unrelated packages this slice did not touch).

## Risks

- **Hand-rolled handshake/supervision vs. adopting `hashicorp/go-plugin`
  wholesale.** As explained above, this was a deliberate scope/complexity
  trade-off, not an oversight. If a future slice needs mTLS between
  host/plugin, richer multi-protocol-version negotiation, or a
  restart-with-backoff policy, evaluate `go-plugin` again at that point —
  its `plugin.Client`/`plugin.Serve` do already solve those, at the cost of
  the plugin subprocess also needing to import a Go-specific library
  (weakening the "any language can write a Tier-C plugin" story this
  proto-first design keeps open).
- **`toGRPCError`'s denial-detection is a `strings.Contains` match against
  `pkg/sdk`'s error TEXT**, not a typed sentinel walk (`errors.Is`), because
  `pkg/sdk`'s gating methods don't wrap their sentinel with `%w` (see
  Current Decisions). This is brittle to a future wording change in
  `pkg/sdk`'s error messages: if a later `pkg/sdk` slice changes
  `ErrScopeNotDeclared`'s `.Error()` string, `toGRPCError`'s detection
  silently stops matching (falls back to `codes.Unknown`, which is still an
  error, just a less-specific gRPC code — no test in THIS slice would
  observe that regression, since the current tests only assert
  success/failure via `resp.Ok`, not the specific gRPC code). If `pkg/sdk`
  later wraps its sentinel with `%w`, switching `toGRPCError` to
  `errors.Is` is a one-line, contained fix.
- **No restart/backoff policy.** A crashed plugin stays crashed — `Broker`
  never attempts to relaunch it. For a real deployment, an operator or a
  higher-level supervisor (not built in this slice) needs to observe
  `Broker.Done()`/`State()` and decide whether/how to relaunch. Explicitly
  named as deferred in the brief; not a gap specific to this
  implementation's choices.
- **`ConsentChecker` is a documented, unused seam, not a wired mechanism.**
  Nothing prevents a caller from constructing a `Broker` around an
  `sdk.HostAPI` built from an unreviewed manifest today — exactly the
  current state of `pkg/sdk` itself (slice 2.1/2.6's own documented gap).
  Slice 2.7 is expected to close this by filtering the manifest (or
  wrapping `sdk.HostAPI`) BEFORE it reaches `rpc.Launch`, not by this
  package growing new logic.
- **UDS temp-directory cleanup relies on `Close()` being called** (removes
  `os.MkdirTemp`'s directory). Every test uses `t.Cleanup(func() {
  b.Close() })`, but a caller that leaks a `Broker` without calling `Close`
  leaks its socket directory on disk until the OS temp-dir is cleared. Not
  a correctness bug (each dir is uniquely named, no collision risk), just a
  minor resource-leak-on-misuse risk worth flagging for callers.
