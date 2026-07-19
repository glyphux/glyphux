# Implementation: Phase 2 slice 2.4 — WASM runtime (Tier B)

## Goal

PRD §8.1 names Tier B ("WASM — Wazero host + WIT contract") as the primary
third-party plugin tier: "small, numerous, untrusted, latency-sensitive
plugins... deny-by-default sandbox, language-agnostic, in-process speed,
single-binary preserved (Wazero is pure Go, no CGO)." §10.3 is the sharper
requirement behind that: "The WASM host... [is] where a plugin is stripped
of anything it did not declare and the admin did not consent to.
Capabilities granted = host functions exposed." This slice builds that
boundary: a real Wazero-hosted runtime that loads a compiled WASM guest and
gives it host functions ONLY for capabilities its manifest declared,
bridging into a real `pkg/sdk.HostAPI` — not a Go-struct simulation of what
a call might do.

## Owning Contexts

- (no `CONTEXT.md` exists yet in this repo — see `docs/agents/domain.md`;
  this is core-kernel work, a new package `pkg/runtime/wasm` that imports
  `pkg/sdk` but does not modify it)

## Status

Complete. Merged into `dev` (go.mod/go.sum auto-merged cleanly alongside
slice 2.5's own new dependencies). Independently re-verified: full repo
build/vet/test -race, plus a hand-driven program chaining the real consent
engine (slice 2.7) → a real `sdk.HostAPI` → this runtime loading the actual
compiled `events_guest.wasm` → a real emitted event reaching a real Go
subscriber over the shared EventBus, and confirming the same guest refuses
to instantiate against a manifest that doesn't declare `events:emit`.

## Current Decisions

- **New dependency: `github.com/tetratelabs/wazero v1.12.0`.** Pure Go, no
  CGO — required to preserve the "single binary, host anywhere" promise
  (PRD §8.1, §11.1). Added via `go get` + `go mod tidy`; ends up a direct
  (not indirect) dependency since `pkg/runtime/wasm` imports it directly.

- **A real, compiled, non-mocked WASM guest DOES run in this environment —
  no tooling wall was hit.** The sandbox has `rustc`/`cargo` pre-installed;
  `rustup target add wasm32-unknown-unknown` succeeded (network access to
  the Rust component registry is available here), giving a real toolchain
  to compile `wasm32-unknown-unknown` binaries. Two `#![no_std]` Rust guest
  fixtures were written and compiled with
  `rustc --target wasm32-unknown-unknown -C opt-level=z -C panic=abort
  --crate-type cdylib <src>.rs -o <out>.wasm`, committed as both source
  (`testdata/*.rs`) and the compiled artifact (`testdata/*.wasm`) so the
  fixture is reproducible and auditable, not just a binary blob. `#![no_std]`
  was chosen deliberately over `std` to avoid `wasm32-unknown-unknown`'s
  allocator/target-feature complications entirely — every fixture uses only
  fixed-size static buffers, no heap. Verified with a throwaway
  `wazero.Runtime.CompileModule` + `ImportedFunctions()`/`ExportedFunctions()`
  inspection that each fixture's real import/export shape is exactly as
  intended before writing any Go implementation against them (see
  "Files/Modules Changed" for the two fixtures' exact shapes).

- **Two separate guest binaries, one per capability domain, not one
  do-everything binary.** `testdata/kv_guest.wasm` imports ONLY
  `env.kv_get`/`env.kv_set`/`env.kv_delete` (exports `run_kv_roundtrip`,
  `trigger_trap`); `testdata/events_guest.wasm` imports ONLY
  `env.emit_event` (exports `run_emit`, `event_buf_ptr`, `on_event`,
  `last_event_len`, `last_event_byte`). This is what makes the
  deny-by-default proof surgical rather than all-or-nothing: WASM's own
  link semantics fail an ENTIRE module's instantiation if any ONE of its
  declared imports is unsatisfied, so a single guest importing both KV and
  event functions would conflate "no events capability" with "the whole
  plugin fails to load" in a way that says nothing precise about the events
  domain specifically. Splitting the fixtures isolates exactly what's being
  proven per test.

- **`kv_get`/`kv_set`/`kv_delete` are unconditionally exposed, regardless of
  declared API scopes.** This mirrors `pkg/sdk`'s own contract exactly:
  `host.go`'s `Store()` doc comment says it is "always present, unlike
  Content()/Users()/Media(), since scoped persistence carries no api
  capability of its own to declare." `buildHostModule` (hostfuncs.go)
  reproduces that same rule rather than inventing a stricter one that
  exists nowhere else in the SDK — `TestKVAvailableRegardlessOfDeclaredAPIScopes`
  proves a manifest with zero API scopes still gets a working KV bridge.

- **`emit_event` is gated on `declaredScope(manifest, "events", "emit")` AND
  `ConsentChecker.Consented(manifest.Name, "events")`.** This is the
  slice's core deny-by-default demonstration: absent either condition,
  `buildHostModule` (hostfuncs.go) simply never registers `emit_event` on
  the "env" host module, so `events_guest.wasm`'s own import section (which
  unconditionally needs it) fails to link — a genuine `wazero` instantiation
  error surfaces from `Runtime.Load`, not a runtime call rejection the guest
  could route around. Proven by
  `TestEventsGuestFailsToInstantiateWithoutDeclaredEmitCapability` (no API
  scopes at all), `TestEventsGuestFailsToInstantiateWithOnlySubscribeDeclared`
  (proving the two "events" scopes, emit and subscribe, are independently
  gated — declaring one does not imply the other), and
  `TestConsentCheckerCanDenyAnAlreadyDeclaredCapability` (proving the
  `ConsentChecker` seam is a real second gate, not decorative — a
  deny-everything checker still blocks a manifest that DID declare
  `events:emit`).

- **The `ConsentChecker` seam (`consent.go`) is a one-method interface:
  `Consented(pluginName, capability string) bool`**, called once per
  candidate capability while building the host module. Slice 2.4
  deliberately ships only `AlwaysConsent{}` (treats every declared
  capability as consented) as the default — building the real install-time
  consent engine is explicitly slice 2.7's job, running in parallel. The
  seam's shape was kept intentionally minimal (a single boolean-returning
  method per plugin+capability pair) specifically so 2.7 can drop in a real
  implementation (backed by whatever storage records an admin's actual
  install-time "Allow?" decision, PRD §10.2 point 2) as a drop-in
  `ConsentChecker` without `Runtime`/`buildHostModule` needing to change
  shape at all.

