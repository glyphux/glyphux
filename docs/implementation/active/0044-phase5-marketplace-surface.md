# Implementation: Phase 5 — marketplace HTTP/UI surface

## Goal

Give the existing marketplace primitives a real admin surface: catalog
listing, install via `preset.InstallFromPackage`/`bundle.InstallFromPackage`,
and offline entitlement status — with PRD §12.5 by-design semantics preserved
and proven (entitlements gate updates only; first-install entitlement skip
asserted, not enforced). Full scope, acceptance criteria, and definition of
done: `docs/specs/phase5-gap-closure-spec.md` Ticket T8.

## Owning Contexts

- No new CONTEXT.md/ADR — additive wiring across existing owning contexts
  (internal/, pkg/sdk, pkg/runtime, capabilities/); see
  docs/agents/domain.md.

## Status

In-flight

## Current Decisions

- Catalog source = embedded sample JSON + optional operator file-path
  override; NO remote catalog server. Catalog entries carry version +
  `requires.core` compatibility via `CoreConstraintSatisfied(kernel.Version)`.
- `POST /api/v0/marketplace/packages/{id}/install` fetches package bytes from
  the catalog and calls the existing `InstallFromPackage` path
  (presets:manage-gated, admin-only + CSRF); signature + `requires.core`
  verified; registry-compat + entitlement skipped by design (unchanged
  semantics).
- `GET /api/v0/marketplace/entitlements` verifies an offline token via
  `CanFetchUpdate` + `DescribeExpiry` -> active/expired/not_yet_valid/
  invalid.
- Config `marketplace.public_key` (hex ed25519, baked-in default + operator
  override).
- Capability-kind packages surface the T4 consent hook before enablement;
  preset/bundle kinds stay consent-free via the existing Import path.

## Open Questions

None open — remote catalog/registry server, update-fetch service, publish
pipeline, and storefront are explicit non-goals.

## Files/Modules Expected

- `internal/api/marketplace.go` (+marketplace_test.go);
  `internal/api/api.go` (WithMarketplace, routes).
- `cmd/glyphuxd/main.go` (stores + key config);
  `internal/config/config.go` (+marketplace section).
- `sdk-js/src/marketplace.ts` (+client/index).
- `admin-ui/src/pages/marketplace/` (+tests, routing).

## Acceptance Criteria

Full list in `docs/specs/phase5-gap-closure-spec.md` Ticket T8. Key proofs:
catalog lists packages with version + requires.core compatibility
(compatible/too-new per entry); valid preset-kind bytes install and persist
an id (signature + requires.core verified); tampered package => 422 and
nothing persisted; too-new `requires.core` rejected; entitlements gate
updates only while first install stays allowed (by-design skip asserted);
capability-kind packages require a consent decision before enablement (UI
hook); 401/403/CSRF; existing `internal/preset` + `internal/bundle` store
tests stay green.

## Risks

- Public-key trust anchor: baked-in default vs operator override — make the
  precedence explicit and tested.
- Catalog source indirection (embedded JSON vs file override) must not fork
  the install path.
- Capability-kind packages couple to T4/T6 enablement — preset/bundle kinds
  must remain consent-free via the existing Import path.
