# Spec: Phase 3 (First-Party Capabilities) + Phase 4 (Layout Composition + Visual Builder)

Manually derived breakdown of `docs/glyphux-prd.md` §14 (Phase 3) and §14
(Phase 4) into implementable tickets, in lieu of `/to-spec`/`/to-tickets`
(not available as skills in this environment). Phase 5 is explicitly
excluded — the PRD marks it "far future, gated... deferred until Phases 1–4
are in production and demand is proven by external users," not committed
work for this round.

Scope decisions confirmed with the user before this doc was written:
- Phase 3's optional/enhancement slices (3.6 `ai`, 3.7 `stock-media`) are
  **deferred**, not built in this round.
- Commerce (3.3)'s payment-gateway integration is built against a **local
  fake-gateway test server** replicating the real gateway's (Stripe-shaped)
  checkout-session + webhook API — no live credentials used or required.
- A **dedicated design-system slice** runs first, before Phase 3's
  admin-page-building slices and before Phase 4, so both consume one
  reusable component library rather than each inventing ad hoc UI.
- Pacing: sequential within each phase, parallel across independent slices
  at each stage (see Sequencing below).
- Every ticket's implementation is delivered as a PR onto `dev` (never a
  direct merge) and passes an independent `/code-review` audit — findings
  verified firsthand, not trusted from the ticket's own summary — before
  merging.

## Sequencing

```
Stage 0  — Design System (solo)                         [ticket DS]
Stage 1  — 3.1 notifications  ‖  3.2 seo                 [tickets P3.1, P3.2]
Stage 2  — 3.3 commerce (solo)                           [ticket P3.3]
Stage 3  — 3.4 membership  ‖  3.5 marketplace            [tickets P3.4, P3.5]
Stage 4  — 4.1 layout contract + 4.2 block registration (solo, foundation) [ticket P4.1]
Stage 5  — 4.3 server-side layout rendering (solo)       [ticket P4.3]
Stage 6  — 4.4 visual builder (solo, largest)            [ticket P4.4]
Stage 7  — 4.5 live preview ‖ 4.6 presets/bundles ‖ 4.7 marketplace distribution ‖ 4.9 media picker
                                                          [tickets P4.5, P4.6, P4.7, P4.9]
(4.8 AI authoring deferred — depends on the deferred 3.6 `ai` capability)
```

Each stage only starts after the previous stage's PR(s) are merged into
`dev` and independently re-verified — the same discipline used for Phase 2.

## Ticket DS — Design System

**PRD anchor:** §5.6/§5.7 (admin shell's design-token system, component
library, accessibility floor, established in Phase 1 slice 1.13); no
dedicated PRD slice number — this ticket is the user-confirmed prerequisite
for Phase 3/4's "premium UI, reusable components" requirement.

**Scope:** Audit `admin-ui`'s existing component library (from slice 1.13)
and extend it into a fuller, documented design system:
- Design tokens: spacing scale, typography scale, color palette (light/dark,
  brandable accent — already partially established in 1.13; extend, don't
  replace).
- A documented, reusable component set covering at minimum: buttons (primary/
  secondary/destructive/ghost), form inputs (text/select/checkbox/radio/
  textarea) with consistent validation-error states, data tables (sortable,
  paginated), modals/dialogs, toasts/notifications, tabs, cards, badges/pills,
  empty states, loading/skeleton states.
- Accessibility floor: keyboard navigation, focus states, ARIA labels,
  color-contrast compliance (WCAG AA) — verified, not assumed.
- Storybook or an equivalent lightweight component-preview surface (judgment
  call for the implementing agent to make and document) so new admin pages
  (Phase 3's commerce/membership/marketplace, Phase 4's builder) can discover
  and reuse existing components instead of re-implementing them.

**Definition of done:** every component has a test (render + interaction
where applicable); existing admin-ui pages (content, users, media) are
migrated to use the new/extended component set where it's a drop-in
replacement (judgment call: full migration vs. new-pages-only, document the
choice); `npm run build` and the existing admin-ui test suite stay green.

## Ticket P3.1 — `notifications` capability

**PRD anchor:** §14 slice 3.1 (Tier C/RPC): "email / SMS / push / in-app,
over events + a mailer adapter... Built early because commerce and
membership depend on it. Validates the RPC tier with a low-risk,
broadly-useful capability." §7.2: `notifications → events, mailer`.

