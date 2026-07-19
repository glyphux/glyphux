# Implementation: Phase 2 slice 2.9 — dogfood `forms` as a first-party plugin

## Goal

PRD §14 roadmap: "2.9 — Dogfood: `forms` capability as a first-party
plugin built entirely on the public API (Principle 6) — the proof the API
is real." §14's definition of done: "First-party `forms` ships through the
identical public path" as any third-party WASM/RPC plugin. This slice is
Phase 2's closing proof: everything built in slices 2.1–2.8
(Manifest/HostAPI, event bus, capability graph, WASM/RPC runtimes,
requires.core/contract + network-permission enforcement, consent engine,
audit logging) is exercised by a real, non-trivial capability that touches
none of it specially — it is just another `sdk.Plugin`.

## Owning Contexts

- (no CONTEXT.md exists yet — see docs/agents/domain.md; this adds a new
  top-level `capabilities/` directory per the PRD's own repo-tree sketch,
  plus one small, genuine gap-fill in `pkg/sdk` itself)

## Status

Complete. Built via TDD (seam confirmed with the user before any test was
written). Full repo build/vet/test -race green across all 25 packages, plus
a hand-driven program proving a *generic, forms-unaware* plugin loader can
load `forms` purely through `sdk.Plugin`/`sdk.HostAPI` and that a real
submission actually lands in the real content engine (verified by
re-reading it back through the raw `content.API`, not just trusting
`forms.Submit`'s own nil error). This closes out Phase 2 (slices 2.1–2.9,
all merged into `dev`).

## Current Decisions

