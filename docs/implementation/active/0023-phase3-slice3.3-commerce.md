# Implementation: Phase 3 slice 3.3 — `commerce` capability

## Goal

PRD §14 slice 3.3 (Tier C/RPC): "product & order content types, checkout
components, payment events, admin pages, allowlisted payment-gateway
network. Validates the heavy RPC tier and payment-scope isolation."
`docs/specs/phase3-4-spec.md`'s Ticket P3.3 is the authoritative scope:
product/order content types via `RegisterContentType`, a checkout flow,
`order.placed`/`payment.completed`/`payment.refunded` event emission
(matching slice 2.2's already-defined `sensitiveEvents` gating for these
exact event names verbatim), admin pages for order management, and payment
processing against a **local fake-gateway test server** — a firm,
user-confirmed decision, not a live integration. Network access goes
through slice 2.6's `AllowsNetworkHost`.

## Owning Contexts

- No `CONTEXT.md` exists yet (see `docs/agents/domain.md`); this adds one
  new package under the existing `capabilities/` directory, following
  `capabilities/forms` (slice 2.9), `capabilities/notifications` (slice
  3.1), and `capabilities/seo` (slice 3.2)'s established conventions. No
  changes to `pkg/sdk` were needed or made: `content`, `events`, `payments`
  api scopes, the `admin_ui`/`network` permissions, and all three payment
  event names' `sensitiveEvents` gating rules already existed from slice
  2.2/2.6.
- One new external dependency: `github.com/stripe/stripe-go/v81` (see
  Current Decisions below for why).

## Status

Complete. Built via TDD. `go build ./... && go vet ./... && go test -race
./...` green across the whole repo (including `internal/api`,
`internal/graphql`, `pkg/client` — the slower packages — all passing).
`go mod tidy` run after adding stripe-go; `go.sum` consistent.

## Current Decisions