- **Each `Load()` call gets its OWN private child `wazero.Runtime`, not one
  shared engine.** Every plugin's host module must be named `"env"` (the
  fixed `wasm_import_module` name `wasm32-unknown-unknown`'s
  `#[link(...)]` convention uses, matching every `testdata/*.rs` fixture),
  and `wazero` requires module names to be unique within one
  `wazero.Runtime`'s namespace — so two plugins loaded against a single
  shared `wazero.Runtime` would collide on `"env"`. Giving each `Instance`
  its own child engine (created fresh inside `Load`, sharing only a
  `wazero.CompilationCache` held by the parent `Runtime` so re-loading the
  same guest bytecode across multiple instances doesn't recompile) sidesteps
  the collision AND strengthens isolation: a trap in one plugin cannot touch
  a sibling's engine state at all, not merely its own linear memory. This
  was discovered empirically — the first version shared one `wazero.Runtime`
  and a second `Load()` call failed with `module[env] has already been
  instantiated`; the fix is documented here rather than left as a silent
  workaround.

- **Crash/trap isolation relies entirely on Wazero's own per-module
  isolation — no extra Go-side recover()/goroutine wrapping was added.**
  `Instance.Call` just forwards to `api.Function.Call`, which already
  returns an ordinary Go `error` on a guest trap (Wazero's own contract);
  `testdata/kv_guest.rs`'s `trigger_trap` deliberately writes through a wild
  pointer (`0xffff_fff0`) to trigger a real out-of-bounds trap.
  `TestGuestTrapDoesNotCrashHostOrSiblingInstance` proves: (1) the trapping
  call returns a non-nil error rather than crashing the test binary, (2) a
  freshly-loaded, unrelated sibling instance on the same parent `Runtime`
  is fully unaffected (its own KV round trip still succeeds), and (3)
  calling the trapped instance again keeps erroring rather than succeeding
  silently or corrupting further.

- **`On`/`Subscribe` is a real host-to-guest callback, not a stub.**
  `Instance.Subscribe(event string) error` calls the real, unmodified
  `sdk.HostAPI.On` (so every gate `pkg/sdk` itself already enforces —
  `events:subscribe`, plus any `sensitiveEvents` domain capability — applies
  exactly as it does for a Tier-A plugin, with zero separate WASM-side gate
  duplicating that logic); the registered handler writes the emitted
  payload's bytes into the guest's own memory (at the address its exported
  `event_buf_ptr()` reports) and then calls the guest's exported
  `on_event(len)` function. This is a real re-entrant call INTO the guest
  from a Go closure running on the shared `EventBus`'s dispatch path, proven
  by `TestSubscribeDeliversEmittedPayloadIntoGuestMemory`, which emits from
  the Go side and then calls back into the SAME guest instance's
  `last_event_len`/`last_event_byte` exports to read back what the guest
  actually received in its own linear memory.

- **The KV/event host-function ABI is hand-rolled, not WIT-generated.**
  Fixed-width `i32` pointer/length/result parameters, raw byte payloads,
  negative sentinel result codes (`kvNotFound = -1`, `kvHostError = -2`) —
  see `hostfuncs.go`. The PRD mentions "Wazero host + WIT contract"; a
  formal WIT interface (and the corresponding `wit-bindgen`/`wasmtime`-style
  generated bindings) is explicitly deferred (see Risks) — this slice proves
  the boundary and its capability-gating semantics work end to end with a
  real guest first, before investing in a generated-bindings layer for an
  ABI that may still change as slices 2.5 (RPC/Tier C) and later Phase-2/3
  slices settle on what the unified contract (§8.3) actually needs.

## Open Questions — resolved

- **Should the WASM boundary re-derive its own capability-gating logic, or
  forward into `pkg/sdk`'s existing gates wherever one already exists?**
  Resolved: forward wherever `pkg/sdk` already has the gate (`On`/`Emit`
  both already check `events:subscribe`/`events:emit` internally, so
  `Subscribe`/`emit_event` just call them unmodified). Where `pkg/sdk` has
  no import-time-relevant gate (Store has none at all; whether
  `emit_event`'s HOST FUNCTION should even EXIST for a given guest is a
  WASM-boundary-specific question `pkg/sdk` itself doesn't answer, since it
  operates at the Go-call level, not the WASM-import level), the WASM
  runtime layer (`declaredScope` in hostfuncs.go) reads the same
  `Manifest.API` data `pkg/sdk` itself reads, rather than inventing new
  manifest fields — same source of truth, evaluated at a different layer
  (import-set construction time vs. Go-call time).
- **Should `Runtime` share one `wazero.Runtime` across all loaded plugins
  for efficiency?** Resolved: no — see the "child `wazero.Runtime` per
  `Load()`" decision above. A shared `wazero.CompilationCache` recovers most
  of the efficiency loss (recompilation is skipped for repeat loads of the
  same bytecode) without the module-name collision.
- **Is a raw byte-slice payload ABI (`emit_event`/`on_event` carrying
  arbitrary bytes with no schema) sufficient for this slice, given
  `pkg/sdk.HostAPI.Emit`'s payload type is `any`?** Resolved: yes, for this
  slice. The Go-side test subscribes with a handler that type-asserts
  `payload.([]byte)`, and the guest fixtures only ever send/receive raw
  bytes — no attempt was made to carry structured (e.g. JSON) payloads
  across the boundary, since the ABI's schema is an open design question
  for whatever later slice formalizes the WIT contract (see Risks).

## Files/Modules Changed

- `go.mod`, `go.sum` — added `github.com/tetratelabs/wazero v1.12.0` (direct
  dependency).
- `pkg/runtime/wasm/consent.go` (new) — `ConsentChecker` interface (the
  slice-2.7 seam) and `AlwaysConsent{}`, the slice-2.4 default
  implementation.
- `pkg/runtime/wasm/hostfuncs.go` (new) — `declaredScope` (manifest API-scope
  lookup), `buildHostModule` (constructs the "env" host module with only the
  functions a manifest+consent combination allows), and the four host
  function bridges (`kvGetFunc`, `kvSetFunc`, `kvDeleteFunc`,
  `emitEventFunc`), each a real closure over a `sdk.HostAPI` reading/writing
  guest linear memory via `api.Module.Memory()`.
- `pkg/runtime/wasm/runtime.go` (new) — package doc (states this slice's
  real-vs-deferred scope up front), `Runtime` (`New`, `Close`, `Load`) and
  `Instance` (`Call`, `Subscribe`, `Close`).
- `pkg/runtime/wasm/runtime_test.go` (new) — 9 tests, all against real
  `sdk.NewHostAPI`-constructed `HostAPI` values and real compiled `.wasm`
  fixtures (no fakes/mocks): KV round trip through the real `Store()`; KV
  availability with zero declared API scopes; guest `emit_event` reaching a
  DIFFERENT plugin's subscriber via a shared `EventBus`; deny-by-default
  instantiation failure with no `events:emit` declared; deny-by-default
  instantiation failure with only `events:subscribe` declared (proving the
  two scopes are independently gated); `ConsentChecker` denial overriding an
  already-declared capability; `Subscribe` rejected without
  `events:subscribe` declared (forwarding `pkg/sdk`'s own
  `ErrScopeNotDeclared`); `Subscribe` delivering a Go-side-emitted payload
  into the guest's own memory and reading it back via the guest's own
  exports; guest trap isolation (trapping call errors without crashing the
  process, a fresh sibling instance is unaffected, repeat calls to the
  trapped instance keep erroring).
- `pkg/runtime/wasm/testdata/kv_guest.rs` (new) — `#![no_std]` Rust source,
  imports `env.kv_get`/`kv_set`/`kv_delete`, exports `run_kv_roundtrip`
  (set→get→compare→delete→confirm-gone) and `trigger_trap` (wild-pointer
  write).
- `pkg/runtime/wasm/testdata/kv_guest.wasm` (new) — compiled artifact
  (`rustc --target wasm32-unknown-unknown -C opt-level=z -C panic=abort
  --crate-type cdylib kv_guest.rs -o kv_guest.wasm`).
- `pkg/runtime/wasm/testdata/events_guest.rs` (new) — `#![no_std]` Rust
  source, imports ONLY `env.emit_event`, exports `run_emit` (guest-initiated
  emit), plus the subscribe-side callback surface: `event_buf_ptr`,
  `on_event`, `last_event_len`, `last_event_byte`.
- `pkg/runtime/wasm/testdata/events_guest.wasm` (new) — compiled artifact,
  same `rustc` invocation as above.

## Acceptance Criteria

- [x] A real, Wazero-hosted, compiled WASM guest module executes and calls
      into a real `pkg/sdk.HostAPI` — not a Go-struct simulation. (No
      tooling wall was hit; see Current Decisions.)
- [x] `Store()` get/set/delete is a real, working bridge:
      `TestKVRoundTripThroughRealHostAPIStore` proves both the guest's own
      observed round trip AND, independently, that the real
      `host.Store().Get` reflects the guest's delete afterward.
- [x] `Emit`/`On` is a real, working bridge in both directions: guest→host
      (`TestEmitEventGuestReachesRealEventBus`, delivered to a DIFFERENT
      plugin's subscriber via a shared bus) and host→guest
      (`TestSubscribeDeliversEmittedPayloadIntoGuestMemory`, payload bytes
      landing in and read back from the guest's own linear memory).
- [x] Deny-by-default is proven by ABSENCE, not merely a runtime error path:
      `TestEventsGuestFailsToInstantiateWithoutDeclaredEmitCapability` and
      `TestEventsGuestFailsToInstantiateWithOnlySubscribeDeclared` show a
      guest whose own import section needs a capability-gated host function
      fails to INSTANTIATE (a real Wazero link error) when that capability
      wasn't declared/consented.
- [x] The `ConsentChecker` seam is real and load-bearing, not decorative —
      `TestConsentCheckerCanDenyAnAlreadyDeclaredCapability` shows a
      deny-everything checker blocks a manifest that DID declare the
      capability.
- [x] Crash/trap isolation is proven with a real trapping guest call:
      `TestGuestTrapDoesNotCrashHostOrSiblingInstance`.
- [x] `go build ./...`, `go vet ./...`, `go test -race ./...` all green
      across the whole repo (see verification note below — full-repo
      `-race` run takes >120s given this repo's existing DB-backed test
      suites, so it was run as a background job; see this doc's final
      status update once it completes).
- [x] `go mod tidy` run; `go.sum` consistent (wazero moved from
      accidental-indirect-if-untidied to a properly recorded direct
      dependency).

## Risks

- **No WIT contract, no generated bindings.** The ABI in `hostfuncs.go` is
  hand-rolled (raw `i32` pointers/lengths, byte payloads, negative sentinel
  codes) and would need to be redesigned, likely non-trivially, once a real
  WIT interface is defined for the unified extension contract (§8.3) across
  all three tiers. This slice's ABI should be treated as a working proof of
  the boundary's ENFORCEMENT semantics (capability→host-function presence),
  not as the ABI third-party plugin authors will eventually target.
- **No resource limits beyond Wazero's defaults.** No fuel/gas metering, no
  explicit memory cap per instance, no execution timeout wrapping `Call`. A
  guest with an infinite loop (not a trap) will hang the calling goroutine
  indefinitely today — this is a real, currently-unmitigated gap for a
  "deny-by-default... untrusted... latency-sensitive" tier per §8.1;
  deferred explicitly, not an oversight.
- **`ConsentChecker`'s real implementation (slice 2.7) is unwritten.**
  `AlwaysConsent{}` treats "declared" as "consented" everywhere this
  package is used until 2.7 lands and a caller passes in something else.
  Nothing currently prevents a caller from continuing to pass
  `AlwaysConsent{}` in production after 2.7 ships a real checker — that
  wiring is 2.7's/the plugin-loading integration's responsibility, not
  something this package can enforce from inside itself.
- **`RegisterContentType`/`RegisterBlock`/`RegisterAdminPage`/`RegisterJob`/
  `Content()`/`Users()`/`Media()` are not bridged to any WASM host
  function.** Only `Store()` and `On`/`Emit` were bridged, per this slice's
  explicit scope ("pick a demonstrably real vertical slice"). A real Tier-B
  plugin loader integrating this package for production use would need to
  extend `hostfuncs.go` with host functions for the remaining `HostAPI`
  surface, following the same `declaredScope`-gated pattern established
  here.
- **The child-`wazero.Runtime`-per-`Load()` design has not been load/perf
  tested.** It sidesteps the `"env"` module-name collision correctly and
  passes every test in this slice, but creating a full `wazero.Runtime` per
  plugin instance has a per-instantiation cost this slice did not measure
  against "many, numerous" Tier-B plugins (§8.1's own framing) — worth
  revisiting if a future slice needs to load large numbers of concurrent
  plugin instances.
- **Guest fixtures are `#![no_std]`, fixed-buffer, hand-written Rust — not
  representative of what a real plugin author (using `std`, dynamic
  allocation, a higher-level SDK wrapper) would write.** They exist purely
  to exercise this slice's Go-side boundary logic against genuine compiled
  WASM bytecode; a future slice building an actual plugin-author-facing SDK
  (e.g. a `sdk-rust` or `sdk-tinygo` crate/package wrapping this ABI) is
  separate, undone work.
