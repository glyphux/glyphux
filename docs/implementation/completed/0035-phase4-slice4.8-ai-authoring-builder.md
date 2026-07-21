# Implementation: Phase 4 slice 4.8 — AI authoring in the builder

## Goal

PRD §14.1's four-surface table, row 2: "AI authoring inside the builder —
Builder client feature — Phase 4 (needs builder)." §14.1's own paragraph:
"AI in the builder emits composition, not markup. AI output is a Layer-2
composition fragment (blocks + presets) run through the *same validation
path as a preset import* (§13.3) — so 'AI used a block you don't have' is a
catchable, explainable condition, not silent breakage." PRD line ~1173
(Phase 4 ticket list, 4.8): "prompt-to-composition assistance that emits
Layer-2 composition validated through the same path as preset import (4.6)
— never opaque markup." Depends on Ticket P3.6 (`capabilities/ai`, merged
via PR #19) and the builder (slice 4.4b) + live preview (slice 4.5) +
presets (slice 4.6), all already merged into `dev`.

## Owning Contexts

- No new CONTEXT.md/ADR — this is additive wiring across existing owning
  contexts (`internal/api`, `internal/preset`, `internal/layout`,
  `capabilities/ai`, `admin-ui/src/pages/builder`), not a new domain
  concept.

## Status

Complete. Built TDD-style against a real SQLite-backed `preset.Store`/
`layout.Store` (no mocked DB) and a hand-rolled, in-process fake
`capabilities/ai.Adapter` (no live provider, no httptest transport server —
this suite tests the compose-and-validate flow, not any provider's wire
shape, which `capabilities/ai`'s own adapter tests already cover
independently). `go build ./... && go vet ./... && go test -race ./...`
green repo-wide; `cd admin-ui && npm run build && npm test` green;
`cd sdk-js && npm run build && npm run typecheck` green.

## Current Decisions

- **No prior `internal/api` precedent for "the admin server builds its own
  `sdk.HostAPI` to call a first-party capability" existed.** Investigated
  per the ticket's instruction: every other first-party capability call
  site in `internal/api` (`preset.Store`, `layout.Store`, `content.API`,
  etc.) is called as a plain Go value, never through a constructed
  `sdk.HostAPI` — because those domain APIs take a `*permission.Principal`
  directly, not a manifest-scoped `HostAPI`. `capabilities/ai.Service` is
  different: its entire domain-API boundary (`Service.Generate`) is
  deliberately gated on a *caller-supplied* `sdk.HostAPI`'s declared
  manifest scopes (`service.go`'s `callGated`) — exactly the same "no
  bypass of the caller's own manifest scope" property a third-party plugin
  gets. So `internal/api/ai.go` builds a manifest declaring
  `api: [ai: [generate]]` (`aiCallerManifest`) and calls
  `sdk.NewHostAPI(manifest, sdk.KernelDeps{})` fresh per request — the admin
  server literally becomes "the calling plugin" for this one feature,
  exactly as PRD §14.1 describes any plugin wanting AI access. Built fresh
  per request rather than cached on `Server`, since it's pure/cheap (one map
  construction) and depends only on the configured `Adapter`'s
  `AllowlistHost()`, not on request state.
- **Fragment shape: the full `pkg/contract.CompositionPreset`, not a
  subset.** The system prompt (`aiComposeSystemPrompt`) instructs the model
  to emit the complete shape (`contract_version`, `name`, `description`,
  `layout`, `manifest`) rather than a bare `Layout` the server fills in the
  wrapper for — this keeps validation identical to a real preset
  save/import with zero special-casing (`fragment.Validate()` then
  `compat.CheckPreset`, byte-for-byte the same two calls
  `internal/preset.Store.Save` makes), and keeps the returned `fragment`
  directly postable to `POST /api/v0/presets` with no server-side
  reshaping on accept.
