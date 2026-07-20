# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root, or
- **`CONTEXT-MAP.md`** at the repo root if it exists — it points at one `CONTEXT.md` per context. Read each one relevant to the topic.
- **`docs/adr/`** — read ADRs that touch the area you're about to work in. In multi-context repos, also check `src/<context>/docs/adr/` for context-scoped decisions.
- **`docs/implementation/active/`** — check for in-flight implementation notes that overlap your task. Read any that overlap after the docs above. See `docs/agents/implementation-tracking.md`.

If any of these files don't exist, **proceed silently**. Don't flag their absence; don't suggest creating them upfront. The producer skill (`/grill-with-docs`) creates them lazily when terms or decisions actually get resolved.

## This repo's layout: single-context (for now)

Glyphux is currently **single-context**: one `CONTEXT.md` + `docs/adr/` at the repo root, once they exist. This matches today's reality — a single Go module (`cmd/`, `internal/`, `pkg/`) delivering `docs/glyphux-prd.md` Phase 0/1 (the core kernel: composition, content, identity, permissions, media). No `admin-ui/`, `capabilities/`, or SDK directories exist yet, so there's nothing to split.

**Documented trigger to revisit:** the PRD's own repository structure (§5.3) and phase plan (§16) anticipate a shift once Phase 2/3 land — `admin-ui/` (React, embedded via `go:embed`) and the first-party `capabilities/` packages (`commerce`, `membership`, `marketplace`, `notifications`, `seo`, `forms`) become genuine bounded contexts at that point. When that code actually lands (not before — see §18.3's anti-premature-structure principle), reconsider switching to multi-context: a root `CONTEXT-MAP.md` pointing at a `CONTEXT.md` per context (core, admin-ui, each capability). Don't pre-declare contexts for code that doesn't exist yet.

## File structure

Single-context repo (most repos):

```
/
├── CONTEXT.md
├── docs/adr/
│   ├── 0001-event-sourced-orders.md
│   └── 0002-postgres-for-write-model.md
└── src/
```

Multi-context repo (presence of `CONTEXT-MAP.md` at the root):

```
/
├── CONTEXT-MAP.md
├── docs/adr/                          ← system-wide decisions
└── src/
    ├── ordering/
    │   ├── CONTEXT.md
    │   └── docs/adr/                  ← context-specific decisions
    └── billing/
        ├── CONTEXT.md
        └── docs/adr/
```

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in `CONTEXT.md`. Don't drift to synonyms the glossary explicitly avoids.

If the concept you need isn't in the glossary yet, that's a signal — either you're inventing language the project doesn't use (reconsider) or there's a real gap (note it for `/grill-with-docs`).

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0007 (event-sourced orders) — but worth reopening because…_