**Scope:** `capabilities/notifications` (or `pkg/runtime/rpc`-hosted RPC
plugin — implementing agent's call on Tier C wiring, informed by slice
2.5's existing `pkg/runtime/rpc` broker) subscribing to domain events
(`user.created`, `order.placed`, `payment.completed`, `membership.started`,
etc. — whichever are wired so far) and dispatching through a provider-
agnostic mailer adapter interface (a documented stub/local adapter is
acceptable — no live email/SMS/push provider credentials available in this
environment; the adapter boundary is what matters, matching the commerce
gateway judgment call).

## Ticket P3.2 — `seo` capability

**PRD anchor:** §14 slice 3.2 (Tier B/WASM): "content-derived meta,
sitemaps, structured data. Low-risk, validates the WASM tier under real
load." §7.2: `seo → content`.

**Scope:** A Tier-B (WASM, per slice 2.4's `pkg/runtime/wasm`) or
demonstrably-real capability generating meta tags/sitemap/structured data
(JSON-LD) from existing content items via the public `ContentAPI` — no
kernel-internal access. Real generated output validated against actual
content fixtures (e.g. a real sitemap.xml is well-formed XML, a real
JSON-LD block validates against schema.org's `Article`/similar shape).

## Ticket P3.3 — `commerce` capability

**PRD anchor:** §14 slice 3.3 (Tier C/RPC): "product & order content types,
checkout components, payment events, admin pages, allowlisted
payment-gateway network. Validates the heavy RPC tier and payment-scope
isolation."

**Scope:** Product/order content types (via `RegisterContentType`),
checkout flow, `order.placed`/`payment.completed`/`payment.refunded` event
emission (matching slice 2.2's already-defined `sensitiveEvents` gating for
these exact event names), admin pages for order management, and payment
processing against a **local fake-gateway test server** (this ticket builds
that fake server as part of its own test infrastructure, replicating a
real gateway's checkout-session-creation + webhook-callback API shape
closely enough that the real gateway SDK's client code — not a hand-rolled
HTTP client — is what's under test). Network access is allowlisted per
slice 2.6's `AllowsNetworkHost` mechanism.

## Ticket P3.4 — `membership` capability

**PRD anchor:** §14 slice 3.4 (Tier C/RPC): "gated content, subscription
state, recurring billing; depends on identity, permissions, content,
notifications, and commerce primitives." §7.2:
`membership → identity, permissions, content, notifications`.

**Scope:** Subscription/tier content types, content-gating enforcement
(a piece of content requires an active membership tier to access — this is
new enforcement logic, not existing permission-role gating), recurring
billing building on commerce's (P3.3) payment primitives, `membership.
started`/`membership.expired` event emission (already gated in slice 2.2).

## Ticket P3.5 — `marketplace` capability

**PRD anchor:** §14 slice 3.5: "packaging, signing, versioning,
multi-vendor... includes the paid-extension protection model (§12.5):
signing, offline-verifiable entitlement tokens, update-gating, and graceful
expiry — no source DRM, no phone-home."

**Scope:** Read §12.5/§12.6 in full before starting (not reproduced here —
this ticket's implementing agent must read those sections directly).
Package signing/verification, entitlement token issuance/verification
(offline-verifiable — no phone-home to a licensing server), update-gating
by entitlement status, graceful expiry handling.

## Ticket P4.1 — Layout Composition contract + block registration

**PRD anchor:** §14 slices 4.1+4.2: "blocks, slots, regions, sections —
additive to Layer 1, never polluting it" + "plugins/themes register blocks
via the public API (§13.2); first-party blocks ship in-tree, third-party
blocks arrive as plugins."

**Scope:** The `layout-composition/v1` contract (new types in `pkg/contract`,
additive alongside the existing `content-composition/v0`), and wiring
`RegisterBlock` (already stubbed in slice 2.1's `pkg/sdk/host.go`) into a
real block registry with real first-party blocks shipped in-tree.

## Ticket P4.3 — Server-side layout rendering

**PRD anchor:** §14 slice 4.3: "themes render Layer-2 composition." §9
(read in full — 9.1 themes never mutate composition; 9.2 theme
capabilities: declares content types/layouts rendered, receives a
read-only typed composition view, emits output, may declare slots/regions;
9.3 rendering contract: the typed interface between resolved composition
and a theme, with a built-in "headless" theme emitting JSON only, proving
the platform is fully usable headlessly; V1 target is server-side Go
templating + static assets).

**Corrected scope (discovered when this ticket was picked up): no theme
system exists at all yet** — Phase 1 deliberately shipped "no renderer, no
builder" (§14's own Phase 1 goal text), so §9's contract has never been
built, not merely something to "extend." This ticket therefore builds, from
scratch:
- A new public `pkg/theme` package: a `Theme` interface every theme
  implements, and a read-only `CompositionView` type combining resolved
  Layer-1 content (via the existing `content.API`/`ContentAPI` shape) with
  an optional Layer-2 `contract.Layout` for the current route — no mutation
  methods anywhere on the view, per §9.1's hard law.
- `themes/headless` (new top-level package, sibling to `capabilities/`):
  the built-in default theme emitting the view as JSON — the "fully usable
  headlessly" guarantee.
- Server-side rendering of Layer-2 blocks/slots/regions through the
  contract, proven against the real `pkg/blocks` registry and first-party
  blocks from Ticket P4.1 (`blocks/firstparty`) — the actual behavior this
  ticket must prove end to end, not just a type shape.
- User-confirmed scope: the HTML/Go-template "starter" reference theme
  (§9.3's other V1 target) IS in scope for this ticket alongside the
  headless JSON theme — build both, not headless-only.

## Ticket P4.4 — Visual builder (client)

**PRD anchor:** §14 slice 4.4 — the largest single slice: "nested,
multi-target, rule-based drag-and-drop editing of the composition through
the public contract... built on dnd-kit primitives with a page-builder
framework (Craft.js or Puck) supplying the editor state model, node tree,
and serialization... an extension of the admin shell, not a separate app."

**Corrected scope (discovered when this ticket was picked up): split into
two sequential sub-tickets, user-confirmed.** No HTTP transport exists yet
for either the block registry (`pkg/blocks`) or Layer-2 `contract.Layout`
documents — only Layer-1 content/media/composition have `/api/v0/...`
routes (see `internal/api/api.go`). The builder cannot be "using only the
public JS SDK" (the ticket's own words) if the JS SDK has nothing to call.

### Ticket P4.4a — Layout/block transport (solo, foundation)

Mirror the existing content/media transport pattern in `internal/api`:
- `GET /api/v0/blocks` — lists every registered block `Definition` (name,
  display name, prop schema, slots) from the shared `pkg/blocks.Registry`.
- `GET /api/v0/layouts/{route}` / `PUT /api/v0/layouts/{route}` — load/save
  a Layer-2 `contract.Layout` document for a named route, validated via
  both `Layout.Validate()` (structural) and `blocks.ValidateLayout()`
  (registry-existence) before being persisted, matching the existing
  `content-types` PUT route's validate-before-persist pattern.
- A real persistence layer (a new DB-backed store, following the existing
  `internal/composition`/`internal/content` store conventions — check the
  current highest migration version before picking a new one, this has
  bitten the project multiple times already).
- `sdk-js` client methods for both new endpoint groups, following its
  existing generated/typed-client conventions (check `sdk-js/src/` for the
  pattern content/media already use).

### Ticket P4.4b — Visual builder UI (depends on P4.4a)

The visual builder UI in `admin-ui`, consuming Ticket DS's design system,
P4.4a's new `sdk-js` methods, and rendering via P4.3's contract — using
only the public JS SDK, holding no privileged access. Built on dnd-kit
primitives (already a dependency, added ahead of time in slice 1.13) with
a page-builder framework (Craft.js or Puck) supplying the editor state
model, node tree, and serialization to/from `contract.Layout`.

## Tickets P4.5–P4.9 — parallel batch after the builder lands

- **P4.5 — Live preview:** editor renders the same composition the API
  serves.
- **P4.6 — Composition presets & bundles + compatibility contract (§13):**
  read §13.3 in full — missing-block detection, import-time compatibility.
- **P4.7 — Marketplace distribution of builder artifacts (§13.4):** signed
  packages for blocks/presets/bundles/themes, building on P3.5's signing
  infrastructure.
- **P4.9 — In-builder media picker:** extends the Phase-1 media library
  into the builder; stock-media integration deferred (3.7 deferred).

(4.8 AI authoring in the builder is deferred — it depends on the deferred
3.6 `ai` capability.)

## Cross-cutting requirements (every ticket)

- TDD per the `/tdd` skill: seams confirmed, behavior-first tests, real
  dependencies not mocks, one behavior per cycle.
- `go build ./... && go vet ./... && go test -race ./...` green repo-wide
  (plus `npm test`/`npm run build` for any `admin-ui`/`sdk-js` changes)
  before the ticket's PR is opened.
- A tracking doc in `docs/implementation/active/`, following the
  established numbered convention (next number: 0020), moved to
  `completed/` only after the PR merges.
- **PR onto `dev`, never a direct merge** — the implementing agent opens
  the PR and stops; the parent session runs `/code-review` against the
  diff, independently verifies every finding (reads the actual code, runs
  the actual tests), and either merges or pushes fixes back to the agent.
- Per-slice production-readiness gate (PRD §17.1): functionality complete
  end-to-end, tests at the domain-API boundary, capability scoping enforced
  at the boundary, structured logging where applicable, migration baseline
  updated, `boundary-verify` still green.
