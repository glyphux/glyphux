# Implementation: Phase 2 slice 2.2 — real cross-plugin event bus

## Goal

PRD §8.4 ("The Event System — Actions and Filters") requires a single event
bus underlying all plugin tiers: plugins subscribe to and emit lifecycle
events (`composition.before_build`, ... `request.after_render`) and domain
events (`user.created`, ... `plugin.error`), and "subscribing to a sensitive
event ... requires the corresponding capability scope, because the
subscriber sees sensitive data flow — emitting and subscribing carry
different risk and are scoped separately." Slice 2.1 built `HostAPI.On`/
`Emit` as a deliberately minimal, same-process, ungated stub with a single
private subscriber map per `HostAPI` instance. This slice replaces that
stub's internals with a real cross-plugin bus and real capability gating on
subscription, while keeping `HostAPI`'s `On`/`Emit` method signatures
unchanged (per 2.1's own tracking doc, that stability was the point of
building the stub first).

## Owning Contexts

- (no `CONTEXT.md` exists yet in this repo — see `docs/agents/domain.md`;
  this is core-kernel work inside the existing `pkg/sdk` package from slice
  2.1)

## Status

Implemented and verified in this session (TDD: failing tests written
against the new `KernelDeps.Bus`/`sdk.NewEventBus` shape first, confirmed a
compile-time red, then the minimal implementation added to turn it green).
Not yet PR'd to `staging` — that happens once more of Phase 2 lands, per the
established "dev → PR → staging" convention. This branch does not merge
itself; the parent session reviews, resolves conflicts with the parallel
2.3/2.6 slices, and merges.

## Current Decisions

