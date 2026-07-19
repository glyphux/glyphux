Acthur Implementation-in-flight Convention:

  docs/
    contexts/
      ...
    implementation/
      active/
      completed/
      paused/

  Recommended shape:

  docs/implementation/
    active/
      0001-repo-architecture-scaffold.md
      0002-vertical-slice-0-dashboard.md

    completed/
      ...

    paused/
      ...

  Each active implementation note should include:

  # Implementation: <name>

  ## Goal

  ## Owning Contexts
  - `docs/contexts/platform/CONTEXT.md`
  - `docs/contexts/backtesting-simulation/CONTEXT.md`

  ## Status
  In-flight

  ## Current Decisions

  ## Open Questions

  ## Files/Modules Expected

  ## Acceptance Criteria

  ## Risks

  Then CONTEXT-MAP.md should say:

  ## Implementations In-Flight

  Before changing architecture or implementing a feature, check:

  - `docs/implementation/active/`

  If an active implementation overlaps your task, read that note after the relevant context docs.

  Do not put in-flight implementation details directly into domain CONTEXT.md unless they are already accepted domain language or
  architectural decisions. Keep CONTEXT.md stable; keep active work in docs/implementation/active/.