- **Validates through `Store.Save`'s validation *sequence*
  (`p.Validate()` then `compat.CheckPreset`), not through
  `Store.Import`**, because the AI's fragment is an unsaved, in-memory
  draft — the same "draft, not yet persisted" relationship
  `handleLayoutPreview` (slice 4.5) already has to `layout.Store.Save`.
  `Store.Import` additionally requires the preset already exist under a
  saved ID, which would force persisting an unreviewed AI proposal before
  the user ever sees it — backwards from "preview then accept."
- **Preview reuses the exact merge `internal/preset.Store.Import` performs
  (fragment regions layered onto the target route's existing saved
  Layout), without persisting it**, then runs `layout.ValidateDraft` +
  `themes/starter.Render` — the identical machinery
  `handleLayoutPreview` (slice 4.5) already uses. This means the compose
  preview shows exactly what a real import into that route would render
  as, not the fragment rendered in isolation.
- **Two existing failure shapes reused verbatim, never a third AI-specific
  one:**
  - Malformed model output (not JSON, or JSON failing
    `CompositionPreset.Validate`'s structural check) → 422
    `contract.ValidationErrors` via `writeDomainError` — byte-for-byte the
    same shape `handlePresetCreate` already returns when `Store.Save`'s own
    `p.Validate()` fails.
  - Structurally valid but referencing an unregistered block/slot → 200
    with the real `compat.Result` (`Compatible: false`,
    `MissingBlocks`/`MissingSlots` populated) — byte-for-byte the same
    shape `handlePresetCheck`/`handlePresetImport` already return for an
    incompatible preset. `aiComposeResponse` embeds `compat.Result`
    directly (not wrapped) so its fields promote to the JSON top level.
- **Accept reuses the existing preset endpoints unchanged — no second
  insert path.** `AiResource.compose()` never persists anything; accepting
  in the admin-ui calls `PresetsResource.save(fragment)` then
  `PresetsResource.import(id, route)`, the identical two calls
  `SavePresetBar` already makes today (minus the import step, which this
  ticket is the first admin-ui caller of — `PresetsResource.import` already
  existed in `sdk-js` since slice 4.6, just unused by any admin-ui page
  until now).
- **Gated on `layouts:manage` AND `presets:manage`, both at the transport
  boundary** (`s.requireCapability(permission.LayoutsManage,
  s.requireCapability(permission.PresetsManage, s.handleAICompose))`,
  chained rather than a single combined check, so an anonymous caller still
  gets 401 before an authenticated-but-under-privileged caller gets 403 —
  mirroring every other admin-only route's `requireCapability` convention)
  **and** defense-in-depth again inside the handler
  (`permission.AllowsPrincipal`, mirroring `handleLayoutPreview`'s identical
  double-check per PRD §10.5) — plus `capabilities/ai.Service`'s own
  independent `ai:generate` scope/rate-limit check against the caller
  manifest. All three gates are real; none substitutes for another.
- **`cmd/glyphuxd` does not wire `WithAI` in.** No operator-facing AI
  provider credential configuration exists in `internal/config` today (no
  `GLYPHUX_AI_*` env vars, no adapter selection) — adding that is a
  materially separate piece of scope from this ticket ("no new provider
  adapters," and choosing/validating a credential-config scheme for four
  existing adapters is its own decision, not a one-line wiring change like
  `WithLayouts`/`WithPresets`). `WithAI` is opt-in exactly like
  `WithOAuth`/`WithLayouts`/`WithPresets` — omitting it leaves
  `POST /api/v0/ai/compose` 404ing, which is what the real compiled
  `glyphuxd` binary does today (proven by `sdk-js/test/ai.test.ts`'s one
  integration test against the real binary). A follow-up ticket should add
  the credential configuration and wire `api.WithAI` into
  `buildFullHandler` once that exists.
- **Model is a required request field, no server-side default.** Ticket
  scope forbids adding new provider adapters/config, and
  `capabilities/ai.GenerateRequest.Model` is passed through opaquely with
  no capability-layer notion of "the right default for whatever adapter is
  configured" (each real adapter file documents provider-specific model
  name shapes). The admin-ui panel supplies a placeholder default
  (`claude-3-5-sonnet-20241022`) as a starting point, not a backend
  guess — an operator wiring a different provider would need to change
  that one constant (`AIComposePanel.tsx`'s `DEFAULT_MODEL`) until a future
  ticket makes this server-configurable.
- **`extractJSONObject` best-effort strips a leading/trailing ` ```json `
  code fence** before `json.Unmarshal` — the one common formatting
  deviation chat-completion-shaped models produce even when explicitly told
  not to. This is not a general markdown parser; `json.Unmarshal` (and, one
  layer up, `CompositionPreset.Validate`) is what actually validates the
  result — this only removes the one wrapper known to make an
  otherwise-valid JSON document fail to parse verbatim.

## Open Questions — resolved

- **Should the compose endpoint accept `theme_regions` like
  `handlePresetCheck`/`handlePresetImport` do?** Yes, included
  (`aiComposeRequest.ThemeRegions`) for parity with the existing preset
  endpoints' compatibility-check surface — a headless/JSON-only theme with
  no declared region restriction simply omits it, same as those endpoints.
- **Should accepting re-validate compatibility, given compose() already
  checked it?** Yes — `AIComposePanel.handleAccept` reads
  `PresetsResource.import`'s own returned `CompatResult` and declines
  (showing a toast, not silently succeeding) if it differs from compose()'s
  earlier check, since compatibility could in principle change between the
  two calls (e.g. a concurrent edit removed a block). This costs nothing
  extra — `import()` always re-runs `compat.CheckPreset` itself regardless
  of what compose() found.

## Files/Modules Changed

- `internal/api/api.go` — `Server.ai` field, `WithAI` option, route
  registration for `POST /api/v0/ai/compose` (gated
  `requireCSRF` + `requireCapability(LayoutsManage)` +
  `requireCapability(PresetsManage)`).
- `internal/api/ai.go` (new) — `aiComposeRequest`/`aiComposeResponse`/
  `aiPreview`, `aiCallerManifest`, `aiComposeSystemPrompt`,
  `extractJSONObject`, `handleAICompose`.
- `internal/api/ai_test.go` (new) — `fakeAIAdapter` (hermetic, in-process,
  implements `capabilities/ai.Adapter` directly), `testServerWithAI`, and
  seven tests: 404 when unconfigured, 401 anonymous, valid-fragment
  success path (validated + previewed + confirmed non-persisting), unknown
  block returns real `compat.Result` diagnostics, malformed model output
  returns the real 422 validation-error shape, missing prompt/model → 400.
- `sdk-js/src/ai.ts` (new) — `AiResource`, `AIComposeRequest`.
- `sdk-js/src/types.ts` — `AIComposeResult` (extends `CompatResult`).
- `sdk-js/src/client.ts`, `sdk-js/src/index.ts` — wire `AiResource` in as
  `client.ai`; export the new types.
- `sdk-js/test/ai.test.ts` (new) — one integration test against the real
  compiled `glyphuxd` binary proving the documented "404 until an AI
  provider is configured" behavior (see Current Decisions on why
  `cmd/glyphuxd` doesn't wire `WithAI` yet).
- `admin-ui/src/pages/builder/AIComposePanel.tsx` (new) — the prompt
  input + Generate button + preview/accept/discard panel.
- `admin-ui/src/pages/builder/AIComposePanel.test.tsx` (new) — seven
  tests: no eager call, disabled-until-prompt, compatible result shows the
  iframe preview, incompatible result shows real diagnostics (not a generic
  error), request failure shows `ErrorState`, accept calls
  `save()`/`import()` and `onAccepted`, discard clears without saving.
- `admin-ui/src/pages/builder/BuilderPage.tsx` — mounts `AIComposePanel`
  next to `LivePreview`/`SaveBar`/`SavePresetBar`, gated on the same
  `canManage` (`layouts:manage`) check; `onAccepted={load}` reloads the
  route's Layout from the server (going through the existing
  loading-spinner gate remounts `Frame` with the freshly persisted data,
  since `Frame` only reads its `data` prop on mount — no new re-key
  mechanism needed).
- `admin-ui/src/pages/builder/BuilderPage.test.tsx` — extended the
  `@/lib/client` mock with `presets.import` and `ai.compose`.

## Acceptance Criteria

- [x] `POST /api/v0/ai/compose` calls `capabilities/ai.Service.Generate`
      via a scoped `sdk.HostAPI` the admin server builds declaring
      `api: [ai: [generate]]` — never a raw/unscoped call.
- [x] The parsed model response is validated through
      `contract.CompositionPreset.Validate` + `pkg/compat.CheckPreset` —
      the same two calls `internal/preset.Store.Save` makes — not a
      parallel AI-specific validator.
- [x] An incompatible fragment (references an unregistered block) returns
      the real `compat.Result` (200, `compatible: false`,
      `missing_blocks` populated) — the same shape an incompatible preset
      import already produces.
- [x] Malformed model output (not JSON, or fails structural `Validate()`)
      returns the same 422 `contract.ValidationErrors` shape an invalid
      preset save already produces.
- [x] A compatible fragment is rendered through the real `themes/starter`
      theme (`layout.ValidateDraft` + `starter.Render`) and returned as a
      preview, without persisting anything.
- [x] Gated on `layouts:manage` AND `presets:manage` (401 anonymous, 403
      under-privileged) plus CSRF, independent of `capabilities/ai`'s own
      `ai:generate` scope/rate-limit gate.
- [x] Accepting a proposal in the admin-ui calls the existing
      `PresetsResource.save()` then `PresetsResource.import()` — no second
      insert/merge path.
- [x] `go build ./... && go vet ./... && go test -race ./...` green
      repo-wide.
- [x] `cd admin-ui && npm run build && npm test` green.
- [x] `cd sdk-js && npm run build && npm run typecheck` green.
- [x] TDD discipline followed: seams (documented above under Current
      Decisions, since this ran as a background agent without a live
      seam-confirmation exchange) confirmed against the spec doc/PRD before
      each test; hermetic fake `Adapter`, real SQLite-backed
      `preset.Store`/`layout.Store` in every backend test — no mocked DB.

## Risks

- **No live-provider smoke test** — by design (this ticket's own testing
  discipline forbids live LLM calls); `capabilities/ai`'s own adapter tests
  already cover each real provider's wire shape against local httptest fake
  servers. A genuinely new risk this ticket does carry: the engineered
  system prompt's actual real-world reliability (does a real Claude/GPT/
  Gemini response actually come back as clean, schema-matching JSON often
  enough to be useful) is unverified — the validation path correctly
  *rejects* a bad response with real diagnostics either way, but prompt
  quality/iteration against a real model is unproven here.
- **`cmd/glyphuxd` wiring deferred** (see Current Decisions) — this feature
  is inert in a real running daemon until a follow-up ticket adds AI
  provider credential configuration and calls `api.WithAI`. Documented, not
  silently dropped.
- **No default-model resolution** — an operator/admin-ui author must know
  which model string their configured adapter expects; nothing here infers
  it from the adapter type (the endpoint has no visibility into which
  concrete adapter is configured beyond its `AllowlistHost()`).
- **Preview merge (fragment layered onto the current route's saved Layout)
  can differ from what `SaveBar`'s unsaved in-editor draft currently
  shows** — the preview loads the route's *persisted* Layout via
  `s.layouts.Load`, not the admin-ui's current unsaved canvas edits (unlike
  `LivePreview`, which previews the live in-editor draft). Accepting a
  proposal therefore merges onto the last-saved Layout, not onto whatever
  unsaved changes are currently in the canvas — consistent with how
  `PresetsResource.import` has always behaved (it merges into the *saved*
  Layout), but worth calling out as a possible point of user confusion if a
  future iteration wants "preview against my current unsaved draft"
  instead.
