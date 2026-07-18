# Implementation: Security primitives (CSRF, CORS opt-in, secrets provider, input sanitization)

## Goal

PRD §1.9 lists "CSRF/CORS handling, rate limiting, secrets provider, input
sanitization, session hardening — kernel-level, per Principle 8, not a
plugin" as a Phase-1 slice. A full audit (2026-07-18) found only
login-failure rate limiting exists today (`internal/api/ratelimit.go`,
scoped to brute-force protection, not general API rate limiting). CSRF,
CORS, a secrets provider abstraction, and input sanitization are entirely
absent — `internal/server/server.go` explicitly comments that there is no
`Access-Control-Allow-Origin` anywhere and CORS is "opt-in only" with
nothing actually able to opt in.

## Owning Contexts

- (no CONTEXT.md yet — core-kernel: `internal/server`, `internal/api`,
  `internal/config`, likely a new `internal/security` or similar package)

## Status

In-flight

## Current Decisions

- CSRF: the admin SPA and wizard are same-origin, cookie-authenticated
  clients — the classic CSRF threat model. Add a double-submit-cookie or
  synchronizer-token CSRF check for cookie-authenticated state-changing
  requests (POST/PUT/PATCH/DELETE), skipped for bearer-token-authenticated
  requests (a stolen bearer token requires the attacker to already read the
  response, unlike a cookie which rides along automatically — bearer
  requests aren't the CSRF threat model). Read how `sessionCookieName` /
  `currentUser` work in `internal/api/auth.go` and mirror the same
  dual-transport pattern the cookie-vs-bearer auth code already uses.
- CORS: today's "no CORS ever" posture is correct for the admin SPA (same
  origin, embedded via go:embed) but wrong for the stated headless-CMS use
  case (PRD's own positioning: external developers building separate
  frontends that call the API cross-origin). Add an explicit, opt-in CORS
  configuration (allowed origins list in `internal/config`, empty/unset by
  default preserving today's "no CORS" behavior) rather than leaving no
  mechanism to opt in at all.
- Secrets provider: an abstraction for where secrets (DB DSNs with
  passwords, OAuth client secrets once 0009 lands, mailer API keys later)
  come from — env var by default, with a documented seam for a real
  secrets manager later (not building Vault integration now, just the
  interface so config loading doesn't hardcode "always read from env" in
  fifty places). Look at `internal/config/config.go`'s current loading
  pattern first; keep this proportional, not speculative infrastructure for
  needs that don't exist yet (Principle in §16: "no new abstraction unless
  it retires real duplicated code or solves a real external use case") — if
  today's env-var-only loading is already clean and there's no real
  duplication to retire, a thin named type/function documenting "this is
  the one place secrets are read from" may be sufficient rather than a full
  provider interface with multiple implementations.
- Input sanitization: content field values are already validated by type
  (string/richtext/number/etc. per `internal/content`'s validation) — the
  gap is likely HTML/script sanitization for rich-text fields specifically
  (stored HTML rendered by themes/clients later) and general defense against
  stored-XSS via content fields. Investigate what `contract.Field`/richtext
  validation currently does (or doesn't do) before designing new sanitization
  — this may already be partially covered and just needs the actual
  HTML-sanitization step added at the write boundary in `internal/content`.
- Session hardening: review current session cookie flags (`HttpOnly`,
  `Secure` — now correctly gated by `TrustProxyHeaders` per the recent audit
  fix, `SameSite=Lax`) and session lifetime/rotation in `internal/identity`
  — add what's missing (e.g. session expiry/idle timeout if absent,
  rotation on privilege change) per what a reasonable "hardening" bar
  requires, without inventing exotic requirements the PRD doesn't ask for.

## Open Questions

- Whether general (non-login) API rate limiting is in scope for this slice
  or deserves its own note — the PRD lists it in the same bullet as
  CSRF/CORS/secrets/sanitization/session-hardening, so default to including
  a basic per-IP or per-token rate limit on the API surface generally,
  reusing `internal/api/ratelimit.go`'s existing limiter primitive if it
  generalizes cleanly; split out if it turns out to need a materially
  different design.

## Files/Modules Expected

- `internal/api/csrf.go` (new) + tests
- `internal/server/server.go`, `internal/config/config.go` — CORS
  configuration + middleware
- `internal/config/config.go` or new `internal/secrets/` — secrets loading
  seam
- `internal/content/*.go` — input sanitization at the write boundary for
  rich-text fields
- `internal/identity/*.go` — session hardening additions if any gaps found
- `internal/api/ratelimit.go` — possibly generalized beyond login
- Admin UI / SDKs: update if CSRF requires a token the SPA must send
  (e.g. `sdk-js`'s `http.ts` learning to attach a CSRF token header for
  cookie-authenticated requests)
- Tracking doc moved to `docs/implementation/completed/` when done

## Acceptance Criteria

- [ ] CSRF protection demonstrably blocks a cross-origin cookie-riding
      state-changing request and demonstrably allows the real admin SPA's
      own requests through (tested at the HTTP seam, not just unit-level).
- [ ] CORS is configurable (opt-in, off by default) and demonstrably applies
      correct headers only when configured.
- [ ] Secrets loading has one clear, documented seam (even if today's only
      implementation is env vars).
- [ ] Rich-text content input is sanitized against stored-script injection,
      proven by a test that submits a malicious payload and asserts it's
      neutralized on read-back.
- [ ] `go build/vet/test ./...` (+ `-race` on touched packages) green.
- [ ] Real end-to-end verification against the compiled binary: confirm
      CSRF/CORS/sanitization behavior with curl against a real running
      instance, not just `go test`.
- [ ] Admin UI's own test suite and build still pass if CSRF requires SPA
      changes.
- [ ] Tracking doc moved to completed with an honest, specific account of
      what's implemented.

## Risks

- Depends on 0006 (domain-boundary capability enforcement) having landed
  first if input sanitization is added inside `internal/content` — check
  `dev`'s current state before starting; if 0006 hasn't merged yet, avoid
  touching `internal/content`'s method signatures and add sanitization as a
  separate, narrowly-scoped internal helper called from the existing
  write path to minimize merge conflict surface.
- Scope discipline risk: CSRF/CORS/secrets/sanitization/session-hardening
  is five different concerns bundled in one PRD bullet — resist gold-plating
  any single one beyond what's actually needed for a real production
  deployment.
