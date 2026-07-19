# Implementation: Phase 3 slice 3.4 — `membership` capability

## Goal

PRD §14 slice 3.4 (Tier C/RPC): "gated content, subscription state,
recurring billing; depends on identity, permissions, content, notifications,
and commerce primitives." `docs/specs/phase3-4-spec.md`'s Ticket P3.4 is the
authoritative scope: `membership_tier`/`membership_subscription` content
types via `RegisterContentType`, content-gating enforcement (a NEW axis —
"does this user's active subscription satisfy a content's required tier" —
distinct from `internal/permission`'s existing role-based capability
gating), recurring billing building on `capabilities/commerce`'s
`PaymentGateway`/fake-gateway convention, and `membership.started`/
`membership.expired` event emission matching slice 2.2's already-defined
`sensitiveEvents` gating for these exact event names verbatim.

## Owning Contexts

- No `CONTEXT.md` exists yet (see `docs/agents/domain.md`); this adds one
  new package under the existing `capabilities/` directory, following
  `capabilities/forms` (slice 2.9), `capabilities/notifications` (slice
  3.1), `capabilities/seo` (slice 3.2), and `capabilities/commerce` (slice
  3.3)'s established conventions. **No changes to `pkg/sdk` were needed or
  made**: the `membership` api scope (`manage`) already existed from slice
  2.2/2.6 purely so a manifest could declare it in order to subscribe to
  `membership.started`/`membership.expired` — this slice's own `Manifest()`
  does not even need to declare it (see Current Decisions), and no other
  `pkg/sdk` surface was missing for this ticket's scope.
- One dependency already present from slice 3.3: `github.com/stripe/
  stripe-go/v81` (this slice additionally imports its `paymentintent`
  subpackage; no `go.mod`/`go.sum` change was needed since the module was
  already a direct dependency).

## Status

Complete. Built via TDD. `go build ./... && go vet ./... && go test -race
./...` green across the whole repo.

## Current Decisions

- **`userID` is an opaque string this package never resolves itself** —
  `capabilities/membership` does not import `internal/identity` or call
  `host.Users()` anywhere. `HasActiveMembership`/`Subscribe`/
  `CancelSubscription` all take a caller-supplied `userID` string and match
  it against `membership_subscription` records' own `"user_id"` field. This
  keeps the ticket's core new logic (content-gating) testable purely against
  real subscription content records, independent of a real `identity.Service`
  wiring, and keeps the two gating axes this ticket is explicit about never
  conflating (`internal/permission`'s existing role-based *capability* gate
  — which PLUGIN PACKAGE may call what — vs. this ticket's new *content*
  gate — which END USER may read a given piece of content based on THEIR
  subscription) genuinely decoupled at the type level, not just by
  convention. A real deployment would pass the stringified
  `identity.User.ID` as `userID`; nothing here depends on that being true.
- **`HasActiveMembership` is a decision PRIMITIVE, not an enforcement
  POINT** — nothing in this package intercepts an actual content-serving
  HTTP request. Wiring it into `internal/api`'s real request path (or a
  theme's rendering) is an explicit non-goal of this ticket (a separate
  frontend/transport concern), documented the same way
  `capabilities/commerce`'s `RegisterAdminPage` documented its own
  frontend-integration boundary. `gating.go`'s tests prove the LOGIC itself
  (tier-rank comparison, elapsed-period detection, cancelled/expired
  exclusion) against real subscription records built through the same
  `Subscribe`/`CancelSubscription`/`ProcessRenewal` seams a real caller would
  use — not a hand-built fixture bypassing them.
- **Tier ordering is a numeric `"rank"` field on `membership_tier`, and
  `HasActiveMembership`'s `requiredTierID` argument is a tier CONTENT ITEM
  ID, not a tier name.** A tier name is display data, not a stable ordering
  key; `rank` is this slice's explicit, queryable ordering axis, matching the
  ticket's own "at/above" framing (`t.Rank >= required.Rank`). If a user
  holds multiple subscriptions, the highest-ranked ACTIVE one determines the
  result (`TestHasActiveMembershipTrueWhenSubscribedTierRanksAboveRequired`)
  — a lapsed higher tier does not shadow an active lower one.