- **Real gap found and fixed first: `pkg/sdk` never defined the `Plugin`
  interface itself.** Slices 2.1–2.8 built `Manifest` and `HostAPI`
  (PRD §8.3's other two pieces) but the conceptual `type Plugin interface {
  Manifest() Manifest; Register(host HostAPI) error }` sketch was never
  actually added as code — an oversight from slice 2.1, not a deferred
  scope decision (nothing in 2.1's own tracking doc calls it out as
  deferred). Added `pkg/sdk/plugin.go` (`Plugin` interface, doc comment
  only — no implementation, by design: Tier B/C plugins don't implement
  this Go interface literally, they speak the same two calls across their
  own transport, per the interface's own doc comment) via its own
  red→green TDD cycle (`plugin_test.go`'s `fakePlugin` proves the shape
  compiles and both methods are called with the right values) before
  touching `capabilities/forms` at all.

- **Scope confirmed with the user before writing any test** (per the /tdd
  skill's seam-confirmation requirement): `capabilities/forms` as a Go
  package implementing `sdk.Plugin`, tested by constructing a real
  `HostAPI` and round-tripping a real submission — NOT also wiring an HTTP
  endpoint into `internal/api` (deferred; see Risks).

- **One shared `form_submission` content type, not one type per form
  name.** A form's *name* ("contact-us", "newsletter") is runtime data
  supplied at submission time, not part of the content schema — content
  types are a structural/schema concern (PRD §11.1's "Layer-1 composition"),
  while which forms exist on a given site is exactly the kind of thing a
  site owner should be able to add/remove without a schema migration. The
  content type has two fields: `form_name` (string) and `data` (string, a
  JSON-encoded blob of whatever fields that specific form collected) —
  deliberately untyped past that point, since `forms` cannot know at
  compile time what fields any given site's forms will define. A dynamic
  per-form field schema (validating a contact form requires `email` while a
  newsletter form requires only `email`, say) is a real future capability of
  a mature forms feature, explicitly deferred here — see Risks.

- **`capabilities/forms` imports `internal/content` for exactly one reason:
  the `*content.Item` type `sdk.ContentAPI`'s own method signatures already
  return to any Tier-A (in-process, compiled Go) plugin.** This is not a
  bypass of the boundary — `forms` never imports or constructs
  `internal/content.API` itself, and every actual data operation
  (`Create`/`List`) flows through `host.Content()`, the HostAPI instance
  passed into `Register`. The alternative (returning `any`/opaque results
  from `Submit`/`List` to avoid the import entirely) was rejected as
  needlessly awkward for zero additional safety: the type is already
  exposed across the exact same boundary by `pkg/sdk` itself, to every
  Tier-A plugin, not just this one. This asymmetry (Tier-A Go plugins see
  Go types; Tier-B/C plugins never do, since they cross a real
  serialization boundary) is inherent to Tier A's definition (PRD §8.1) and
  not something this slice invented.

- **`forms.Submit`/`forms.List` are free functions, not methods on
  `*Plugin`.** A submission doesn't need the `Plugin` value at all — it
  only needs a `HostAPI` (which any caller holding a loaded plugin's host
  already has) and a form name. Making them free functions keeps `*Plugin`
  itself minimal (exactly `Manifest()`+`Register()`, nothing else) and
  mirrors how a REAL caller would use this in practice: the plugin loader
  keeps the `HostAPI` around after `Register` returns, and callers hit
  `forms.Submit(ctx, thatHost, ...)` directly, not through the `Plugin`
  value.

- **`List` filters client-side over `host.Content().List`'s full result
  set**, not via a query parameter on `ContentAPI.List` (which takes no
  filter argument at all — content.API's `List` doesn't support field-level
  filtering as of this slice). Fine for a proof-of-concept at any
  reasonable submission volume; a real production forms feature would need
  either a dedicated content-type-per-form design (rejected above) or a
  filtering capability added to `ContentAPI.List` itself (a `pkg/sdk`
  change out of scope for this slice).

## Open Questions — resolved

- **Should `forms` run through a real WASM/RPC boundary instead of Tier A,
  to more rigorously prove "identical public path"?** No — per the user's
  confirmed scope, and because `capabilities/` per the PRD's own repo-tree
  sketch is a first-party, in-process concern (alongside `commerce`,
  `membership`, etc., none of which the PRD suggests should ship as
  self-hosted WASM/RPC plugins of the kernel's own binary). "Identical
  public path" is satisfied by `forms` using ONLY `sdk.Plugin`/`sdk.HostAPI`
  — the same two-call contract a WASM or RPC plugin's host adapts its own
  transport to (see `pkg/runtime/wasm`/`pkg/runtime/rpc`'s own doc
  comments) — not by literally running `forms` inside a WASM sandbox.

## Files/Modules Changed

- `pkg/sdk/plugin.go` (new) — the `Plugin` interface (a genuine gap-fill,
  not new scope creep: PRD §8.3 already specified this shape in slice 2.1's
  own referenced section).
- `pkg/sdk/plugin_test.go` (new) — `TestPluginInterfaceIsSatisfiedByManifestAndRegister`
  (a `fakePlugin` proving the interface's two methods compile and are
  called correctly against a real `HostAPI`).
- `capabilities/forms/forms.go` (new) — `Plugin`, `New`, `Manifest`,
  `Register`, `Submit`, `List`, `formNameOf`.
- `capabilities/forms/forms_test.go` (new) — 5 tests: manifest well-formed,
  `Register` really defines the content type (verified by reloading the
  composition), submit-then-list round trip, list filters by form name
  across multiple forms, `Register` denied without `content:write` (proving
  no bypass of `pkg/sdk`'s own gate).

## Acceptance Criteria

- [x] `sdk.Plugin` interface exists and matches PRD §8.3's conceptual shape.
- [x] `capabilities/forms` implements `sdk.Plugin`, importing only
      `pkg/sdk`, `pkg/contract`, and `internal/content` (for the `Item`
      type only, as documented above) — no other kernel internals.
- [x] A generic, forms-unaware loader (`sdk.NewHostAPI` +
      `Plugin.Register`) can load `forms` with zero special-casing.
- [x] A real submission through `forms.Submit` is independently verifiable
      via the raw `content.API` — not merely "no error returned."
- [x] `Register` is denied when the host's manifest lacks `content:write`
      — `forms` carries no gate-bypass of its own.
- [x] `go build ./... && go vet ./... && go test -race ./...` green
      repo-wide.
- [x] TDD discipline followed: seam confirmed with the user before any
      test was written; red confirmed before each implementation
      (`Plugin` interface, then `capabilities/forms`).

## Risks

- **No HTTP-exposed submission endpoint** — per the user's confirmed scope,
  this slice proves the Go-level `sdk.Plugin`/`HostAPI` contract is
  sufficient, not that forms are network-submittable yet. A future slice
  would wire a route (e.g. `POST /api/v0/forms/{name}/submit`) into
  `internal/api`, calling `forms.Submit` under the hood.
- **No per-form field schema/validation** — every form shares one untyped
  `data` blob; a real product feature would need a way to declare (and
  validate against) each form's own field list. Explicitly deferred, not
  silently dropped — see Current Decisions.
- **`List`'s client-side filtering does not scale** — fine for this proof;
  a real deployment with many submissions per form would need either a
  `ContentAPI.List` filter parameter or a per-form content type, both out
  of scope here.
- **This slice does not exercise the WASM/RPC runtimes at all** — `forms`
  is Tier A. The PRD's "identical public path" claim is satisfied at the
  `sdk.Plugin`/`HostAPI` contract level (see Open Questions), not by
  routing `forms` through `pkg/runtime/wasm`/`pkg/runtime/rpc`; a reader
  wanting proof of the WASM/RPC boundaries specifically should look at
  those slices' own tracking docs (0015/0016), which each already exercise
  a real (non-forms) guest/subprocess plugin end to end.
