# glyphux

## Agent skills

### Issue tracker

Issues are tracked as GitHub Issues on `glyphux/glyphux` via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Default triage vocabulary: needs-triage, needs-info, ready-for-agent, ready-for-human, wontfix. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.

### Implementation tracking

In-flight implementation work is tracked as notes under `docs/implementation/{active,completed,paused}/`. Check `docs/implementation/active/` before changing architecture or implementing a feature. See `docs/agents/implementation-tracking.md`.

### Session Replay

Every session (a `pi` process that runs from start to quit) is auto-recorded into `sessions/{date}-{time}-{model}.json`. See `scripts/session-replay.ts` and the `ext-session-replay` extension.

### Session Manager

When you open multiple Pi agents that share a session key (e.g. from the same terminal), the Session Manager automatically coalesces them into a single session. This lets you open three agents—say, `pi`, `pi -e ext-pure-focus`, and `pi -e ext-tool-counter`—and they will all share the same session key, appear as related agents in the UI, and their tool calls and tokens will be correctly aggregated for cost, branch traversal, and file-change metrics.

## Tooling
- **Task runner**: `just` (see justfile)
- **Extensions run via**: `pi -e extensions/<name>.ts`

## Project Structure
- `extensions/` — Pi extension source files (.ts)
- `.pi/agents/` — Agent definitions for agent-team extension
- `.pi/agent-sessions/` — Ephemeral session files (gitignored)

## Conventions
- Extensions are standalone .ts files loaded by Pi's jiti runtime
- Available imports: `@mariozechner/pi-coding-agent`, `@mariozechner/pi-tui`, `@mariozechner/pi-ai`, `@sinclair/typebox`, plus any deps in package.json
- Register tools at the top level of the extension function (not inside event handlers)
- Use `isToolCallEventType()` for type-safe tool_call event narrowing