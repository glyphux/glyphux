# Implementation: Phase 2 slice 2.3 — capability dependency graph + resolver

## Goal

PRD §7.2 ("Capability Dependency Graph") and §7.5 ("Resolution Rules")
require a queryable capability dependency graph and a resolver that, given a
set of explicitly-declared capabilities, computes the full transitive
dependency closure, rejects cycles, and records which resolved capabilities
were explicit vs. implicit. This is the foundation slice 2.1's HostAPI/
Manifest work anticipated but deliberately deferred ("actually checking...
is the capability registry's job (slice 2.3)").

## Owning Contexts

- (no CONTEXT.md exists yet — see docs/agents/domain.md; this is core-kernel
  work in the existing `pkg/sdk` package)

## Status

In progress (implemented and verified in this session; not yet PR'd —
branch is being handed to the parent session for review/merge alongside the
parallel 2.2 event-bus and 2.6 permissions-enforcement slices).

## Current Decisions

- **The capability graph lives in a new file, `pkg/sdk/capability.go`, in
  the same package as `Manifest`/`HostAPI` — not a new subpackage.** It is
  small, pure, dependency-free logic that is conceptually part of the same
  extension contract slice 2.1 established; splitting it into
  `pkg/sdk/capability` would add an import boundary with no present benefit
  (nothing outside `pkg/sdk` needs to import the graph without the rest of
  the package).

- **The single most important judgment call: the capability graph is a
  deliberately separate vocabulary from `manifest.go`'s `knownAPIScopes`,
  and this slice does not touch `knownAPIScopes` or any of manifest.go's
  validation logic.** Reasoning:
  - `knownAPIScopes` (content, users, media, events) validates a *single
    plugin manifest's* API axis against the four domain surfaces `HostAPI`
    currently has real implementations for (slice 2.1).
  - The PRD §7.2 graph is a broader, composition/deployment-level
    vocabulary — content, identity, permissions, media, storage, forms,
    seo, mailer, events, notifications, membership, payments, commerce,
    packaging, signing, marketplace, tenancy, ai, stock-media (19 nodes;
    see below) — most of which (identity, permissions, forms, seo, mailer,
    notifications, membership, payments, commerce, marketplace, tenancy,
    ai, stock-media, storage, packaging, signing) have no `HostAPI` surface
    built yet and are not, and should not yet be, valid `APIScope.Capability`
    values a plugin manifest can declare.
  - Forcing `knownAPIScopes` to grow to the full 19-node vocabulary now
    would mean a plugin manifest could declare `api: identity: [read]` today
    with zero enforcement behind it (no `HostAPI.Identity()` method exists).
    That is a worse state than leaving the two vocabularies decoupled.
  - manifest.go's validation logic (`Requires`, `Permission`) is explicitly
    slice 2.6's territory in parallel right now; not touching it at all
    removes any chance of a merge conflict or scope creep into that slice's
    job.
  - Concretely: the registry's seam is `Graph.Resolve(explicit []string)
    (Resolution, error)` — plain capability-name strings, not
    `sdk.APIScope`. A future slice can add a thin translation from a
    manifest's declared `API []APIScope` capability names into this
    package's capability-name vocabulary (e.g. deciding whether
    `APIScope{Capability: "users"}` maps to `"identity"` here) once that
    slice actually needs to run manifests' capabilities through the
    resolver. This slice does not assume or perform that mapping — doing so
    would require a decision (is "users" the same concept as "identity"?)
    that §7.1 and §7.2 phrase inconsistently themselves (§7.1's prose list
    says "users"; §7.2's formal graph says "identity") and that isn't this
    slice's to make unilaterally.

- **The graph has 19 nodes, not the PRD's headline list of 15.** §7.2's
  diagram names 15 capabilities as graph entries (content, identity,
  permissions, media, forms, seo, mailer, notifications, membership,
  payments, commerce, marketplace, tenancy, ai, stock-media) but its own
  edges reference four more names that never appear as their own entry in
  that list: `storage` (media's dependency), `events` (notifications',
  payments', commerce's, and ai's dependency — this is slice 2.2's event
  bus), and `packaging`/`signing` (marketplace's dependencies). A graph
  cannot be resolved if an edge points to a node that doesn't exist, so
  this slice adds all four as ordinary root nodes (no further dependencies)
  — the same treatment §7.2 itself gives `identity` ("kernel-backed... but
  participates in the graph"). This is documented here explicitly so it
  isn't mistaken for scope creep: no behavior is attached to `storage`,
  `packaging`, or `signing` beyond being resolvable graph nodes, and
  `events` here is purely a graph-node name — it has no relationship to
  slice 2.2's actual event-bus implementation in `host.go`'s `On`/`Emit`.

- **`tenancy` is a normal, inert graph node** with its declared dependencies
  (`identity`, `permissions`) per §11.5's forward-compatibility instruction,
  and nothing else. No defaults, no enforcement, no special-casing anywhere
  in `capability.go` treats `tenancy` differently from any other node. This
  is stated plainly per the task brief's own instruction, so a future
  reader doesn't mistake its presence in the graph for a built feature.

- **"Explicit takes precedence over implicit" (§7.5 rule 1) is implemented
  as: a capability is recorded in `Resolution.Explicit` if the caller named
  it directly, and in `Resolution.Implicit` only if it is reachable as a
  dependency AND was not also named explicitly.** Concretely: if the caller
  declares both `permissions` and `identity` explicitly (even though
  `permissions` already depends on `identity`), `identity` is recorded once,
  under `Explicit`, and `Implicit` is empty — never duplicated or
  downgraded. `Resolution.All()` returns the deduplicated union, sorted, and
  `Resolution.IsExplicit(name)` answers the precedence question directly
  for a caller that only has a capability name.

- **Cycle detection uses a straightforward DFS with an explicit recursion
  (`onStack`) set**, not a general topological-sort library — the graph is
  small (19 real nodes, resolution depth well under a dozen) and a
  hand-rolled DFS keeps the cycle's actual path available for the error
  message (`*CycleError` reports the full cycle, e.g. `"a -> b -> c -> a"`),
  which a black-box "has-cycle" boolean would not give a composition author
  trying to fix their manifest.

- **Unknown-capability errors are also raised if a graph's own edges name a
  node that isn't itself a key in the graph** (`fmt.Errorf("%w: %q (required
  by %q)", ErrUnknownCapability, dep, node)`), not just for a caller's bad
  input — this is what caught the "15 vs 19 nodes" gap above during TDD
  (writing the graph with only the 15 headline names first made
  `Resolve("media")` fail with `unknown capability "storage"`, which is
  exactly the signal that surfaced the need for the four extra root nodes).

- **`NewGraph` deep-copies its input map and slices.** `Graph` is meant to be
  usable as an immutable shared value (`CapabilityGraph` is a package-level
  var); a caller mutating the map literal it passed in after construction
  must not be able to corrupt a `Graph` already handed out.

- **No CLI command** (§7.5 rule 5, `glyphux composition show
  --capabilities`). Per the task brief's own instruction: `cmd/` currently
  has only `cmd/glyphux` and `cmd/glyphuxd`, neither with any existing
  subcommand scaffold, so building a whole CLI subsystem for one command
  was explicitly out of scope. `Resolution` is exported and structured
  (`Explicit []string`, `Implicit []string`, `All() []string`,
  `IsExplicit(name) bool`) specifically so a future CLI slice can print the
  resolved tree by calling `sdk.CapabilityGraph.Resolve(...)` directly with
  no further plumbing. **Deferred, explicitly, to a future CLI slice.**

- **No composition-contract field added.** The brief suggested "you're
  likely adding a new field... to something composition-shaped" as one
  option. After reading `pkg/contract/contract.go`, `Composition` already
  has a `Capabilities []string` field (added by an earlier slice, currently
  unused/unvalidated). This slice deliberately does **not** wire
  `Composition.Capabilities` through `CapabilityGraph.Resolve` yet: doing so
  would mean deciding where composition-validation-time resolution actually
  runs (`Composition.Validate()`? A new method? At load time in some
  `internal/composition` caller?) and how a resolution failure surfaces
  through `contract.ValidationErrors`, which is a wiring decision that
  affects `internal/composition` — outside this slice's `pkg/sdk` scope and
  overlapping with the parallel 2.6 slice's territory (which is also
  touching composition-validation-adjacent enforcement). The graph and
  resolver are complete and independently tested; wiring them into
  `Composition.Validate()` is left as a clearly-named follow-up rather than
  guessed at here.

