# Implementation: Phase 5 — capability registration + AI config/enablement

## Goal

Register the 5 first-party capability plugins (forms, seo, commerce,
membership, notifications) in the daemon via a new in-process (Tier A)
registrar that builds a shared `KernelDeps` + `NewHostAPI`, and make AI
operator-configurable: `ai{provider, model, api_key, base_url, rate_limit}`
via `GLYPHUX_AI_*` env with the key resolved only through the secrets seam,
`api.WithAI` applied iff a provider is configured — replacing the AI
endpoint's 404-until-configured opt-in with a real configured path. Full
scope, acceptance criteria, and definition of done:
`docs/specs/phase5-gap-closure-spec.md` Ticket T5.

## Owning Contexts

- No new CONTEXT.md/ADR — additive wiring across existing owning contexts
  (internal/, pkg/sdk, pkg/runtime, capabilities/); see
  docs/agents/domain.md.

## Status

Completed (merged to dev @ 5531619)

## Current Decisions

- New `internal/plugin` package: `RegisterPlugin(p sdk.Plugin)` /
  `Registered() []sdk.Plugin`; shared
  `KernelDeps{Compositions, Content, Media, Identities, Bus, KV
  (MemoryKVBackend for now — T6 swaps in the DB backend), Blocks, Audit(nil
  until T7)}`.
- `cmd/glyphuxd` registers all 5 capabilities at boot; fatal-fast on
  invariant violation (duplicate/empty names, matching the
  `firstparty.RegisterAll` invariant style). No per-capability enable flags —
  all first-party enabled at boot.
- AI config: unknown provider => fail-fast startup error naming the valid set
  (`claude|openai|gemini|openai-compatible`); `api_key` only via the
  `secrets.go` seam; redaction asserted (plain/serialized config never
  contains the key); `rate_limit` maps to `Service.Limits`.
- `internal/api/ai.go` is unchanged — it consumes `WithAI`.

## Open Questions

None open — notifications boots with the log-mailer default adapter (no SMTP
credentials required), per scope decision.

## Files/Modules Expected

- `internal/plugin/registrar.go` (+test).
- `internal/config/config.go` + `secrets.go` (+ai section).
- `cmd/glyphuxd/main.go`.
- `internal/api/ai.go` unchanged (consumes WithAI).

## Acceptance Criteria

Full list in `docs/specs/phase5-gap-closure-spec.md` Ticket T5. Key proofs:
`api.WithAI(ai.NewService(adapter))` applied iff provider configured
(`POST /api/v0/ai/compose` 200 via fake adapter/httptest fake provider);
unset provider => 404 + clean startup without a key; unknown provider fails
fast naming the valid set; key resolved via secrets seam with redaction
asserted; all 5 manifests registered and their content types
(`form_submission`, `product`, `order`, `membership_tier`,
`membership_subscription`) + only-first-party blocks visible through
`/api/v0/content-types` and `/api/v0/blocks` on a booted daemon;
`capabilities/*` suites stay green (read-only).

## Risks

- Emitter host placement is transport-level in `internal/api` (the
  `aiCallerManifest` precedent) — follow it rather than inventing a new
  path.
- Notifications needs the log-mailer default to boot without credentials.
- `rate_limit` must map onto the existing `Service.Limits` shape, not a new
  config surface.
