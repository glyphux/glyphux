# Implementation: Phase 4 slices 4.1+4.2 — Layout Composition contract + block registration

## Goal

PRD §14 slices 4.1+4.2: "blocks, slots, regions, sections — additive to
Layer 1, never polluting it" + "plugins/themes register blocks via the
public API (§13.2); first-party blocks ship in-tree, third-party blocks
arrive as plugins." Per `docs/specs/phase3-4-spec.md`'s Ticket P4.1, this is
Phase 4's foundation slice — the Layer-2 contract types, a real public
block registry, `pkg/sdk`'s `RegisterBlock` wired to it for real (it was a
no-op stub since slice 2.1), and a handful of first-party blocks shipped
in-tree. Everything else in Phase 4 (server-side layout rendering, the
visual builder, live preview, presets/bundles) builds on this.

## Owning Contexts

- No `CONTEXT.md` exists yet (see `docs/agents/domain.md`). New: `pkg/
  contract/layout.go` (additive to the existing `pkg/contract` package),
  `pkg/blocks` (new public package), `blocks/firstparty` (new top-level
  directory, sibling to `capabilities/`/`themes/` per the PRD's own
  repo-tree sketch). Modified: `pkg/sdk/host.go` (`KernelDeps.Blocks`,
  `BlockDef` extended, `RegisterBlock` wired to the real registry instead
  of an internal, inert slice).

## Status

Complete. Built by the parent session directly via TDD (not a dispatched
ticket), the same way slice 2.1 was — this is Phase 4's foundational slice,
everything else depends on it. `go build ./... && go vet ./... && go test
-race ./...` green across the whole repo (29 packages, two new: `pkg/
blocks`, `blocks/firstparty`).

## Current Decisions

- **`Layout` is a wholly separate top-level document from `Composition`,
  not a field added to it.** PRD §14's "additive to Layer 1, never
  polluting it" reads most literally as "Layer 2 doesn't touch Layer 1's
  own type at all" — a Layout describes how blocks are arranged for one
  route/template; a Composition describes what content types exist. Adding
  a `Layout *Layout` field to `Composition` would conflate two independently
  versioned contract layers (§3.2) into one struct's lifecycle.
