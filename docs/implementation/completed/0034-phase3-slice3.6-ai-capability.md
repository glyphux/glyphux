# Implementation: Phase 3 slice 3.6 — `ai` capability

## Goal

PRD §14 in full ("AI Integration") — "AI is never a kernel concern and
never a privileged surface... consumed only through existing architectural
slots: a capability, a builder client, and a Layer-3 action type." §14.1's
Surface 1 ("AI for the products users build"): the `ai` capability in the
§7 sense — depends on `content` and `events`, exposes scoped domain APIs
(`generate`, `embed`, `classify`), provider-agnostic behind an adapter,
never a raw provider API key or raw network reach to the provider
(allowlisted through the capability). §14.2 ("Provider-Agnostic by
Adapter"): Claude/OpenAI/Ollama/local models are swappable adapters behind
one stable internal contract, "the same discipline as database, storage,
and payment adapters" — no provider's specifics leak above the adapter
boundary. `docs/specs/phase3-4-spec.md`'s Ticket P3.6: a real Claude
adapter, a real OpenAI (ChatGPT) adapter, a real Google Gemini adapter, and
a generic OpenAI-API-compatible adapter (Ollama/self-hosted), each proven
against a local httptest-based fake-provider server — no live API keys
used or required, the same discipline `capabilities/commerce`'s
`fakegateway_test.go` established for Stripe.

## What was built

**`capabilities/ai`** (new package, `sdk.Plugin` implementation):

- `adapter.go` — the provider-agnostic contract: `Adapter` interface
  (`Generate`/`Embed`/`Classify`/`AllowlistHost`), the request/response
  types (`GenerateRequest`/`Response`, `EmbedRequest`/`Response`,
  `ClassifyRequest`/`Response`), and `ErrNotSupported` (returned by an
  adapter method a given provider has no real API for).
- `service.go` — `Service`, this capability's own domain-API boundary.
  `Service.Generate`/`Embed`/`Classify` each: (1) check the CALLING
  plugin's own `sdk.HostAPI.HasAPIScope("ai", scope)`; (2) check
  `host.AllowsNetworkHost(Adapter.AllowlistHost())`; (3) enforce this
  `Service`'s own `Limits` at this boundary (never delegated to the
  adapter); (4) call through `Adapter`; (5) best-effort emit an
  observability event (`ai.generated`/`ai.embedded`/`ai.classified`) via
  `host.Emit`, only if the caller's host also declared `events:emit`.
  `Service.SummarizeContentItem` is the one concrete, tested use of this
  capability's `content` dependency: fetches a content item through the
  caller's own `host.Content()` (gated on that caller's own `content:read`)
  and feeds it into `Generate`.
- `ratelimit.go` — `RateLimit`/`Limits`/`DefaultLimits`, and
  `checkRateLimit`: a fixed-window per-operation call budget persisted in
  the CALLING plugin's own `host.Store()` (already namespaced per plugin by
  `pkg/sdk.hostAPI.Store`) — so one plugin's AI usage can never exhaust
  another plugin's budget, and no new kernel/DB dependency is needed to
  enforce it.
- `ai.go` — `Plugin` (`sdk.Plugin`): wraps one configured `Adapter` via
  `Service`. `Manifest()` declares `content:[read]` and `events:[emit]`
  (this capability's own PRD-declared dependencies — used by
  `SummarizeContentItem` and the best-effort event emission, respectively)
  plus `network` for the adapter's allowlisted host. `Register()` is a
  documented no-op: this capability's entire surface is `Service`'s methods,
  called directly by a caller against ITS OWN host — there is no content
  type, admin page, or event subscription for `Register` to declare.
- `claude.go` — `ClaudeAdapter`: real Anthropic Messages API shape (`POST
  /v1/messages`, `x-api-key` + `anthropic-version` headers, `{model,
  max_tokens, messages}` body). `Embed` returns `ErrNotSupported` (Anthropic
  has no embeddings endpoint). `Classify` is built on `Generate` (see below).
