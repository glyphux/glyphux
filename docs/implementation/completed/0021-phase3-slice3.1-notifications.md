# Implementation: Phase 3 slice 3.1 — `notifications` capability

## Goal

PRD §14 slice 3.1: "email / SMS / push / in-app, over events + a mailer
adapter (region-relevant providers, e.g. Resend/Vonage/FCM). Built early
because commerce and membership depend on it. Validates the RPC tier with a
low-risk, broadly-useful capability." §7.2's capability dependency graph:
`notifications -> events, mailer`. `docs/specs/phase3-4-spec.md`'s Ticket
P3.1 is the authoritative scope: `capabilities/notifications` subscribing
to domain events and dispatching through a provider-agnostic mailer adapter
interface, a documented stub/local adapter being acceptable since no live
email/SMS/push provider credentials exist in this environment — "the
adapter boundary is what matters."

## Owning Contexts

- (no CONTEXT.md exists yet — see docs/agents/domain.md; this adds one new
  package under the existing `capabilities/` directory, following the same
  convention `capabilities/forms` established in slice 2.9. No changes to
  `pkg/sdk` were needed: `events`, `users`, `payments`, and `membership` api
  scopes, plus every event this capability subscribes to, were already
  present in `pkg/sdk/manifest.go`'s `knownAPIScopes` and
  `pkg/sdk/host.go`'s `sensitiveEvents` from slice 2.2.)

## Status

Complete. Built via TDD. `go build ./... && go vet ./... && go test -race
./...` green across the whole repo.

## Current Decisions

- **In-process `sdk.Plugin` (Tier A), not a `pkg/runtime/rpc`-hosted Tier C
  plugin.** The PRD's own sketch names Tier C for this capability
  specifically ("validates the RPC tier"), and that remains a legitimate
  future step — but this slice ships zero live external provider
  integration (no email/SMS/push credentials exist in this environment),
  only the `MailerAdapter` boundary plus an in-memory stub adapter. There
  is no real out-of-process dependency, subprocess, or third-party SDK for
  a Tier C broker to isolate yet; standing up gRPC broker/subprocess
  plumbing to host a plugin that makes zero real network calls would add
  machinery without exercising any additional behavior an in-process
  implementation doesn't already prove. `capabilities/forms` (slice 2.9)
  established this exact judgment call for a similarly low-risk capability,
  and this slice follows it. The natural point to revisit Tier C is a
  future slice that wires a real provider (Resend/Vonage/FCM), which would
  bring its own SDK/dependencies genuinely worth isolating out-of-process.

- **Four events wired, not all seven `sensitiveEvents` entries.**
  `user.created` (welcome email), `payment.completed` (payment receipt),
  `membership.started` (membership welcome), `membership.expired`
  (membership renewal reminder). This covers all three of
  `sensitiveEvents`' distinct gating rules (`users:read`, `payments` any
  scope, `membership` any scope) and both events named in the PRD's own
  §7.2 dependency line ("commerce and membership depend on it"), which is
  sufficient to prove the dispatch-routing boundary without inventing
  notification behavior the PRD doesn't ask for (e.g. `order.placed` and
  `payment.completed` would produce near-duplicate notifications with no
  spec guidance on how they should differ). `user.deleted`,
  `order.placed`, and `payment.refunded` are left unwired — see Risks.

- **This capability defines its own event payload shapes**
  (`UserCreatedEvent`, `PaymentCompletedEvent`, `MembershipEvent` — all in
  `capabilities/notifications/notifications.go`), rather than assuming some
  existing canonical payload type. No PRD-defined or kernel-defined payload
  shape exists yet for these events: payments and membership don't back a
  real `HostAPI` method surface as of this slice (see
  `pkg/sdk/manifest.go`'s `knownAPIScopes` doc comment — the same gap slice
  2.2 already documented), and `user.created` has no kernel emitter yet
  either (`internal/identity` doesn't call `host.Emit` anywhere yet). These
  types are this capability's documented expectation of what an eventual
  emitter (a future commerce/membership/identity integration) should
  populate when it starts actually emitting these events for real — a
  contract this slice's tests exercise directly by emitting instances of
  these exact types from a separate "emitter" `HostAPI`, mirroring how a
  real cross-plugin emitter would behave. A handler receiving any other
  payload shape returns a type-assertion error rather than panicking or
  silently dropping the notification (`TestWrongPayloadTypeReturnsErrorInsteadOfSendingAnything`).

- **`membership.started` and `membership.expired` share one `MembershipEvent`
  payload type** (`{Email, PlanName}`), rather than each getting its own
  near-identical struct (as the first draft did, with `MembershipStartedEvent`
  and `MembershipExpiredEvent`). Neither event needs a field the other
  doesn't as of this slice — only the rendered template differs, not the
  data — so one shared type avoids two structurally-identical types with no
  behavioral difference. Revisit (split back into two) if a later event
  needs a field the other doesn't (e.g. an expiry date on
  `membership.expired`).