## Open Questions — resolved

- **Should the graph reject a caller declaring an unknown capability name,
  or silently ignore it?** Resolved: reject, with `ErrUnknownCapability` —
  silently ignoring a typo'd capability name in a composition would be a
  much worse failure mode (a plugin silently missing dependencies it
  thought it had) than a loud, well-typed error at validation time, which
  is exactly what §7.5 rule 2's spirit ("rejected at composition-validation
  time") asks for cycles and, by the same logic, malformed input.

- **Does "identity" vs "users" naming inconsistency between §7.1 and §7.2
  need resolving in this slice?** Resolved: no — the capability graph
  follows §7.2's graph verbatim (using "identity", "permissions", etc.,
  since that's the section that actually defines dependency edges); §7.1 is
  prose-level scene-setting, not a formal spec. Reconciling this with
  `manifest.go`'s "users" APIScope name is explicitly deferred to whichever
  future slice builds the manifest-to-capability-graph translation (see
  Current Decisions above).

## Files/Modules Changed

- `pkg/sdk/capability.go` (new) — `Capability`-graph types: `Graph`,
  `NewGraph(map[string][]string) *Graph`, `Resolution` (`Explicit`,
  `Implicit` fields, `All()`, `IsExplicit()` methods),
  `(*Graph).Resolve(explicit []string) (Resolution, error)`,
  `ErrUnknownCapability`, `CycleError` (+ `Error()`), and the package-level
  `CapabilityGraph` var implementing the PRD §7.2 graph (19 nodes, see
  Current Decisions).
