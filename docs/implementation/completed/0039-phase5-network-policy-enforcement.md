# Implementation: Phase 5 — network policy enforcement

## Goal

Turn the `network` permission's allowlist from a decision primitive into an
enforcement point: `sdk.FilterManifest(m, granted)` produces a manifest whose
`AllowsNetworkHost` answers only from the granted subset (deny-by-default,
exact-match); the RPC broker injects the granted allowlist into subprocess
env at launch; an optional `rpc_outbound_proxy_url` maps to
`HTTP_PROXY`/`HTTPS_PROXY`; and Tier B denial is proven structurally (no
network host functions exist). Full scope, acceptance criteria, and
definition of done: `docs/specs/phase5-gap-closure-spec.md` Ticket T3.

## Owning Contexts

- No new CONTEXT.md/ADR — additive wiring across existing owning contexts
  (internal/, pkg/sdk, pkg/runtime, capabilities/); see
  docs/agents/domain.md.

## Status

Completed (merged to dev @ 5531619)

## Current Decisions

- `sdk.FilterManifest` semantics unchanged from `pkg/sdk/manifest.go`:
  granted wins over declared, deny-by-default, exact-match hosts — the
  consent store's granted set is the source of truth.
- `rpc.Broker` sets `GLYPHUX_NETWORK_ALLOWLIST` in subprocess env at launch
  (denial survives the subprocess boundary).
- `rpc_outbound_proxy_url` -> `HTTP_PROXY`/`HTTPS_PROXY`, default off;
  per-plugin proxies and wildcards out of scope.
- Tier B has no network host funcs (`hostfuncs.go` unchanged) — denial by
  absence, proven by a structural test.

## Open Questions

None open — hostile Tier C can ignore env+query absent an operator egress
proxy; recorded as a documented advisory bound.

## Files/Modules Expected

- `pkg/sdk/filter.go` (+filter_test.go).
- `pkg/runtime/rpc/broker.go` (+env wiring, broker_test.go).
- `pkg/runtime/wasm/hostfuncs_test.go` (structural-denial proof).
- `internal/config/config.go` (+`rpc_outbound_proxy_url`).

## Acceptance Criteria

Full list in `docs/specs/phase5-gap-closure-spec.md` Ticket T3. Key proofs:
granted>declared exact-match filtering (declared `[a,b]`, granted `[a]` =>
`b.example` denied); consent dropping network denies every host; a real
subprocess fixture plugin sees exactly the granted hosts in
`GLYPHUX_NETWORK_ALLOWLIST`; proxy env set/unset follows config; no wasm
guest can dial out (no host function exists). The in-daemon filtered-host
proof is a T6 acceptance criterion, not here.

## Risks

- Hostile Tier C can ignore env+query without an operator egress proxy —
  documented advisory bound, not solved this round.
- Filtering must not regress `manifest.go`'s existing exact-match semantics.
- Only package-level here; the loaded-host proof belongs to T6.
