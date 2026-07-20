# Implementation: Content-type management HTTP API

## Goal

Add an HTTP-level way to declare, update, and remove content types after
first-run setup completes. Surfaced as a gap while building slice 1.11 (the
JS SDK's test harness had to seed a content type via a direct Go-level
`composition.Store.Save` call because no such endpoint existed). This blocks
real admin use today and blocks slice 1.13 (admin shell needs to manage
content types through the browser).

## Owning Contexts

- `docs/glyphux-prd.md` §16 Phase 1 (slice 1.1's "content types & fields ...
  persisted and migrated automatically" implicitly requires a write path
  beyond the first-run wizard, which this slice adds)

## Status

Completed

## Current Decisions

- **Routes**: `GET /api/v0/content-types` (public, mirrors `GET
  /api/v0/composition`), `PUT /api/v0/content-types/{name}` (upsert — create
  or replace the named type's field set), `DELETE
  /api/v0/content-types/{name}`.
- **Capability**: new `content_types:manage`, held only by `admin` (schema
  changes are structural/site-wide, unlike per-item `content:write`).
  Confirmed with user.
- **Delete guard**: `DELETE` returns 409 if any content items of that type
  still exist, rather than silently orphaning `content_items` rows whose
  `type` no longer appears in the composition. Confirmed with user.
- **Validation**: reuses `pkg/contract.Composition.Validate()` (already
  covers field types, relation targets, identifier shape) via
  `composition.Store.Save`'s existing validate-before-persist behavior — no
  new validation logic needed.
- **Seam (TDD)**: domain-level tests at `composition.Store` (real sqlite,
  `db.OpenSQLite` + migrations, no mocks) for `DefineContentType` /
  `RemoveContentType`; HTTP-level tests at `internal/api` (real
  `api.Server`, `httptest`, same pattern as existing `content_test.go`) for
  capability gating, the delete guard, and error-status mapping.

## Open Questions

- None blocking.

## Files/Modules Expected

- `internal/composition/store.go` (+ new `store_test.go`): `DefineContentType`, `RemoveContentType`, `ErrContentTypeNotFound`.
- `internal/content/store.go`, `internal/content/content.go`: `countByType` / `API.CountItems` for the delete guard.
- `internal/permission/permission.go`: `ContentTypesManage` capability, admin-only.
- `internal/api/contenttypes.go` (new) + `contenttypes_test.go`: HTTP handlers + routes.
- `pkg/client/content_types.go` (+ test): Go SDK `ContentTypesService` (List/Define/Delete), reusing `pkg/contract.ContentType`/`Field` directly rather than redeclaring the shape.
- `sdk-js/src/content-types.ts`, `sdk-js/src/types.ts` (+ test): JS SDK `ContentTypesResource`, wired into `GlyphuxClient.contentTypes`.

## Acceptance Criteria

- [x] `PUT /api/v0/content-types/{name}` creates a new type or replaces an existing one's fields; invalid shapes (bad field type, dangling relation target, bad identifier) return 422 with issues.
- [x] `DELETE /api/v0/content-types/{name}` removes an empty type; returns 404 for an unknown type; returns 409 (not deleted) if items of that type exist.
- [x] `GET /api/v0/content-types` lists the current set, publicly readable.
- [x] Non-admin callers (editor/viewer/anonymous) get 401/403 on PUT/DELETE.
- [x] Both SDKs (Go `pkg/client`, JS `sdk-js`) extended with a `ContentTypes`/`contentTypes` resource covering list/define/delete, TDD'd against real servers.
- [x] `go build/vet/test` green (including `-race` on the touched packages); `sdk-js` `npm test`/`npm run build` green; verified against the real compiled binary end-to-end by hand (define, list, blocked delete with items present, item cleanup, successful delete, unauthenticated 401).

## Risks

- None encountered. The delete-guard read-then-write (`CountItems` then `RemoveContentType`) is not wrapped in a single DB transaction — a concurrent item creation between the count and the composition write could theoretically race. Accepted for v1: this is a low-frequency admin action, and the existing content-item write path has no equivalent transactional guard against composition changes either. Worth revisiting if/when Phase 2's capability registry needs stronger consistency guarantees.