- **A real `EventBus` type, shared via `KernelDeps.Bus`, mirrors slice
  2.1's `KernelDeps.KV`/`MemoryKVBackend` pattern exactly.** `NewHostAPI`
  uses `deps.Bus` if non-nil, otherwise gives that single `HostAPI` its own
  private bus (same "nil means private, shared field means genuinely
  shared" shape as `KV`). Two `HostAPI` instances built from a `KernelDeps`
  with the same `*EventBus` in `.Bus` truly share subscriptions/emissions —
  proved by `TestHostAPIEventBusIsSharedAcrossPluginsOnSameKernelDeps`,
  which constructs two different plugins from one `KernelDeps`, subscribes
  from one and emits from the other, and asserts the subscriber's handler
  actually ran.
- **Dispatch order is registration order** (same guarantee slice 2.1's
  per-instance stub made, now true bus-wide across every plugin sharing the
  bus) — a plain `map[string][]EventHandler` under one mutex, append-only.
- **Emit's error semantics: collect-and-continue via `errors.Join`, not
  stop-at-first-error.** The PRD doesn't specify this, so this is this
  slice's own defensible choice: plugins are mutually untrusted (§10.1
  "adversarial by default"), so one plugin's misbehaving/erroring handler
  should not silently prevent every *other* plugin subscribed to the same
  event from running at all — that would let one buggy or malicious plugin
  degrade every other plugin on the bus by subscribing to a popular event
  and always erroring. Every subscriber for the event runs regardless of
  earlier failures; the emitting caller gets a non-nil, `errors.Join`-wrapped
  error back if *any* subscriber failed, so it isn't silently swallowed
  either. Verified by
  `TestHostAPIEmitDispatchesToAllSubscribersAndCollectsErrors` (two
  subscribers, the first errors, both are asserted to have actually run, and
  `Emit`'s returned error is non-nil).
- **Baseline gate: `On` requires `events:subscribe`; `Emit` requires
  `events:emit`.** These two scopes already existed in slice 2.1's
  `knownAPIScopes["events"]` but were unused (the stub was "deliberately
  ungated"). This slice gives them real teeth as the floor every plugin must
  clear to touch the bus at all, before any event-specific sensitivity check
  runs. This is a **behavior change** from slice 2.1: a manifest declaring no
  `events` capability can no longer call `On`/`Emit` at all (previously both
  always succeeded). `TestHostAPIOnAndEmitAreCallable` (2.1's original
  smoke test) was updated to declare `events: [subscribe, emit]` rather than
  changed in meaning; two new tests
  (`TestHostAPIOnDeniedWithoutEventsSubscribeScope`,
  `TestHostAPIEmitDeniedWithoutEventsEmitScope`) cover the newly-denied case
  directly.
- **Per-event sensitive-domain gating on top of the baseline, `On` only —
  not `Emit`.** Per §8.4's explicit text, this slice reads emitting and
  subscribing as *deliberately, textually* differently scrutinized: "the
  subscriber sees sensitive data flow" is the stated reason subscribing
  needs the extra check; nothing in the PRD says emitting a sensitive event
  needs anything beyond the baseline `events:emit`. Reasoning for why this
  is defensible, not just PRD-literalism: a plugin that *emits*
  `payment.completed` already possesses that data by construction (it's
  telling the bus about something it already computed/observed); a plugin
  that merely *subscribes* is being granted visibility into data flow it did
  not originate and might otherwise never see. Gating the emit side too
  would not add meaningful protection (the emitting plugin already has the
  data) while gating the subscribe side is the entire point of §8.4's rule.
  `sensitiveEvents` (a `map[string]sensitiveEventRule` in `pkg/sdk/host.go`)
  is the fixed table this slice's judgment produced — a single map literal,
  same "cheap to revise" shape `knownAPIScopes` already uses, specifically
  so a later slice can adjust it without hunting through the codebase.
- **Sensitive-event-to-capability mapping (the hardest judgment call in this
  slice), with reasoning per row:**
  - `user.created`, `user.deleted` -> **`users:read`** (a specific scope,
    not just "declared at all"). Observing account creation/deletion is a
    read-grade exposure of user data, and `users:read` already exists and
    already gates `UsersAPI.List` at the same read-grade — reusing it rather
    than inventing a new scope keeps the vocabulary from proliferating for
    no reason.
  - `order.placed`, `payment.completed`, `payment.refunded` -> **`payments`
    capability declared at all** (any scope; not gated to a specific
    scope). `payments` did not exist in slice 2.1's `knownAPIScopes` — this
    slice adds it, with scopes `charge`/`refund` copied **verbatim** from
    the PRD's own §7.4 example (`payments: [charge, refund]`, line ~620 of
    the PRD). No real `PaymentsAPI` method surface exists yet (commerce is
    Phase 3, slice 3.3) so there is no finer-grained scope to require beyond
    "this plugin declared the payments capability at all" — the same
    "declared at all" gate slice 2.1's `RegisterBlock` already uses for an
    analogous "no finer scope exists yet" situation. `order.placed` is
    folded into the same `payments` gate as the two `payment.*` events
    rather than getting its own capability, because the PRD's own capability
    dependency graph (§7.4, ~line 592-594) does not name a separate
    "orders" capability distinct from "payments" — orders and payments are
    treated as one sensitivity tier (both are financial-transaction data).
    This is a real judgment call and the most likely of this slice's
    mappings to be revisited once slice 3.3 defines commerce for real.
  - `membership.started`, `membership.expired` -> **`membership` capability
    declared at all** (any scope). Also newly added to `knownAPIScopes` by
    this slice, with a single placeholder scope `manage` (the PRD gives no
    explicit scope example for membership the way it does for payments) —
    this is an honest guess, flagged for revision when slice 3.4 defines a
    real `MembershipAPI`. Kept as its own capability rather than folded into
    `users:read` because subscription/access-tier state is a different fact
    than account identity, and the PRD's own capability graph (§7.4) names
    `membership` as its own capability, separate from `users`/`identity`.
  - Every other named §8.4 event (`composition.before_build`/
    `after_build`, `content.before_save`/`after_save`,
    `content.published`/`unpublished`, `request.before_render`/
    `after_render`, `plugin.loaded`/`plugin.error`) gets **no additional
    gate beyond the baseline `events:subscribe`** — these are lower-
    sensitivity structural lifecycle hooks, not domain-data exposure, per
    the PRD's own framing of the two named examples (`user.created`,
    `payment.completed`) as the sensitive category.
- **`knownAPIScopes` extended, not replaced**, with `payments` and
  `membership` entries (see above) — `pkg/sdk/manifest.go`'s existing
  validation *behavior* for `content`/`users`/`media`/`events` is untouched;
  this only adds two new recognized capabilities, which is additive, not a
  change to any existing accept/reject case. All of slice 2.1's
  `manifest_test.go` tests still pass unmodified.
- **No new DB migration.** The bus is in-memory, process-lifetime only,
  exactly like slice 2.1's `MemoryKVBackend` — same deferred-persistence
  gap, same reasoning (see Risks). Checked the highest migration version in
  use repo-wide before starting (`internal/media/store.go`'s version 8 is
  currently the highest across `composition`/`content`/`media`) specifically
  to avoid the migration-version collision this week's incident report
  flagged — moot here since this slice adds no migration at all, but
  recorded per the task's instruction to check first regardless.