- **"Active" requires BOTH `status == StatusActive` AND
  `current_period_end` not yet elapsed** — `HasActiveMembership` does not
  merely trust the stored `status` field. Discovered as a real requirement
  while writing `TestHasActiveMembershipFalseWhenPeriodElapsedEvenIfStatus
  StillActive`: a subscription whose period lapsed before any renewal job
  got around to calling `ProcessRenewal` should gate as inactive the moment
  its own stored period says so, not remain trivially "active" until some
  separate background process catches up. This makes the gating logic robust
  to a renewal job that hasn't run yet, rather than silently permissive.
- **Recurring billing is a documented SIMPLIFICATION, not a real Stripe
  Billing/Subscriptions API integration** — `RecurringGateway.
  ChargeSubscription` models one isolated "attempt a charge for this
  renewal" call via a Stripe `PaymentIntent` (real `stripe-go` client,
  `paymentintent.Client`, pointed at a local fake-gateway `httptest` server —
  the same "real SDK, fake server" convention `capabilities/commerce`'s
  `StripeGateway` established for slice 3.3, applied here with
  `paymentintent` instead of `checkout/session`). Real Stripe Subscriptions
  manage their own invoice/proration/dunning-retry lifecycle server-side;
  reimplementing that surface was explicitly out of this ticket's scope per
  its own instructions ("a first vertical slice proving... success extends
  the period / failure marks it expired is the right scope"). `ProcessRenewal`
  is this vertical slice: on a successful charge it extends
  `current_period_end` by the tier's `billing_interval` and leaves the
  subscription active (no event — a renewal is not a new "start" and the
  subscription never lapsed); on a declined/failed charge it marks the
  subscription `StatusExpired` and emits `membership.expired`.
- **`billingPeriod` maps `billing_interval` to a fixed duration (30 days
  monthly, 365 days yearly), not real calendar-month arithmetic.** A
  documented simplification — `time.AddDate(0, 1, 0)`-style calendar
  arithmetic keyed to a wall-clock date would be the production-correct
  approach (a monthly renewal "on the 31st" behaves oddly with a pure
  30-day duration across months of different lengths); flagged here rather
  than silently assumed correct.
- **`membership_tier`/`membership_subscription` are each one shared content
  type**, not per-tenant or dynamically named types — mirrors
  `capabilities/forms`'s single-`form_submission`-type and
  `capabilities/commerce`'s single-`product`/`order`-type precedent:
  content types are a structural/schema concern, tier/subscription
  *instances* are runtime data.
- **No `"membership"` api scope declared in `Plugin.Manifest()`.**
  `pkg/sdk/host.go`'s `sensitiveEvents` only gates SUBSCRIBING (`On`) to
  `membership.started`/`membership.expired` on that capability; `Emit` is
  gated only on the baseline `events:emit` scope, per `Emit`'s own doc
  comment ("deliberately not gated per-event... emitting is the
  lower-risk side of the bus"). This capability only ever emits those two
  events, never subscribes to them, so declaring `"membership"` would grant
  nothing this package uses — verified directly by
  `TestSubscribeEmitsMembershipStarted`'s subscriber `HostAPI` (a SEPARATE
  manifest that DOES declare `"membership"`, since IT is doing the
  subscribing) succeeding, while `TestOnMembershipStartedDeniedWithoutMembership
  Capability` proves a subscriber lacking that declaration is refused.
- **`SubscriptionEvent{SubscriptionID, UserID, TierID}` is this package's
  OWN emitted payload shape for both `membership.started` and
  `membership.expired`** — one shared type rather than a pair, mirroring
  `capabilities/commerce`'s `PaymentEvent` and
  `capabilities/notifications`'s `MembershipEvent` precedent (neither event
  needs a field the other doesn't). This does NOT match
  `capabilities/notifications`'s own expected `MembershipEvent{Email,
  PlanName}` shape for the same two event names — a pre-existing,
  already-accepted repo-wide gap (PRD §8.4's domain events have no
  canonical cross-plugin payload shape yet; `capabilities/commerce`'s
  `PaymentEvent{OrderID}` for `payment.completed` doesn't match
  `notifications`'s own `PaymentCompletedEvent{Email, OrderID, Amount}`
  either). A real membership-emits/notifications-receives wiring today would
  need a payload-shape adapter in between; flagged in Risks, not silently
  glossed over.
- **`fakeRecurringGateway` (`fakegateway_test.go`) is duplicated/adapted
  from `capabilities/commerce/fakegateway_test.go`, not imported.** Per this
  ticket's own instruction (reuse or closely mirror commerce's pattern,
  author's call on duplicate vs. import): a `_test.go` file is unexported
  test-only code, not importable across a package boundary regardless, and
  this slice's actual API shape (`POST /v1/payment_intents`, no webhook
  delivery — `ProcessRenewal` calls the gateway synchronously, unlike
  commerce's async webhook-driven settlement) differs enough from commerce's
  checkout-session-plus-webhook shape that a shared helper would have added
  more indirection than it removed. The per-subscription outcome (succeed
  vs. decline) is configured via `SetOutcome`, looked up server-side by the
  `PaymentIntent`'s `metadata[subscription_id]` form field
  `StripeRecurringGateway.ChargeSubscription` sets.

## Open Questions — resolved

- **Should `ProcessRenewal` check `AllowsNetworkHost` itself, or trust
  `RecurringGateway` to self-police?** `ProcessRenewal` checks it, before
  calling `gateway.ChargeSubscription` at all — mirrors
  `capabilities/commerce.StartCheckout`'s identical decision (keep the
  enforcement point in this capability's own code path). Proven by
  `TestProcessRenewalRefusedAgainstDisallowedGatewayHost`: a plugin built
  with `New(nil)` (declaring no network permission) is refused against a
  gateway it was never configured to trust, before any outbound HTTP call.
- **What counts as "at or above" a required tier when a user holds several
  subscriptions?** The highest-ranked currently-ACTIVE one — see Current
  Decisions. A cancelled/expired higher-tier subscription never masks an
  active lower one, nor does it grant access it no longer entitles.
- **Does a successful renewal need to re-emit `membership.started`?** No —
  `membership.started` describes a subscription coming into existence, which
  a renewal does not change; a renewal that fails is the only follow-on
  event this ticket's spec asks for (`membership.expired`), so a successful
  `ProcessRenewal` call emits nothing.

## Files/Modules Changed

- `capabilities/membership/membership.go` (new) — `Plugin`, `New`,
  `Manifest`, `Register` (`membership_tier`/`membership_subscription`
  content types), status/interval/content-type-name constants.
- `capabilities/membership/tiers.go` (new) — `CreateTier`, internal `tier`
  parsed-view type, `getTier`/`parseTier`/`numberField` helpers.
- `capabilities/membership/subscriptions.go` (new) — `Subscribe` (+
  `SubscriptionEvent`), `CancelSubscription`, `expireSubscription` and
  `patchSubscription` shared helpers, `billingPeriod`.
- `capabilities/membership/gating.go` (new) — `HasActiveMembership`: the
  ticket's headline new content-gating logic.
- `capabilities/membership/billing.go` (new) — `RecurringGateway` interface,
  `ChargeRequest`/`ChargeResult`, `ErrGatewayHostNotAllowed`,
  `StripeRecurringGateway` (`NewStripeRecurringGateway`, `AllowlistHost`,
  `ChargeSubscription` — real `stripe-go` `paymentintent` client wiring),
  `ProcessRenewal`.
- `capabilities/membership/fakegateway_test.go` (new, test-only) —
  `fakeRecurringGateway`: an `httptest.Server` implementing `POST
  /v1/payment_intents` (real form-decoding, real JSON response shape,
  per-subscription configurable outcome via `SetOutcome`).
- `capabilities/membership/membership_test.go` (new) — full TDD suite:
  manifest validity; content-type registration; `Register` denied without
  `content:write`; tier creation + subscribe; `membership.started`/
  `membership.expired` emission verified via a separate subscriber
  `HostAPI` on the shared bus (mirrors `commerce`'s/`notifications`'s
  pattern); `On` denied without the `membership` capability declared;
  cancellation; `HasActiveMembership` across seven scenarios (active at
  required tier, subscribed tier ranks above/below required, no
  subscription, after cancellation, after period elapses despite `status`
  still active, after a failed renewal); `ProcessRenewal` success (period
  extended) and failure (expired + event emitted) against the fake gateway;
  `ProcessRenewal` refused against a disallowed gateway host.
- No changes to `pkg/sdk`, `capabilities/marketplace`, `capabilities/commerce`,
  `capabilities/notifications`, `capabilities/seo`, or `capabilities/forms`.

## Acceptance Criteria

- [x] New package `capabilities/membership` implementing `sdk.Plugin`
      (`Manifest()`/`Register(host)`), registering `membership_tier` and
      `membership_subscription` content types via `RegisterContentType`.
- [x] Content-gating enforcement: `HasActiveMembership(ctx, host, userID,
      requiredTierID) (bool, error)`, proven correct against real
      subscription records (not mocked) across active/wrong-tier/none/
      cancelled/elapsed-period/post-failed-renewal scenarios. Frontend/
      transport wiring explicitly out of scope and documented as such.
- [x] Recurring billing: `RecurringGateway`/`StripeRecurringGateway`
      (real `stripe-go` `paymentintent` client) + `ProcessRenewal`,
      proving "period elapses -> charge attempted -> success extends
      period / failure marks expired" against a local fake-gateway
      `httptest` server. Documented as a simplified simulation, not a full
      Stripe Subscriptions/Billing integration.
- [x] `membership.started`/`membership.expired` emitted via `host.Emit`,
      matching `pkg/sdk/host.go`'s `sensitiveEvents` names verbatim (no
      new/renamed events invented); cross-plugin delivery verified via a
      separate subscriber `HostAPI` on the shared event bus.
- [x] Network access to the recurring-billing gateway gated through
      `AllowsNetworkHost` (slice 2.6); a disallowed host is proven rejected
      (`TestProcessRenewalRefusedAgainstDisallowedGatewayHost`).
- [x] TDD, real SQLite-backed `KernelDeps` (mirrors
      `capabilities/forms`/`capabilities/seo`/`capabilities/commerce`), no
      mocks beyond the fake gateway server itself.
- [x] No changes to `pkg/sdk` (existing `membership: [manage]` api scope
      was already sufficient); no changes to `capabilities/marketplace`,
      `capabilities/commerce`, `capabilities/notifications`,
      `capabilities/seo`, or `capabilities/forms`.
- [x] `go build ./... && go vet ./... && go test -race ./...` green
      repo-wide.
- [x] Tracking doc replaces the reserved 0024 stub.

## Risks

- **No real content-serving-path wiring.** `HasActiveMembership` is proven
  correct in isolation; no HTTP route in `internal/api` (or a theme) calls
  it yet. A follow-up ticket must decide exactly where in the real request
  path this check belongs (middleware? a `content.API` wrapper? per-content-
  type opt-in?) — a design question this ticket deliberately left open per
  its own scope note.
- **Recurring billing is a simplified simulation, not a real Stripe
  Subscriptions/Billing integration.** No invoice objects, no proration, no
  dunning/retry schedule, no idempotency handling on a would-be webhook
  (there is none here — `ProcessRenewal` is called synchronously by
  whatever caller decides a period elapsed, not driven by an inbound
  gateway callback). A production hardening pass would very likely move to
  real Stripe Subscription objects and drive renewals off Stripe's own
  `invoice.payment_succeeded`/`invoice.payment_failed` webhooks instead of a
  caller-invoked `ProcessRenewal`.
- **`billingPeriod`'s fixed-duration approximation drifts against real
  calendar months** (30-day months, no month-length/leap-year awareness) —
  documented in Current Decisions; a production version should key off
  calendar-date arithmetic instead.
- **Emitted `SubscriptionEvent` payload shape does not match
  `capabilities/notifications`'s expected `MembershipEvent{Email,
  PlanName}` shape for the same two event names.** A pre-existing,
  already-accepted repo-wide gap (see Current Decisions) — a real
  membership-to-notifications wiring needs a payload adapter that does not
  exist yet, in either capability.
- **No admin-UI surface for tier/subscription management** — this ticket's
  spec did not ask for one (unlike `capabilities/commerce`'s
  `RegisterAdminPage`), so none was built; an operator has no UI to create
  tiers or inspect subscriptions today beyond direct `content.API`/HostAPI
  calls.
- **`ProcessRenewal` must be invoked by some external scheduler/caller** —
  this ticket built no `host.RegisterJob`-based cron trigger to call it
  automatically when a period elapses; that wiring (and whatever decides
  *when* to call it for which subscriptions) is left to a future slice.