- `pkg/sdk/capability_test.go` (new) — 14 tests: per-capability resolution
  (mailer with no deps, commerce's full closure, marketplace's full
  closure, tenancy's inert-but-real deps), explicit-precedence-over-implicit,
  dedup of repeated explicit input, no-duplicates-across-diamond-deps
  (membership reaching identity via two paths), unknown-capability
  rejection, empty-input resolves to nothing, cycle rejection (3-node cycle
  and a 1-node self-cycle, both via a synthetic test-only `Graph` — the real
  `CapabilityGraph` is acyclic by construction and not used for cycle
  tests), a non-cyclic synthetic graph accepted, and full coverage that
  every one of the 19 PRD-graph node names actually resolves.

## Acceptance Criteria

- [x] The 16 (actually 19, once the graph's own implied leaf nodes are
      counted — see Current Decisions) capabilities and edges from PRD §7.2
      exist as a real, queryable structure — `sdk.CapabilityGraph`,
      verified by `TestCapabilityGraphContainsAllPRDNodes`.
- [x] Declaring `commerce` resolves its full transitive closure (content,
      identity, permissions, payments, events, notifications) — verified by
      `TestResolveCommerceResolvesFullTransitiveClosure`.
- [x] Declaring `marketplace` resolves its full transitive closure through
      `commerce` — verified by `TestResolveMarketplaceResolvesFullTransitiveClosure`.
- [x] Declaring a capability with no dependencies (`mailer`) resolves to
      just itself — verified by `TestResolveMailerHasNoImplicitDependencies`.
- [x] Circular dependencies are rejected with a clear, well-typed error
      (`*sdk.CycleError`, not a stack overflow or silent wrong answer) —
      verified by `TestResolveRejectsCircularDependency` and
      `TestResolveRejectsSelfReferentialCycle` against synthetic graphs.
- [x] Explicit capabilities take precedence over implicit ones, recorded
      once — verified by `TestResolveExplicitTakesPrecedenceOverImplicit`.
- [x] Diamond dependencies (a capability reachable via more than one path)
      never appear twice in the resolved set — verified by
      `TestResolveNoDuplicatesAcrossDiamondDependencies`.
- [x] `tenancy` exists in the graph with its declared deps and nothing
      else built for it — verified by
      `TestResolveTenancyIsAnInertGraphNodeWithItsDeclaredDeps`; no other
      file in this slice references `tenancy` at all.
- [x] Unknown capability names are rejected with a well-typed sentinel
      error (`sdk.ErrUnknownCapability`) — verified by
      `TestResolveUnknownExplicitCapabilityReturnsError`.
- [x] No CLI command built (deferred, per task brief) — `Resolution`'s
      exported shape is the seam a future CLI slice needs.
- [x] No installation/consent flow, no audit logging, no changes to
      `manifest.go`'s `Requires`/`Permission` validation logic or
      `knownAPIScopes` — confirmed by `git diff` touching only
      `pkg/sdk/capability.go`, `pkg/sdk/capability_test.go`, and this doc.
- [x] `go build ./...`, `go vet ./...`, and `go test -race ./...` all green
      across the whole repo.

## Risks

- **The graph's 19-vs-15-node discrepancy is this slice's own resolution of
  an underspecified part of the PRD** (§7.2's diagram references
  `storage`/`events`/`packaging`/`signing` as dependency targets without
  ever listing them as capabilities in their own right in §7.1's or §7.2's
  prose). If a future slice decides these should NOT be first-class graph
  nodes (e.g. `storage` should be an internal implementation detail of
  `media` rather than a resolvable capability), this graph and its tests
  will need revision. Flagging this explicitly rather than quietly picking
  the interpretation and moving on.
- **No wiring into `pkg/contract.Composition` yet** (see Current Decisions)
  — `Composition.Capabilities []string` exists but nothing calls
  `CapabilityGraph.Resolve` against it, and `Composition.Validate()` does
  not yet surface unresolved/circular capability errors as
  `contract.ValidationErrors`. This is a real gap for "the resolver runs at
  composition-validation time" (§7.2's own words) — the resolver exists and
  is fully tested in isolation, but is not yet invoked automatically
  anywhere in the composition-validation path. Left as an explicit
  follow-up rather than guessed at, to avoid colliding with the parallel
  2.6 slice's composition-adjacent work.
- **The manifest-to-capability-graph translation is undecided** (does a
  manifest declaring `api: users: [read]` imply capability `identity`?).
  Deferred deliberately (see Current Decisions and Open Questions); no code
  in this slice assumes an answer.
- This slice was built in a worktree that started 58 commits behind `dev`
  (missing slice 2.1's `pkg/sdk` entirely) and was rebased onto `dev` before
  work began; the rebase was clean with no conflicts, but the parent
  session should still double check this branch's base matches `dev`
  before merging.