- **Real `github.com/stripe/stripe-go/v81` SDK client code, exercised
  against a local fake-gateway `httptest` server — not a hand-rolled HTTP
  client, and not a mocked SDK.** This was the ticket's own highest-judgment
  call. Reasoning: stripe-go's checkout-session client
  (`checkout/session.Client`) and its backend abstraction
  (`stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL:
  ...})`) are built precisely to let a caller point the *real* client at a
  *different* base URL — this is a first-class, documented seam in the SDK
  itself, not a hack. Pointing that real client at
  `capabilities/commerce/fakegateway_test.go`'s `fakeGateway` (a genuine
  `httptest.NewServer`) means every checkout-session-creation request in
  this package's tests is real HTTP, real form-encoding
  (`application/x-www-form-urlencoded`, exactly as stripe-go's
  `BackendImplementation.Call` sends it), and a real JSON response decode —
  the fake server only supplies the *response body*, never a mocked method
  call. Webhook verification is even more literal: `StripeGateway.ParseWebhook`
  calls the real `github.com/stripe/stripe-go/v81/webhook.ConstructEvent`,
  the exact HMAC-SHA256 signature-verification code a production
  integration runs, against a payload the fake gateway signs with the real
  `webhook.GenerateTestSignedPayload` helper and delivers over a real
  outbound HTTP POST to a merchant-side `httptest` receiver. A hand-rolled
  HTTP client against a bespoke shape would have been *easier* to write
  (no need to satisfy `stripe.CheckoutSessionParams`'s full form-encoding
  contract or match Stripe's real JSON field names) but would prove nothing
  about a real integration's client-side code path — the explicit thing
  this ticket's spec asks to avoid mocking away. stripe-go was not already
  a dependency; adding it was proportionate given the ticket explicitly
  named it as the preferred path and its backend-URL override made the
  "real SDK, fake server" shape possible with no source modification to
  stripe-go itself.
- **`PaymentGateway` interface (`capabilities/commerce/gateway.go`) is the
  capability's own provider-adapter boundary**, mirroring
  `capabilities/notifications`'s `MailerAdapter` pattern from slice 3.1:
  `checkout.go`/`webhook.go` (this capability's own logic) never import
  `stripe-go` directly, only this interface. `StripeGateway` is the one real
  implementation this slice ships. A future gateway (PayPal, a regional
  provider) would implement the same three methods
  (`CreateCheckoutSession`/`ParseWebhook`/`AllowlistHost`) without this
  capability's checkout/webhook logic changing at all.
- **In-process `sdk.Plugin` (Tier A), not a `pkg/runtime/rpc`-hosted Tier
  C plugin**, despite the PRD's own sketch naming Tier C for this
  capability ("validates the heavy RPC tier"). Standing up gRPC
  broker/subprocess plumbing to host this vertical slice's actual behavior
  (content-type registration, checkout, webhook processing) would add
  transport machinery without exercising any additional behavior an
  in-process implementation doesn't already prove — the same judgment call
  slice 3.1's tracking doc made for `notifications`, applied here for the
  identical reason (no real out-of-process dependency needs isolating; the
  *heaviness* Tier C is meant for is a real Stripe SDK's dependency graph,
  which this slice already pulls in-process without issue). Revisit if a
  future slice's payment-gateway dependency set grows heavy/untrusted
  enough to be worth isolating out-of-process.
- **One shared content type each for `product` and `order`**
  (`capabilities/commerce/commerce.go`), not per-tenant or dynamically
  named types — mirrors `capabilities/forms`'s single-`form_submission`-type
  precedent (slice 2.9): content types are a structural/schema concern,
  product/order *instances* are runtime data.
- **Order-management admin page: `RegisterAdminPage` only, no admin-ui
  React page.** `Plugin.Register` calls `host.RegisterAdminPage` with slug
  `commerce-orders` — proving the `admin_ui`-gated registration call
  succeeds and is recorded server-side. Building the actual React page that
  would render at that slug is an explicit non-goal of this ticket (a
  separate frontend concern, per the ticket's own scope note) and is
  deferred — see Risks.
- **`order.placed` is emitted by `CreateOrder` (order creation), not by
  `StartCheckout`.** An order existing at all — regardless of whether
  checkout has even been initiated yet — is the fact `order.placed`
  describes; a cart/order can legitimately be placed and only later proceed
  to checkout. `payment.completed`/`payment.refunded` are emitted only by
  the webhook handler (`webhook.go`), the sole place this capability learns
  a payment outcome actually happened.
- **Order-ID recovery on webhook events reuses the checkout session's
  `client_reference_id` field for BOTH `checkout.session.completed` and
  `charge.refunded` events** (`StripeGateway.ParseWebhook`,
  `fakegateway_test.go`'s `deliverWebhook`). Real Stripe's `charge` object
  has no `client_reference_id` (that field lives only on the Checkout
  Session object); recovering an order ID from a real refund event would
  need a `PaymentIntent`/Session lookup this slice doesn't implement. The
  fake gateway round-trips the same key on its synthetic `charge.refunded`
  payload so `ParseWebhook`'s extraction logic stays uniform — documented
  as a simplification specific to this vertical slice, not a claim about
  real Stripe's actual charge-object shape. A production hardening pass
  would resolve the order ID via a session lookup keyed on the charge's
  `payment_intent`, or by having `StartCheckout` also stash the checkout
  session ID *as* Stripe metadata queryable independent of the charge.
- **`patchOrder` (`checkout.go`) fetches-merges-writes rather than calling
  `host.Content().Update` with a partial map directly.** Discovered during
  TDD: `internal/content.API.Update`'s `validate` call checks the *entire*
  data map passed to it against the order content type's required fields,
  not just the keys being changed — so a naive partial-field `Update` call
  (e.g. writing only `checkout_session_id`) fails validation for every
  other required field it doesn't happen to touch. `patchOrder` is the one
  seam both `StartCheckout` (writes `checkout_session_id`) and
  `HandleWebhook` (writes `status`) go through, rather than each
  reimplementing fetch-merge-write.
- **Unrecognized webhook event types are accepted, not errored** (see
  `HandleWebhook`'s doc comment) — an authenticated-but-unhandled callback
  (a real gateway may deliver event types this slice has no logic for) is a
  legitimate no-op, not a failure worth surfacing as an error to the
  gateway's retry logic.

### Post-review fixes (same PR, before merge)

An independent `/code-review` audit against this PR came back clean on the
Spec axis; three Standards findings were fixed before merge:

- **`HandleWebhook`'s two branches (`checkout.session.completed` /
  `charge.refunded`) were duplicating the same
  patch-status-then-emit-then-wrap-error sequence**, differing only in the
  target status, event name, and (before this fix) payload type. Extracted
  `transitionOrder(ctx, host, orderID, status, eventName string) error`
  (`webhook.go`) — both branches are now a single `return
  transitionOrder(...)` call. This is the same "go one level further than
  `patchOrder`" pattern the reviewer pointed at.
- **`PaymentCompletedEvent`/`PaymentRefundedEvent` were structurally
  identical** (`{OrderID string}`). Merged into one `PaymentEvent{OrderID
  string}` used for both "payment.completed" and "payment.refunded" —
  `capabilities/notifications` (slice 3.1, already merged) established
  exactly this precedent for `MembershipStartedEvent`/`MembershipExpiredEvent`
  → `MembershipEvent`, for the identical reason ("neither event needs a
  field the other doesn't"). Revisit (split back into two) if a later need
  arises for a field one event carries that the other doesn't (e.g. a
  refund reason/amount `payment.completed` has no counterpart for).
- **`StartCheckout` took six loose positional parameters
  (`orderID, amountCents, currency, productName, successURL, cancelURL`)
  that were immediately reassembled into a `CheckoutRequest` inside the
  function body.** Since `CheckoutRequest` already exists and bundles
  exactly these fields, `StartCheckout` now accepts a `CheckoutRequest`
  directly (`StartCheckout(ctx, host, gateway, req CheckoutRequest)`); the
  no-longer-needed internal reassembly is gone.

No behavior change — same tests (updated to the new call shapes), green
before and after. The reviewer's non-required suggestion (a `Money{Cents
int64; Currency string}` type to stop `amountCents`/`currency` traveling as
a raw pair across `CreateProduct`/`CreateOrder`/`StartCheckout`/
`CheckoutRequest`/`OrderPlacedEvent`) was left as a documented future
cleanup rather than applied now — it's a larger, more invasive change
touching many signatures for a naming/grouping improvement with no
behavioral payoff, better done deliberately in its own pass than folded
into a post-review fixup.

## Open Questions — resolved

- **Should `StartCheckout` check `AllowsNetworkHost` itself, or should
  `PaymentGateway` be trusted to check it internally?** `StartCheckout`
  checks it (`checkout.go`), before calling `gateway.CreateCheckoutSession`
  at all — keeps the enforcement point in this capability's own code path
  (matching the ticket's framing: "network access from this capability must
  go through `AllowsNetworkHost`"), rather than trusting every current and
  future `PaymentGateway` implementation to remember to self-police. A
  disallowed-host call never reaches the gateway's `CreateCheckoutSession`
  at all, proven by `TestStartCheckoutRefusedAgainstDisallowedGatewayHost`.
- **How should the manifest's declared network-permission host be derived?**
  `Plugin.Manifest()` calls `p.Gateway.AllowlistHost()` — the same gateway
  instance the plugin was constructed with — rather than requiring a
  caller to separately specify the host in two places (once when building
  the gateway, again when building the manifest) with no guarantee they'd
  ever match. `StripeGateway.AllowlistHost()` derives it from the same
  base URL passed to `NewStripeGateway`, so gateway and manifest can never
  drift apart.
- **Should `charge.refunded`'s order-ID recovery differ from
  `checkout.session.completed`'s?** No — see Current Decisions above; both
  read the same `client_reference_id` key for this slice's scope, with the
  real-Stripe caveat documented rather than silently glossed over.

## Files/Modules Changed

- `capabilities/commerce/commerce.go` (new) — `Plugin`, `New`, `Manifest`,
  `Register` (product/order content types, `commerce-orders` admin page),
  content-type/status name constants.
- `capabilities/commerce/gateway.go` (new) — `PaymentGateway` interface,
  `CheckoutRequest`/`CheckoutSession`/`WebhookEvent` types,
  `ErrGatewayHostNotAllowed`, `StripeGateway` (`NewStripeGateway`,
  `AllowlistHost`, `CreateCheckoutSession`, `ParseWebhook`) — the real
  `stripe-go` client wiring.
- `capabilities/commerce/checkout.go` (new) — `CreateProduct`,
  `CreateOrder` (+ `OrderPlacedEvent`), `StartCheckout` (post-review:
  takes a `CheckoutRequest` directly instead of six loose params),
  `patchOrder` helper.
- `capabilities/commerce/webhook.go` (new) — `HandleWebhook`, `PaymentEvent`
  (post-review: merged from the initial `PaymentCompletedEvent`/
  `PaymentRefundedEvent` pair), `transitionOrder` helper (post-review:
  extracted from `HandleWebhook`'s two duplicated branches).
- `capabilities/commerce/fakegateway_test.go` (new, test-only) —
  `fakeGateway`: an `httptest.Server` implementing
  `POST /v1/checkout/sessions` (real form-decoding, real JSON response
  shape) and `CompleteSession`/`RefundSession` (real signed webhook
  delivery via `webhook.GenerateTestSignedPayload`) to a configurable
  merchant webhook URL.
- `capabilities/commerce/commerce_test.go` (new) — full TDD suite: manifest
  validity; network-allowlist declaration; content-type/admin-page
  registration; `Register` denied without `content:write` /
  `admin_ui`; product+order creation with cross-plugin `order.placed`
  verification (separate subscriber `HostAPI` on the shared bus, mirroring
  `notifications`'s pattern); checkout against the fake gateway (session
  returned, session ID persisted on the order); checkout refused against a
  gateway host absent from the manifest's network allowlist (proven via one
  fake-gateway server reached under two hostnames, `127.0.0.1` vs
  `localhost`, so the refusal is provably the permission gate and not an
  incidental connection failure); webhook-driven `payment.completed` /
  `payment.refunded` each verified via a separate subscriber `HostAPI`;
  bad-signature webhook rejection.
- `go.mod`/`go.sum` — added `github.com/stripe/stripe-go/v81` (direct
  dependency); `go mod tidy` run, `go.sum` consistent.
- No changes to `pkg/sdk`, `capabilities/forms`, `capabilities/notifications`,
  or `capabilities/seo`.

## Acceptance Criteria

- [x] New package `capabilities/commerce` implementing `sdk.Plugin`
      (`Manifest()`/`Register(host)`), registering `product` and `order`
      content types via `RegisterContentType`.
- [x] Checkout flow (`StartCheckout`) against a real payment-gateway client
      (`StripeGateway`, real `stripe-go`), producing a real hosted checkout
      session.
- [x] `order.placed`/`payment.completed`/`payment.refunded` emitted via
      `host.Emit`, matching `pkg/sdk/host.go`'s `sensitiveEvents` names
      verbatim (no new/renamed events invented).
- [x] Admin page for order management registered via
      `host.RegisterAdminPage` (`admin_ui`-gated); explicitly documented
      that the admin-ui React page itself is out of this ticket's scope.
- [x] Payment gateway: local fake-gateway `httptest` server, not a live
      network integration; real `stripe-go` SDK client code exercised
      against it (checkout-session creation + signed webhook delivery),
      per this ticket's firm, user-confirmed decision. Judgment call
      documented above.
- [x] Network access gated through `AllowsNetworkHost` (slice 2.6);
      manifest declares the fake gateway's host in a `network` permission;
      a disallowed host is proven rejected
      (`TestStartCheckoutRefusedAgainstDisallowedGatewayHost`).
- [x] TDD, real SQLite-backed `KernelDeps` (mirrors
      `capabilities/forms`/`capabilities/seo`), no mocks beyond the fake
      gateway server itself.
- [x] Cross-plugin event verification via a separate subscriber `HostAPI`
      on the shared event bus (mirrors `notifications`'s pattern), for all
      three events.
- [x] `go build ./... && go vet ./... && go test -race ./...` green
      repo-wide; `go mod tidy` run, `go.sum` consistent.
- [x] Tracking doc replaces the reserved 0023 stub.

## Risks

- **No real admin-ui React page for order management.** Only the
  server-side `AdminPageDef` registration exists; an operator has no actual
  UI to view/manage orders yet. Explicitly out of this Go-package ticket's
  scope per the spec; building it is a separate frontend-track ticket.
- **No live payment gateway integration.** `StripeGateway` has only ever
  been exercised against this package's own fake gateway server — going to
  production requires real Stripe (or another provider's) credentials, a
  real webhook-signing secret from a real dashboard, and re-verifying the
  same client code against the real API (which the backend-URL-override
  design makes a config change, not a code change).
- **Refund order-ID recovery is a documented simplification** (see Current
  Decisions) that would not work unmodified against a real Stripe
  `charge.refunded` event, which carries no `client_reference_id`. A
  production hardening pass needs a real session/PaymentIntent lookup.
- **No idempotency handling on webhook delivery.** A gateway redelivering
  the same event (real gateways retry on non-2xx) would reapply
  `patchOrder`'s status write and re-`Emit` the event; harmless for a
  same-value status write, but a genuine idempotency key check
  (`event.ID` dedup) is not implemented and would be needed before
  production use.
- **No refund/failure distinction.** `charge.refunded` is the only
  "payment didn't succeed" path wired; a distinct outright payment-failure
  event (as opposed to a refund of a previously-successful payment) isn't
  modeled separately — both would currently need to arrive as
  `charge.refunded`-shaped callbacks to reach `payment.refunded`.
- **Currency/amount stored as opaque cents integers with no currency-aware
  validation** (e.g. no check that `currency` is a supported ISO code
  beyond what the gateway itself would reject). `amountCents`/`currency`
  also travel as a raw pair across several signatures
  (`CreateProduct`/`CreateOrder`/`StartCheckout`'s `CheckoutRequest`/
  `OrderPlacedEvent`) rather than one `Money{Cents int64; Currency string}`
  type — flagged in code review as worth a look but deliberately deferred
  as a larger, more invasive follow-up rather than folded into this PR's
  fixups (see Current Decisions' Post-review fixes note).