- **"Sections" (the PRD's fourth noun, alongside blocks/slots/regions) are
  modeled as an ordinary `Block`, not a distinct Go type.** The PRD never
  differentiates a section's structural behavior from any other
  container-shaped block (both accept nested blocks in named slots); adding
  a second type with identical behavior to `Block` would be Speculative
  Generality. A first-party "section" block (a full-width container) is a
  natural addition to `blocks/firstparty` later, using the exact same
  `Definition{Slots: [...]}` shape `container` already demonstrates.
- **`pkg/contract.Layout.Validate()` is purely structural — it does NOT
  check a `Block.Type` against any real registered block.** `pkg/contract`
  must not depend on a runtime registry (it's the public composition
  contract, importable with zero kernel/runtime context); checking a
  block's existence against what's actually registered is
  `pkg/blocks.ValidateLayout`'s job, one layer up, exactly the same
  separation `pkg/contract.ContentType`'s structural `Validate()` already
  has from the composition-level, registry-aware checks slice 2.1's
  `pkg/sdk` performs.
- **`pkg/blocks` is a new top-level public package** (not folded into
  `pkg/sdk` or `pkg/contract`) because it's a genuinely separate concern
  from both: it's not the extension contract (`pkg/sdk`) and it's not the
  composition document shape (`pkg/contract`) — it's the live, mutable
  registry of what block types actually exist in a running instance,
  analogous to how `pkg/sdk`'s `EventBus`/`MemoryKVBackend` are live runtime
  state distinct from the contract types they operate over.
- **Block type names are a flat, global namespace, exactly like content
  type names** (`pkg/blocks.Registry.Register` rejects a duplicate name
  regardless of which plugin/HostAPI instance registers it) — proven by
  `TestHostAPIRegisterBlockRejectsDuplicateAcrossPlugins`: two different
  `HostAPI` instances sharing one `KernelDeps.Blocks` cannot both register
  `"hero"`. This mirrors the existing behavior of every other shared
  `KernelDeps` resource (`Bus`, `KV`) and matches how a real running daemon
  would need block names to be unambiguous for a layout to reference one.
- **`pkg/sdk.BlockDef` is a distinct type from `pkg/blocks.Definition`**,
  not a type alias — so a caller of `HostAPI.RegisterBlock` doesn't need to
  import `pkg/blocks` at all (mirroring how `pkg/sdk.AdminPageDef`/`JobDef`
  are pkg/sdk's own vocabulary, not aliases of some other package's types).
  `RegisterBlock` translates `BlockDef` into `blocks.Definition` internally.
- **`KernelDeps.Blocks` follows the exact "nil = private default" pattern
  `Bus`/`KV`/`Audit` already established** — a `HostAPI` built without
  `KernelDeps.Blocks` set gets its own private `blocks.New()` registry
  (proven by `TestHostAPIRegisterBlockWithoutSharedRegistryStillWorks`),
  so every pre-existing test in `host_test.go` that predates this slice
  continues to work unmodified.
- **First-party blocks (`blocks/firstparty`) are deliberately non-logic-bearing
  — pure `Definition` values (prop schema + slot names), no behavior.**
  PRD §8.1 reserves logic-bearing blocks for Tier-B WASM plugins ("a block
  with logic runs as a sandboxed, capability-scoped plugin"); giving a
  first-party block a Go method a WASM-hosted third-party block couldn't
  also provide would make first-party blocks structurally privileged,
  contradicting the "first-party ships through the identical public path"
  principle slice 2.9 (`forms`) already established for capabilities.
  Four blocks shipped: `heading`, `paragraph`, `image` (leaf blocks, prop
  schemas only) and `container` (the one block with a slot, proving the
  nested-slot shape end to end).
- **`blocks/firstparty` is NOT wired into `cmd/glyphuxd`'s actual daemon
  startup.** Checked first: none of Phase 2/3's first-party capabilities
  (`capabilities/forms`/`notifications`/`seo`/`commerce`/`membership`/
  `marketplace`) are wired into `cmd/glyphuxd`'s bootstrap either — there is
  no existing plugin-loading sequence in the running daemon yet at all, for
  any capability. Inventing one here, for blocks specifically, would be
  scope creep beyond what any sibling slice did and beyond what this ticket
  asked for. `RegisterAll(registry)` is real, tested, and ready for whatever
  future slice adds the daemon's actual plugin/block bootstrap sequence.

## Open Questions — resolved

- **Should `Layout` support more than one document per route/template
  (e.g. versioning, drafts)?** Out of scope — Layer-1 `Composition`'s own
  draft/version handling lives in `internal/content`/`internal/composition`,
  a kernel concern; `pkg/contract.Layout` itself is just the document shape,
  same as `pkg/contract.Composition` doesn't itself carry versioning logic.
- **Should slot/prop validation cross-check a block's declared `Props`
  schema against what a `Block.Props` map actually contains (type-checking
  prop values)?** Deferred — `pkg/blocks.ValidateLayout` only checks block
  *existence*, not prop-value type-correctness. Doing so is a natural
  follow-up (comparing `Block.Props`' value types against
  `Definition.Props`' declared `contract.Field.Type`s) but wasn't asked for
  by this ticket's scope text ("wiring RegisterBlock... into a real block
  registry with real first-party blocks") and would add a non-trivial new
  validation surface; flagged as a Risk, not silently skipped.

## Files/Modules Changed

- `pkg/contract/layout.go` (new) — `LayoutCompositionV1`, `Layout`,
  `Region`, `Block`, `(l *Layout) Validate()`, `validateBlocks` (recursive
  helper).
- `pkg/contract/layout_test.go` (new) — 7 tests.
- `pkg/blocks/registry.go` (new) — `Definition`, `Registry`, `New`,
  `Register`, `Get`, `List`, `ValidateLayout`, `validateBlockTypes`.
- `pkg/blocks/registry_test.go` (new) — 10 tests, including a concurrent-use
  test (`-race` clean) and nested-slot unknown-block-type detection.
- `blocks/firstparty/firstparty.go` (new) — `RegisterAll`, `heading`,
  `paragraph`, `image`, `container` definitions.
- `blocks/firstparty/firstparty_test.go` (new) — 5 tests.
- `pkg/sdk/host.go` — `KernelDeps.Blocks *blocks.Registry` (new field,
  nil-safe default); `BlockDef` extended with `DisplayName`/`Props`/`Slots`;
  `hostAPI.blocks` changed from an inert `[]BlockDef` slice to a real
  `*blocks.Registry`; `RegisterBlock` now delegates to
  `h.blocks.Register(...)` for real (including real duplicate-name
  rejection) instead of silently appending to a slice nothing ever read.
- `pkg/sdk/host_test.go` — 3 new tests:
  `TestHostAPIRegisterBlockDefinesItInSharedRegistry`,
  `TestHostAPIRegisterBlockRejectsDuplicateAcrossPlugins`,
  `TestHostAPIRegisterBlockWithoutSharedRegistryStillWorks`.

## Acceptance Criteria

- [x] `layout-composition/v1` contract types exist in `pkg/contract`,
      additive alongside `content-composition/v0`, with structural
      validation (version, region/slot names, recursive block-type
      presence) and multi-violation reporting matching Layer-1's own
      `ValidationErrors` convention.
- [x] A real, thread-safe, public block registry (`pkg/blocks`) exists,
      with duplicate-name rejection and existence-checking against a
      `Layout` document.
- [x] `pkg/sdk.HostAPI.RegisterBlock` is wired to a real, shared registry
      (previously an inert stub) — proven with a cross-plugin duplicate-
      rejection test.
- [x] At least one real first-party block ships in-tree with a slot,
      proving the nested-block shape end to end (`container`), plus three
      leaf blocks (`heading`, `paragraph`, `image`).
- [x] `go build ./... && go vet ./... && go test -race ./...` green
      repo-wide.
- [x] TDD discipline followed: seam confirmed with the user (contract
      types + registry + HostAPI wiring, all three in one ticket) before
      any test was written; red confirmed before each of the four
      implementation groups (contract types, registry, HostAPI wiring,
      first-party blocks).

## Risks

- **Prop-value type-checking is not implemented** — `ValidateLayout` only
  checks that a `Block.Type` exists in the registry, not that
  `Block.Props`' values match the registered `Definition.Props`' declared
  field types. A future slice (likely 4.4's builder, which needs to render
  a prop-editing UI from a block's schema anyway) is the natural place to
  add this.
- **No slot-membership constraint** — a `container` block's `"content"`
  slot currently accepts any registered block type; the PRD doesn't specify
  per-slot type restrictions (e.g. "this slot only accepts `card` blocks"),
  so none were added. Flagged in case a future slice needs it.
- **No daemon bootstrap wiring** — see Current Decisions. This applies
  equally to every first-party capability, not a gap unique to this slice.