- **The four event handlers funnel through one generic `dispatch[T any]`
  helper** (`notifications.go`) rather than each repeating its own
  assert-format-render-send sequence. `dispatch` type-asserts the payload to
  `T`, formats one consistent error on a type mismatch, then calls a
  `render func(T) (to, subject, body string)` and sends through the
  adapter — the only per-event code left is `dispatch(ctx, p.adapter,
  "<event>", payload, render<Event>)`, a one-line call per handler. Found
  during code review (four near-identical ~8-line handlers) and fixed
  before merge; `templates.go`'s render functions were widened to also
  return the recipient address (`to`) so `dispatch` needs no separate
  per-type field accessor.

- **`MemoryMailerAdapter` (in `capabilities/notifications/mailer.go`) is
  the only `MailerAdapter` implementation this slice ships.** It records
  every `Send` call instead of contacting a real provider — safe to use as
  a running `glyphuxd`'s placeholder wiring until a real provider adapter
  exists, and directly reusable in tests. A real deployment would supply a
  different `MailerAdapter` implementation (e.g. wrapping Resend's API)
  without this capability's dispatch logic changing at all — that
  substitutability is what proves the adapter boundary is real, per the
  spec's own framing ("the adapter BOUNDARY... is what this slice proves,
  not a live third-party integration").

- **No admin_ui/scheduled_jobs permission, no content/media api scope.**
  This capability touches nothing but the event bus and the mailer adapter
  — it registers no admin page, no content type, no scheduled job.

## Open Questions — resolved

- **Should `Register`'s four `host.On` calls fail atomically, or can a
  partial subscription succeed?** `Register` returns on the first error
  (mirroring `capabilities/forms`'s single-call `Register`), so a manifest
  missing e.g. `membership` never partially subscribes to
  `membership.started` while silently subscribing to the other three — an
  operator either gets full notification coverage or an explicit
  construction-time error, never a partially-wired plugin silently missing
  one event class.

- **Should a malformed event payload panic, propagate as an `Emit` error,
  or be silently ignored?** Propagate as an error via `errors.Join`
  (`EventBus.emit`'s existing behavior, slice 2.2) — silently ignoring a
  malformed payload would hide a real integration bug (an emitter using the
  wrong shape) from whoever is watching `Emit`'s return value, and this
  capability has no logging/audit surface of its own to fall back on
  instead.

## Files/Modules Changed

- `capabilities/notifications/mailer.go` (new) — `MailerAdapter` interface,
  `SentMessage`, `MemoryMailerAdapter` (`NewMemoryMailerAdapter`, `Send`,
  `Sent`).
- `capabilities/notifications/notifications.go` (new) — `Plugin`, `New`,
  `Manifest`, `Register`, the four event payload types, and the four
  `host.On` handler methods.
- `capabilities/notifications/templates.go` (new) — `renderWelcome`,
  `renderPaymentReceipt`, `renderMembershipStarted`,
  `renderMembershipExpired`: subject/body builders per event, kept separate
  from dispatch logic so a future richer templating mechanism (e.g.
  site-configurable copy) can replace just this file.
- `capabilities/notifications/mailer_test.go`,
  `capabilities/notifications/notifications_test.go` (new) — manifest
  validity; `Register` subscribes to all four events without error;
  `Register` denied without `users:read` (proving no bypass of `pkg/sdk`'s
  own gate); four cross-plugin dispatch tests (one per wired event), each
  constructing a real `HostAPI` for this capability and a *separate*
  emitter `HostAPI` sharing the same `sdk.KernelDeps.Bus`, then asserting
  the shared `MemoryMailerAdapter` actually received a correctly-addressed,
  non-empty notification; one malformed-payload test. (Post-review update:
  the two membership tests were repointed at the merged `MembershipEvent`
  type; no test behavior changed, only the payload type constructed.)
- Post-review follow-up (same PR, before merge): collapsed the four
  handlers' duplicated assert-format-render-send logic into one generic
  `dispatch[T any]` helper in `notifications.go`; merged
  `MembershipStartedEvent`/`MembershipExpiredEvent` into one
  `MembershipEvent`; widened `templates.go`'s render functions to also
  return the recipient address; trimmed `notifications.go`'s and
  `mailer.go`'s doc comments to point at this tracking doc instead of
  repeating its rationale inline. No behavior change — same tests, green
  before and after.
- No changes to `pkg/sdk` — every api scope (`events`, `users`, `payments`,
  `membership`) and every subscribed event's `sensitiveEvents` gating rule
  already existed from slice 2.2.
- No changes to `capabilities/seo` or any file under it (parallel slice
  3.2, out of scope here).

## Acceptance Criteria

- [x] New package `capabilities/notifications` implementing `sdk.Plugin`
      (`Manifest()`/`Register(host)`).
- [x] Subscribes to at least 2-3 of the named domain events via `host.On`
      — subscribes to 4: `user.created`, `payment.completed`,
      `membership.started`, `membership.expired`.
- [x] Dispatches through a provider-agnostic `MailerAdapter` interface,
      with a documented in-memory stub adapter usable in tests.
- [x] Manifest declares exactly the api scopes `sensitiveEvents` requires
      for each subscribed event (`users:read`, `payments`, `membership`),
      plus the baseline `events:subscribe`.
- [x] Proven via a real `HostAPI` built from real (if minimal, since this
      capability needs no store/content backing) `KernelDeps`, with a
      *separate* emitter `HostAPI` sharing `KernelDeps.Bus` emitting the
      event, mirroring slice 2.2's own cross-plugin test — not a
      same-instance self-dispatch.
- [x] Runtime tier judgment call made and documented (in-process, per
      Current Decisions above).
- [x] `go build ./... && go vet ./... && go test -race ./...` green
      repo-wide.
- [x] Tracking doc replaces the reserved 0021 stub.

## Risks

- **No real mailer provider is wired.** `MemoryMailerAdapter` is a stub;
  going to production requires a real `MailerAdapter` implementation (e.g.
  Resend for email) and, per this slice's own judgment call, is the natural
  trigger to revisit Tier C/RPC hosting if that provider's SDK is heavy
  enough to warrant out-of-process isolation.
- **No real emitter exists yet for any of these four events.** No kernel
  code (`internal/identity`, or a future `internal/commerce`/
  `internal/membership`) calls `host.Emit` for `user.created`,
  `payment.completed`, `membership.started`, or `membership.expired` as of
  this slice — commerce and membership are still-unbuilt Phase 3 slices
  (3.3/3.4 per the PRD), and no slice has wired identity's user-creation
  path to the event bus yet either. This capability's tests prove the
  dispatch-routing boundary using a hand-built emitter `HostAPI`, the same
  proof shape slice 2.2's own bus tests use; wiring `internal/identity` to
  actually emit `user.created` is explicitly out of this ticket's scope
  (it would be a change to `internal/identity`/`pkg/sdk`'s `UsersAPI`, not
  to `capabilities/notifications`).
- **`user.deleted`, `order.placed`, and `payment.refunded` are unwired.**
  Deferred, not silently dropped — see Current Decisions for the
  reasoning. Adding them later is a matter of one more payload type plus
  one more `host.On`/template pair each, following the exact pattern
  already established here.
- **No SMS/push/in-app channel, only email-shaped dispatch
  (`to/subject/body`).** The PRD names "email / SMS / push / in-app" as
  this capability's eventual full scope; `MailerAdapter`'s
  `Send(ctx, to, subject, body)` shape is email-shaped specifically. A
  multi-channel version would need either a richer adapter interface (a
  channel field, or one adapter interface per channel) or per-recipient
  channel-preference data this slice has no source for. Deferred as
  explicit future scope, consistent with the spec's own framing that the
  adapter boundary — not full multi-channel delivery — is what this slice
  proves.
- **No templating/localization system** — `templates.go`'s four render
  functions are hardcoded English strings via `fmt.Sprintf`, not
  site-configurable copy or localized per `internal/localization` (Phase 1
  slice 1.4). Fine for proving the dispatch boundary; a real product
  feature would need customizable, localized templates.
