# Implementation: Phase 3 slice 3.2 — `seo` capability

## Goal

PRD §14 roadmap: "3.2 — `seo` capability (Tier B/WASM): content-derived
meta, sitemaps, structured data. Low-risk, validates the WASM tier under
real load." §7.2's capability dependency graph: `seo → content`. Per
`docs/specs/phase3-4-spec.md`'s "Ticket P3.2": generate meta tags, a
sitemap, and structured data (JSON-LD) from existing content items via the
public `ContentAPI` only — no kernel-internal access — with real generated
output validated against real content fixtures (well-formed XML, valid
JSON-LD with the required `Article` fields), not just "no error returned."

## Owning Contexts

- (no `CONTEXT.md` exists yet — see `docs/agents/domain.md`; this adds one
  new package under the existing `capabilities/` top-level directory the
  PRD's own repo-tree sketch already reserves for `seo`, per slice 2.9's
  precedent. No changes to `pkg/sdk` were needed or made.)

## Status

Complete. Built via TDD: `capabilities/seo/seo_test.go` was written first
(confirmed red — the package didn't exist yet, a build failure) before
`capabilities/seo/seo.go` was implemented; all 7 tests passed on the first
real implementation attempt. Full repo `go build ./... && go vet ./... &&
go test -race ./...` green.

## Current Decisions

- **Built as a Tier-A `sdk.Plugin` (in-process Go), not a real WASM guest —
  a deliberate call, made explicit per the spec doc's own permission to do
  so.** The PRD tags this slice "Tier B/WASM," and slice 2.4's tracking doc
  confirms a real Rust→wasm32 toolchain is available in this environment.
  The call here is that `seo`'s entire job is deterministic
  string/XML/JSON generation from data already crossing the `HostAPI`
  boundary (`ContentAPI.List`/`Get`) — there is no privileged operation a
  WASM sandbox would meaningfully constrain beyond what the `content:read`
  scope gate already constrains, and no untrusted third-party code is
  involved (this is a first-party capability, exactly like `forms`). Per
  the spec's own "Low-risk, validates the WASM tier under real load"
  framing, the WASM-tier validation is better served by a slice that
  actually needs isolation (e.g. a genuinely untrusted or resource-heavy
  workload) than by retrofitting one onto `seo`'s pure-function shape. If a
  later slice needs `seo` to run as a real WASM guest for defense-in-depth
  or a marketplace-distributed variant, this package's public functions
  (`GenerateMetaTags`, `GenerateStructuredData`, `GenerateSitemap`) can be
  wrapped by a WASM guest without changing their signatures — the same
  "sdk.Plugin/HostAPI contract is what's identical, not the literal
  runtime" argument slice 2.9's tracking doc already made for `forms`.

- **`seo` declares only `content:read`, no `write`/`publish`.** Per PRD
  §7.2 (`seo → content`) and the capability's actual job: it only ever
  derives output from content someone else created/published, it never
  writes. This is a narrower grant than `forms` needed (`content:read,
  write`), reflecting `seo`'s genuinely read-only nature.

- **`Register` has nothing structural to define (no content type of its
  own, unlike `forms`'s `form_submission`), so it does one thing: fail
  fast if `host.Content()` is `nil`** (i.e. the host was built from a
  manifest that dropped `content:read`). This mirrors `forms`'s "no bypass
  of `pkg/sdk`'s own gate" guarantee — proven here by
  `TestRegisterDeniedWithoutContentReadScope` — even though `seo`'s
  `Register` does no other work.

- **Sitemap generation filters to `content.StatusPublished` items only,
  client-side over `ContentAPI.List`'s full result set** — exactly the
  same pattern `forms.List` established for filtering by `form_name`
  (`sdk.ContentAPI.List` has no server-side filter parameter as of this
  slice; adding one is a `pkg/sdk` change explicitly out of scope per the
  parent session's instruction to avoid touching `pkg/sdk`). A sitemap is
  a public-facing document; including draft items would leak unpublished
  content's existence/URLs, so this filter is not optional. Verified in
  `TestGenerateSitemapProducesWellFormedXMLForPublishedItems`, which seeds
  two published items and one draft and asserts the draft's URL is absent.

- **Field-name convention for content items is a judgment call, since
  content types are declared dynamically per-site and `seo` cannot require
  any specific field at compile time:** `title`, `description`, and `slug`
  string fields, read via a small `fieldString` helper that falls back
  gracefully (to `""` for title/description, to the item's own `ID` for
  slug) when a field is absent, non-string, or empty. This is documented
  here rather than enforced by any schema — a site whose content type
  doesn't define `slug` still gets a working (if less pretty) sitemap URL
  keyed on the item ID, rather than an error.

- **`GenerateMetaTags` and `GenerateStructuredData` are pure functions of
  `*content.Item` + a `baseURL` string — no `HostAPI` involved at all.**
  This matches the ticket's own framing ("this can be a pure function
  taking a `*content.Item`-shaped input... producing a string/struct of
  tags") and makes both trivially unit-testable against hand-built
  fixtures, with no database. Only `GenerateSitemap` takes a `HostAPI`,
  since listing *which* items exist is the one part of this slice that
  necessarily crosses the content boundary.

- **Structured data is a schema.org `Article`, populated only with fields
  derivable from a generic `content.Item`**: `headline`, `description`,
  `datePublished` (`item.CreatedAt`), `dateModified` (`item.UpdatedAt`),
  `url`. `author` is a real, commonly-expected `Article` field schema.org
  recommends but does not strictly require — deferred here (see Risks)
  since a generic content item has no author field convention yet in this
  codebase (`internal/content.Item` carries no author reference as of this
  slice).

- **`capabilities/seo` imports `internal/content` for exactly one reason —
  the `*content.Item` type `sdk.ContentAPI`'s own method signatures already
  expose — identical to the precedent `forms` established** (slice 2.9's
  tracking doc explains this asymmetry in full). No other kernel internals
  are imported; `pkg/sdk` itself was not modified.

- **Post-review fixes (parent session's independent `/code-review` audit on
  PR #4):** two Standards findings, both addressed before merge. (1) The
  `host.Content() == nil` scope-gate check was duplicated verbatim in
  `Register` and `GenerateSitemap` — extracted into a shared
  `requireContent(host sdk.HostAPI) (sdk.ContentAPI, error)` helper, called
  from both. (2) Error wrapping was inconsistent — hand-written guard
  clauses were prefixed (`"seo: ..."`) but `json.Marshal`/
  `xml.MarshalIndent` passthroughs in `GenerateStructuredData`/
  `GenerateSitemap` returned bare `err`. Normalized both to
  `fmt.Errorf("seo: ...: %w", err)`, matching `capabilities/forms`'s own
  convention. Spec axis of the audit came back clean with no findings.

## Open Questions — resolved

- **Should `seo` run through a real WASM boundary to more rigorously prove
  the "Tier B" tag?** No — see the Tier decision above. Satisfied instead
  by using only `sdk.Plugin`/`sdk.HostAPI`, the same two-call contract a
  WASM guest's host would adapt its own transport to, per the precedent
  `forms`'s tracking doc (0019) already set for exactly this question.
- **Which schema.org type should structured data use — `Article`,
  `WebPage`, or something content-type-specific?** `Article` — the most
  common, most widely-supported type for exactly the kind of generic
  titled/described content item this slice's fixtures model, and the type
  the ticket itself names as an example ("a real schema.org `Article` (or
  whichever type you choose)"). A future slice could make the type
  configurable per content type; explicitly deferred, not silently
  dropped.

## Files/Modules Changed

- `capabilities/seo/seo.go` (new) — `Plugin`, `New`, `Manifest`, `Register`,
  `MetaTags` (+ `Render`), `GenerateMetaTags`, `GenerateStructuredData`,
  `GenerateSitemap`, plus unexported helpers (`fieldString`,
  `canonicalURL`, the sitemap XML shape).
- `capabilities/seo/seo_test.go` (new) — 7 tests: manifest well-formed,
  `Register` denied without `content:read`, meta-tag generation (title,
  description, canonical, Open Graph fields, HTML rendering), meta-tag
  slug fallback to item ID, structured-data JSON-LD validated by
  unmarshalling and checking `@context`/`@type`/required `Article` fields,
  sitemap XML validated by unmarshalling with `encoding/xml` and asserting
  exactly the published items' `<loc>`/`<lastmod>` entries are present
  (and the draft item's is absent), sitemap generation denied without
  `content:read`.

## Acceptance Criteria

- [x] `capabilities/seo` implements `sdk.Plugin`, reached only via
      `host.Content()` (`ContentAPI.List`) — no kernel-internal access
      beyond the `*content.Item` type precedent `forms` already
      established.
- [x] A generated sitemap.xml is validated as real well-formed XML by
      parsing it back with `encoding/xml` in the test and asserting it
      contains the expected `<url>`/`<loc>`/`<lastmod>` entries for real
      published content items, and excludes a real draft item.
- [x] A generated JSON-LD block is validated as valid JSON and checked
      structurally for the fields a schema.org `Article` requires
      (`@context`, `@type`, `headline`, `description`, `datePublished`,
      `dateModified`, `url`) — not eyeballed.
- [x] Meta-tag generation produces the `<title>`/`<meta
      name="description">`/Open Graph tag set a real theme/renderer would
      emit, as a pure function over a `*content.Item`-shaped input, tested
      against real content fixtures.
- [x] `Register` is denied when the host's manifest lacks `content:read` —
      `seo` carries no gate-bypass of its own.
- [x] No modification to `pkg/sdk` (including `manifest.go`'s
      `knownAPIScopes`) — `content:read` already existed.
- [x] `go build ./... && go vet ./... && go test -race ./...` green
      repo-wide.
- [x] TDD discipline followed: `seo_test.go` written and confirmed red
      (build failure — package didn't exist) before `seo.go` was written.
- [x] Tier decision (Tier A vs. real WASM guest) made explicitly and
      documented with rationale, per the ticket's own instruction to do so
      either way.

## Risks

- **No HTTP-exposed endpoints** (`GET /sitemap.xml`, meta-tag injection
  into rendered pages, a `/robots.txt` reference to the sitemap) — this
  slice proves the Go-level generation functions are real and correct;
  wiring them into `internal/api`/a theme-rendering path is a future slice,
  exactly like `forms`'s deferred HTTP-submission endpoint (0019's own
  Risks).
- **No `author` field in structured data** — schema.org recommends it for
  `Article`; deferred since `internal/content.Item` has no author-reference
  convention yet. A future slice adding an author-attribution convention to
  content items could extend `GenerateStructuredData` without changing its
  signature.
- **Sitemap's client-side published-only filter does not scale** to very
  large content sets, identical in kind to `forms.List`'s own documented
  scaling risk (0019) — both are waiting on the same potential
  `ContentAPI.List` filter-parameter enhancement, out of scope for either
  slice individually.
- **Only one schema.org type (`Article`) is supported** — a
  content-type-to-schema-type mapping (e.g. `product` → `Product`, `page`
  → `WebPage`) is a real future need, explicitly deferred (see Open
  Questions).
- **This slice does not exercise the WASM/RPC runtimes** — by design, per
  the Tier decision above; a reader wanting proof of those boundaries
  should look at slices 2.4/2.5's own tracking docs (0015/0016).
