# Implementation Tracking

> **Workflow (owner update):** `dev` is the integration base. Feature work
> uses SHORT-LIVED per-feature/per-ticket branches off `dev` (e.g.
> `t5-capability-registration`); each branch is PR-reviewed before merging
> into `dev`; do NOT compound multiple features into one long-lived isolated
> branch. Nothing merges into `staging` or `main` without review. Promotion
> flow: `dev` -> `staging` -> `main`.

How to track in-flight implementation work so agents don't duplicate or collide with work already underway, and so architectural context isn't lost between sessions.

## File structure

```
docs/implementation/
  active/
    0001-repo-architecture-scaffold.md
    0002-vertical-slice-0-dashboard.md
  completed/
    ...
  paused/
    ...
```

Numbering is sequential and zero-padded (`0001`, `0002`, ...), followed by a kebab-case name. Numbers are never reused, even after a note moves out of `active/`.

## Before changing architecture or implementing a feature

Check `docs/implementation/active/`. If a note overlaps your task, read it — after the relevant `CONTEXT.md` / ADRs, before writing code. It may already record decisions, open questions, or risks that should shape your approach.

## Creating a new active implementation note

Use this template:

```markdown
# Implementation: <name>

## Goal

## Owning Contexts
- `CONTEXT.md` (or `docs/contexts/<context>/CONTEXT.md` in multi-context repos)

## Status
In-flight

## Current Decisions

## Open Questions

## Files/Modules Expected

## Acceptance Criteria

## Risks
```

## Keeping a note up to date

Update `Current Decisions`, `Open Questions`, and `Risks` as the work evolves — this is the running log that lets a future session (agent or human) pick the work back up without re-deriving it.

## When work finishes or pauses

- **Completed**: move the file to `docs/implementation/completed/`, set `Status` to `Completed`.
- **Paused**: move the file to `docs/implementation/paused/`, set `Status` to `Paused`, and add a line noting why (so it's clear whether it's safe to resume as-is or needs re-scoping).

## Keep CONTEXT.md stable

Don't put in-flight implementation details directly into a domain `CONTEXT.md` unless they're already accepted domain language or architectural decisions. Keep active work in `docs/implementation/active/`; only promote language/decisions into `CONTEXT.md` once they've settled.