- **2.3 (capability dependency resolver) is not depended on or blocked on.**
  This slice's capability-existence check for `payments`/`membership` is the
  same flat `knownAPIScopes` map lookup slice 2.1 already used for
  `content`/`users`/`media`/`events` — no transitive-dependency resolution,
  no cycle detection, nothing that anticipates what 2.3 will add. If 2.3
  wants to validate that declaring `payments`/`membership` auto-resolves
  other capabilities (per PRD §7.4's dependency graph, e.g. `payments:
  events`), that is new work for 2.3 to add on top of this slice's flat
  existence check, not something this slice attempted.

## Open Questions — resolved

- **Should `On`/`Emit`'s new capability checks apply per-`HostAPI`-instance
  or per-bus?** Resolved: per-`HostAPI`-instance, unchanged from slice 2.1.
  Each `hostAPI` value already carries its own `apiScope` map built from its
  own manifest; the shared thing across plugins is only the underlying
  `*EventBus` (subscriber storage and dispatch), never the capability
  check itself — a plugin's gate is always its own manifest, never another
  plugin's. This keeps the "capability not declared is not present"
  invariant (§8.3) exactly as true for the bus as it already is for
  Content()/Users()/Media().
- **Does a lifecycle-only subscriber need `events:subscribe` even though
  slice 2.1 left `On` fully ungated?** Resolved: yes — see the "baseline
  gate" decision above. Leaving the two existing-but-unused `emit`/
  `subscribe` scopes permanently ungated would have made them dead
  vocabulary in `knownAPIScopes` forever; giving them a real baseline
  meaning (and layering the sensitive-event checks on top) is what "giving
  the events capability real teeth" (this slice's brief) means literally.

## Files/Modules Changed

- `pkg/sdk/host.go` — added `EventBus` (`NewEventBus`, `subscribe`, `emit`),
  `sensitiveEventRule`/`sensitiveEvents`; added `Bus *EventBus` field to
  `KernelDeps`; `hostAPI` struct's `subscribers map[string][]EventHandler`
  field replaced with `bus *EventBus`; `NewHostAPI` now resolves
  `deps.Bus` (or a private `NewEventBus()` if nil), mirroring the existing
  `deps.KV` resolution; `On`/`Emit` method bodies rewritten to gate on
  `events:subscribe`/`events:emit` (+ `sensitiveEvents` for `On`) and
  delegate to `h.bus`; `HostAPI` interface doc comment on `On`/`Emit`
  updated to describe the real gating. No other existing method
  (`Content()`/`Users()`/`Media()`/`Store()`/`RegisterContentType`/
  `RegisterBlock`/`RegisterAdminPage`/`RegisterJob`) was touched.
- `pkg/sdk/manifest.go` — `knownAPIScopes` gains two new entries:
  `"payments": {"charge": true, "refund": true}` and `"membership":
  {"manage": true}`. No existing entry or `Validate()` logic changed.
- `pkg/sdk/host_test.go` — `TestHostAPIOnAndEmitAreCallable` updated to
  declare `events: [subscribe, emit]` (previously ungated, now must declare
  the baseline scopes it exercises). Added: `manifestWithEvents` test
  helper; `TestHostAPIOnDeniedWithoutEventsSubscribeScope`,
  `TestHostAPIEmitDeniedWithoutEventsEmitScope`,
  `TestHostAPIEventBusIsSharedAcrossPluginsOnSameKernelDeps`,
  `TestHostAPIEmitDispatchesToAllSubscribersAndCollectsErrors`,
  `TestHostAPIOnDeniedForSensitiveEventWithoutDomainCapability` /
  `...AllowedForSensitiveEventWithDomainCapability` (users:read /
  user.created), `TestHostAPIOnDeniedForPaymentEventWithoutPaymentsCapability`
  / `...AllowedForPaymentEventWithPaymentsCapability` (payments /
  order.placed, payment.refunded), `TestHostAPIOnDeniedForMembershipEventWithoutMembershipCapability`
  / `...AllowedForMembershipEventWithMembershipCapability` (membership /
  membership.expired). Added `"errors"` import.

## Acceptance Criteria

- [x] Multiple `HostAPI` instances built from a shared `KernelDeps.Bus`
      genuinely share subscriptions/emissions (not per-instance private
      maps) — `TestHostAPIEventBusIsSharedAcrossPluginsOnSameKernelDeps`.
- [x] An emitted event dispatches to every subscriber across every plugin
      sharing the bus — same test above (subscriber is a *different*
      plugin than the emitter).
- [x] Emit's handler-error semantics are decided (collect-and-continue via
      `errors.Join`) and documented, and provably every subscriber runs
      despite an earlier one erroring —
      `TestHostAPIEmitDispatchesToAllSubscribersAndCollectsErrors`.
- [x] Subscribing to a sensitive event without the mapped capability is
      denied, and succeeds once declared, for all three mapped families
      (users, payments, membership) —
      `TestHostAPIOnDeniedForSensitiveEventWithoutDomainCapability` /
      `...AllowedForSensitiveEventWithDomainCapability`,
      `TestHostAPIOnDeniedForPaymentEventWithoutPaymentsCapability` /
      `...AllowedForPaymentEventWithPaymentsCapability`,
      `TestHostAPIOnDeniedForMembershipEventWithoutMembershipCapability` /
      `...AllowedForMembershipEventWithMembershipCapability`.
- [x] Baseline `events:subscribe`/`events:emit` scopes are enforced (a
      manifest declaring neither cannot call `On`/`Emit` at all) —
      `TestHostAPIOnDeniedWithoutEventsSubscribeScope`,
      `TestHostAPIEmitDeniedWithoutEventsEmitScope`.
- [x] Every existing slice-2.1 test in `pkg/sdk` still passes (only the one
      smoke test that exercised the now-gated `On`/`Emit` needed a manifest
      update; no other test's meaning changed).
- [x] `go build ./...`, `go vet ./...` both clean.
- [x] `go test -race ./...` green across the whole repo — confirmed by
      running the full suite after the change (see session log; all
      packages `ok`, including `pkg/sdk` at ~12s for its 47 tests under
      `-race`).
- [ ] Not attempted (explicitly out of scope per the task brief): WASM/RPC
      runtime wiring (2.4/2.5), the capability dependency resolver (2.3),
      the consent engine (2.7), audit logging (2.8) — none of `On`/`Emit`'s
      gating in this slice is audit-logged or consent-checked; that is
      later slices' job to layer on top of this same boundary.

## Risks

- **The event bus is in-memory, process-lifetime only** — a subscription
  registered by a plugin does not survive a daemon restart, and there is no
  durability/at-least-once delivery guarantee if a subscriber is slow or the
  process crashes mid-`Emit`. This mirrors slice 2.1's `MemoryKVBackend` gap
  exactly and is deferred for the same reason: this slice's brief was the
  bus's cross-plugin sharing and capability-gating semantics, not
  persistence/durability, and a durable event log is a nontrivial addition
  (likely its own migration) better done deliberately in a later slice than
  rushed into this one.
- **The `payments`/`membership` capability additions to `knownAPIScopes`
  are speculative** — they gate subscription to events about domains that
  have no real kernel implementation yet (commerce/membership are Phase 3).
  A manifest can declare `payments` or `membership` today with zero
  corresponding `HostAPI` method surface actually appearing (no
  `PaymentsAPI`/`MembershipAPI` exists) — the capability currently exists
  *only* to gate event subscription, which is honest about the current
  state of the kernel but means these two capability names may need
  re-shaping (different scopes, possibly split further) once slices 3.3/3.4
  build the real domain APIs behind them.
- **`sensitiveEvents`' event-name strings are untyped `string` literals**,
  matching `On`/`Emit`'s existing `event string` signature from slice 2.1 —
  there is no compile-time enforcement that a plugin (or this map) spells an
  event name correctly (e.g. a typo'd `"user.creaeted"` silently subscribes
  with no gate at all, since it isn't in `sensitiveEvents` and isn't
  flagged as an error by `On`). Introducing a closed `EventName` type/enum
  was considered out of scope for this slice (it would be a breaking change
  to `HostAPI`'s existing method signatures, which the task explicitly asked
  to keep stable) — worth flagging for whichever slice next revisits the
  `On`/`Emit` signatures.
- **No audit logging on subscription/emission** — PRD §10.5 says "every
  sensitive grant and every cross-boundary call is recorded by the audit
  subsystem," but audit logging is explicitly slice 2.8's job (out of scope
  here per the task brief); a sensitive-event subscription being
  granted/denied by this slice is not currently recorded anywhere.