- `openai.go` — one unexported `openAIAdapter` (see design decision below),
  two exported constructors: `NewOpenAIAdapter` (pins `api.openai.com`) and
  `NewOpenAICompatibleAdapter` (any base URL, optional API key). Real
  OpenAI chat-completions/embeddings shape (`POST /v1/chat/completions`,
  `POST /v1/embeddings`, optional `Authorization: Bearer <key>`).
- `gemini.go` — `GeminiAdapter`: real Google Gemini
  `generateContent`/`embedContent` shape (`POST /v1beta/models/{model}:
  generateContent?key=...`, API key as a query parameter, model name in the
  URL path — both real, documented differences from Claude/OpenAI's wire
  shape, kept entirely inside this file per the Adapter boundary's "no
  provider specifics leak above it" discipline).
- Shared helper (`claude.go`, reused by `openai.go`/`gemini.go`):
  `classificationGenerateRequest` builds one provider-agnostic
  constrained-output prompt ("respond with exactly one label"); `matchLabel`
  parses the label back out of generated text (exact match first, then
  longest-label-first substring match, so `"spam"` never shadows
  `"not-spam"`).
- Tests (33 total, all real — no mocked HTTP client anywhere):
  - `service_test.go` (package `ai`, internal — needs `timeNow` for
    deterministic window-rollover testing): scope-denial for each of
    generate/embed/classify, network-host denial, rate-limit enforcement
    (including window reset and per-calling-plugin isolation), conditional
    event emission, and `SummarizeContentItem` against a real SQLite-backed
    content store (mirrors `capabilities/commerce`'s own `testKernel`
    convention).
  - `claude_test.go`/`openai_test.go`/`gemini_test.go` (package
    `ai_test`): a local `httptest.NewServer` per provider replicating that
    provider's real request/response JSON shape, proving each adapter's
    real request-building, response-parsing, and error-handling code —
    happy path, provider error envelope surfaced, wrong-API-key rejection,
    `Classify`-via-`Generate`, and (OpenAI-compatible only) a no-API-key
    path proving the Ollama/self-hosted case works unauthenticated.
  - `ai_test.go` (package `ai_test`): `Plugin.Manifest()` validity/network
    allowlist, `Register`'s no-op success, and one explicit end-to-end test
    (`TestEndToEndCallerPluginUsesAIPluginsService`) building a SEPARATE
    caller manifest declaring `api: [ai: [generate]]`, calling
    `Service.Generate` against it, and getting a real answer back from a
    local fake OpenAI-shaped server — proving PRD §14.1's whole promise end
    to end, not just each gate in isolation.

**`pkg/sdk` (small, additive changes only):**

- `manifest.go`: added `"ai": {"generate": true, "embed": true, "classify":
  true}` to `knownAPIScopes` — previously `"ai"` was only a node in the
  PRD-graph-level `CapabilityGraph` (`pkg/sdk/capability.go`, pre-existing),
  not a capability a manifest could actually declare on its API axis. This
  is what makes `api: [ai: [generate]]` (the ticket's own literal example)
  a manifest a caller can actually write and have `Manifest.Validate()`
  accept.
- `host.go`: added `HostAPI.HasAPIScope(capability, scope string) bool` (and
  its `hostAPI` implementation, delegating to the pre-existing unexported
  `hasScope`). See the design decision below for why this was necessary.

## Design decision: a new `HostAPI.HasAPIScope` method was required

This is the one design choice that isn't just "follow `capabilities/
commerce`'s pattern" and deserves its own explanation.

Every existing HostAPI-gated capability (`content`, `users`, `media`,
`events`) has a DEDICATED `HostAPI` method (`Content()`, `Users()`,
`Media()`, `On`/`Emit`) whose own implementation checks the caller's
declared scope internally (`hasScope`, unexported) before doing anything.
`payments`/`membership` are the closest precedent for a capability with NO
dedicated HostAPI method: `pkg/sdk/manifest.go`'s own doc comment on
`knownAPIScopes` says they "exist here only so a manifest has a capability
to declare in order to subscribe to the corresponding sensitive domain
event" — i.e. their only enforcement point today is indirect, through
`sensitiveEvents`' gating of `On`.

`ai` needed something neither of those precedents provides: a capability
with NO dedicated HostAPI method, whose own domain-API functions (outside
`pkg/sdk` entirely, in `capabilities/ai`) need to check "did the CALLING
plugin declare `ai:generate`" against that caller's own `HostAPI`. Nothing
before this slice exposed that check publicly — `hasScope` is a private
method on the unexported `hostAPI` struct, reachable only from within
`pkg/sdk` itself. Two options were considered:

1. **Add `HasAPIScope(capability, scope string) bool` to `HostAPI`** — a
   generic decision primitive in the same spirit as the already-public
   `AllowsNetworkHost`. Minimal, symmetric with existing precedent, and
   reusable by any future capability in `ai`'s position (a capability whose
   domain API is exposed as methods/functions over an explicit `HostAPI`
   parameter, not a new dedicated `HostAPI` method).
2. **Give `ai` its own dedicated `HostAPI.AI()` method**, mirroring
   `Content()`/`Users()`/`Media()` exactly.

Option 2 was rejected: it would grow `pkg/sdk.HostAPI`'s own interface with
a provider-specific-shaped method (returning some `AIAPI` interface tightly
coupled to this one capability's `Generate`/`Embed`/`Classify` signatures),
exactly the kind of "this capability's specifics leak into the shared
kernel-facing interface" coupling the PRD's adapter-boundary discipline
(§14.2) argues against one layer up — `pkg/sdk` itself becoming
AI-capability-aware felt wrong for a capability the PRD explicitly frames
as an "enhancement, not a pillar" (§14). Option 1 keeps `pkg/sdk` capability-
agnostic (`HasAPIScope` takes plain strings, exactly like
`AllowsNetworkHost` takes a plain string) while still giving `ai` (and any
future capability in the same position) a real, generic enforcement
primitive. Went with option 1.

## Design decision: OpenAI vs. "OpenAI-compatible" — one adapter, two constructors

The ticket asked explicitly to "investigate whether it should be the OpenAI
adapter parameterized by base URL, or a thin wrapper around it." Went with:
**one unexported `openAIAdapter` type, parameterized by base URL and API
key, exposed through two constructors** — `NewOpenAIAdapter(apiKey)` (pins
`https://api.openai.com`) and `NewOpenAICompatibleAdapter(baseURL, apiKey)`
(any base URL, API key optional).

Reasoning: real self-hosted/local runtimes (Ollama and most others)
deliberately implement the exact same OpenAI chat-completions/embeddings
wire shape specifically so existing OpenAI clients work against them
unmodified. The only real differences are (a) the base URL — a local
address instead of `api.openai.com` — and (b) authentication — local
runtimes commonly require no API key at all, or accept any placeholder
value, versus OpenAI's own API which always requires a real key. Neither
difference touches a single line of request-building, response-parsing, or
error-handling. A separate `OpenAICompatibleAdapter` type (even a thin
wrapper delegating every method) would have added an indirection with zero
independent behavior to justify it, and would have invited the two
implementations to drift apart over time for no real reason. Modeling it
as one parameterized type with two constructors captures the actual
relationship precisely: OpenAI's own API IS the OpenAI-compatible shape, at
one canonical, always-authenticated host — not a separate thing. Proven in
`openai_test.go`: `TestOpenAIAdapterGenerateAgainstFakeServer` points the
shared implementation at a local fake server via
`NewOpenAICompatibleAdapter` (proving the wire-shape code is identical to
what `NewOpenAIAdapter` would use against the real host), and
`TestOpenAICompatibleAdapterWorksWithNoAPIKey` specifically proves the
no-authentication Ollama-shaped case.

## Design decision: `Classify` has no native provider endpoint — built on `Generate` everywhere

None of the four providers this slice covers (Anthropic, OpenAI, Gemini, or
any OpenAI-compatible self-hosted runtime) expose a dedicated
classification API — all four are chat/completions-shaped LLM APIs. Rather
than fabricate a fake "classify" wire call that no real provider has (which
would be dishonest against this ticket's own "prove real request/response
handling" TDD instruction), every adapter's `Classify` builds a
constrained-output prompt ("respond with exactly one label from this list")
via the shared `classificationGenerateRequest` helper, calls its own
`Generate`, and parses the label back out via `matchLabel`. This is
documented in `Adapter`'s own doc comment as real, honest behavior for how
classification is actually done against a chat-shaped LLM API in practice —
not a stub standing in for a future real endpoint. Each adapter's
`Classify` test (`claude_test.go`/`openai_test.go`/`gemini_test.go`) proves
this against that provider's own real wire shape (the fake server still
receives and must correctly parse a real `generateContent`/
`chat/completions`/`messages` request — only the prompt content differs
from a plain `Generate` call).

## Design decision: rate limiting is per-CALLING-plugin, in that plugin's own `ScopedKV`

Per the ticket's own instruction ("design this against the domain-API
boundary the way `commerce`/`membership` scope their own operations,
rather than each adapter reimplementing it"), `checkRateLimit`
(`ratelimit.go`) is called once, inside `Service`'s three methods, never
inside any adapter. It persists a fixed-window counter
(`{window_start, count}` JSON) in `host.Store()` — the CALLING plugin's
own `ScopedKV`, already namespaced per plugin by `pkg/sdk.hostAPI.Store`
(keyed by that plugin's manifest `Name`). This gives two properties for
free, both proven by test: (1) one plugin exhausting its own budget cannot
affect any other plugin's budget
(`TestRateLimitIsPerCallingPluginNotGlobal`); (2) no new kernel dependency
or DB table was needed — the existing `MemoryKVBackend` (process-lifetime,
already used by every other capability's `Store()`) is sufficient for this
ticket's scope. A fixed window (not a token bucket / sliding window) is a
documented simplification — precise smoothing is not this ticket's
concern, only proving a real per-caller budget is enforced at the
domain-API boundary.

## Current decisions

- **`Service.Generate`/`Embed`/`Classify` are methods on a `Service` value
  the calling plugin holds a reference to, not new `HostAPI` methods.**
  Mirrors `capabilities/commerce.StartCheckout(ctx, host, gateway, req)`
  and `capabilities/membership.ProcessRenewal(ctx, host, gateway, id)`'s
  established shape exactly: an explicit `host sdk.HostAPI` parameter, not
  a provider-specific addition to `pkg/sdk.HostAPI` itself (see the
  `HasAPIScope` design decision above for the one generic addition that WAS
  necessary).
- **Event emission is best-effort and conditional on the CALLER's own
  `events:emit` scope, not a hard requirement.** A plugin that only wants
  `ai:generate` is not forced to also declare `events` just to get an
  answer back — proven by `TestGenerateEmitsObservabilityEventOnlyWhenEventsScopeDeclared`.
- **`ai` capability's own `Manifest()` does NOT declare its own `"ai"` api
  scope.** The `ai` `Plugin` is the capability that HOSTS the adapter, not
  a caller of its own domain API — declaring `ai:[...]` on itself would be
  meaningless (nothing in `Register` calls `Service`'s methods against its
  own host).
- **No new DB migration.** Rate-limit state lives in the calling plugin's
  existing `ScopedKV` (`host.Store()`), not a new table — re-verified live
  by grepping `Version:` across `internal/*/*.go` before writing any code:
  no migration was added, so the highest existing slot is unchanged by this
  ticket.
- **Not wired into `cmd/glyphuxd/main.go`.** Checked first: none of
  `commerce`/`membership`/`notifications`/`seo`/`forms` are wired into the
  real daemon today (`grep` across `cmd/glyphuxd/main.go` for each package
  name returns nothing) — this ticket matches that precedent rather than
  inventing new scope; `ai` is likewise not wired in.
- **`GenerateRequest.Model` is passed through opaquely.** This capability's
  domain layer never validates or interprets a model name — keeping the
  provider-agnostic promise (§14.2) intact one level up: nothing above the
  `Adapter` boundary needs to know what a valid model string looks like for
  any particular provider.

## Files/modules changed

- `capabilities/ai/adapter.go` (new)
- `capabilities/ai/service.go` (new)
- `capabilities/ai/ratelimit.go` (new)
- `capabilities/ai/ai.go` (new)
- `capabilities/ai/claude.go` (new)
- `capabilities/ai/openai.go` (new)
- `capabilities/ai/gemini.go` (new)
- `capabilities/ai/service_test.go` (new)
- `capabilities/ai/claude_test.go` (new)
- `capabilities/ai/openai_test.go` (new)
- `capabilities/ai/gemini_test.go` (new)
- `capabilities/ai/ai_test.go` (new)
- `pkg/sdk/manifest.go` (edited — `"ai"` added to `knownAPIScopes`)
- `pkg/sdk/host.go` (edited — `HostAPI.HasAPIScope` added, plus its
  `hostAPI` implementation)
- No changes to `admin-ui` or `sdk-js` — backend-only ticket, confirmed via
  `git status`/`git diff --stat` before opening the PR (only the files
  listed above changed).
- Not wired into `cmd/glyphuxd/main.go` — matches existing precedent (no
  first-party capability is wired in there yet).

## Acceptance criteria

- [x] `ai` capability (`capabilities/ai`) implementing `sdk.Plugin`,
      depending on `content`/`events` per §14.1.
- [x] Scoped domain APIs: `generate`, `embed`, `classify`
      (`Service.Generate`/`Embed`/`Classify`) — a caller declares
      `api: [ai: [generate]]` etc. and gets a scoped, rate-limited surface,
      never a raw API key or raw network to the provider (every call gated
      by `host.AllowsNetworkHost` first).
- [x] Provider-adapter contract (`Adapter`) all four adapters implement —
      no provider-specific shape above the boundary.
- [x] Four real adapters (Claude, OpenAI, Gemini, generic OpenAI-compatible)
      each proven against a local httptest-based fake-provider server — no
      live API keys anywhere.
- [x] Rate-limiting/scoping is the capability's own job, at the domain-API
      boundary (`Service`), not duplicated per-adapter.
- [x] TDD: real fake HTTP servers per provider (not mocked HTTP clients),
      real SQLite-backed content store for the content-dependency test, one
      behavior per test.
- [x] `go build ./... && go vet ./... && go test -race ./...` green
      repo-wide (verified before opening the PR).
- [x] No `admin-ui`/`sdk-js` changes — confirmed via `git status`.
- [x] Migration version re-verified live — no new migration added.
- [x] Not wired into `cmd/glyphuxd/main.go` — matches precedent (checked
      first that no other first-party capability is wired in there either).

## Risks / follow-ups

- **No real provider credentials were ever used or tested against** — by
  design (per the ticket and this repo's established discipline), but it
  means a real deployment's first live call against Claude/OpenAI/Gemini is
  still the first true end-to-end proof that each provider's ACTUAL API
  matches this slice's fake-server replication exactly. Provider API
  surfaces also evolve; a future slice should periodically re-diff each
  adapter's wire shape against that provider's current API docs.
- **Fixed-window rate limiting, not a token bucket/sliding window** — a
  documented simplification (see above). A real deployment under bursty
  load at a window boundary could see up to `2x MaxCalls` in a short span;
  acceptable for this ticket's scope, worth revisiting if real usage
  patterns demand smoother limiting.
- **No admin-UI surface for configuring which adapter/API key/model a
  deployment uses** — this ticket is the backend capability + adapters
  only, per the ticket's own scope (no admin-ui/sdk-js work was asked for).
  A future slice would need an admin page (or config file) to let an
  operator choose a provider and supply real credentials.
- **Ticket P4.8 (AI authoring in the builder, Surface 2) depends on this
  ticket** and is explicitly dispatched as a separate follow-up once this
  one merges (per `docs/specs/phase3-4-spec.md`'s own sequencing note) —
  not started here.
- **`SummarizeContentItem` is the only concrete use of the `content`
  dependency this slice ships.** It is a genuinely useful, tested
  convenience, not a hard requirement of `Generate`/`Embed`/`Classify`
  themselves (those three need no content access at all) — a future slice
  building richer content-aware AI features (e.g. auto-tagging, semantic
  search over a content type via `Embed`) would extend this file, not
  `service.go`'s core gating logic.
