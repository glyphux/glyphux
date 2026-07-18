# Implementation: Security primitives (CSRF, CORS opt-in, secrets provider, input sanitization)

## Goal

PRD §1.9 lists "CSRF/CORS handling, rate limiting, secrets provider, input
sanitization, session hardening — kernel-level, per Principle 8, not a
plugin" as a Phase-1 slice. A full audit (2026-07-18) found only
login-failure rate limiting exists today (`internal/api/ratelimit.go`,
scoped to brute-force protection, not general API rate limiting). CSRF,
CORS, a secrets provider abstraction, and input sanitization were entirely
absent — `internal/server/server.go` explicitly commented that there is no
`Access-Control-Allow-Origin` anywhere and CORS is "opt-in only" with
nothing actually able to opt in.

## Owning Contexts

- (no CONTEXT.md yet — core-kernel: `internal/server`, `internal/api`,
  `internal/config`, `internal/content`)

## Status

**Completed** (2026-07-18)

## What Was Implemented

- **CSRF** (`internal/api/csrf.go`): a double-submit-cookie check. Login
  issues a second, non-`HttpOnly` cookie (`glyphux_csrf`) alongside the
  session cookie; every state-changing route (POST/PUT/PATCH/DELETE) is
  wrapped in `requireCSRF`, which requires a cookie-authenticated request to
  mirror the `glyphux_csrf` cookie's value in an `X-CSRF-Token` header.
  Bearer-token-authenticated requests are exempt entirely (checked by
  presence of an `Authorization: Bearer` header, before any cookie check
  runs) — a stolen bearer token already requires the attacker to read the
  response, unlike a cookie a forged cross-site request carries
  automatically. The admin SPA needed **no changes**: it already
  authenticates every request via a bearer token stored in `localStorage`
  (`admin-ui/src/lib/client.ts`), so it was already outside the CSRF threat
  model this closes; `sdk-js` likewise. Both suites were run to confirm
  (see Verification).
  - Test-helper impact: `internal/api/*_test.go`'s cookie-driven mutation
    tests all route through a new `authCreds` struct (session + CSRF
    cookie) via `sessionCookie`/`authedServer`/`doWithCookie(Body)`, so
    every existing cookie-driven mutation test now also exercises the CSRF
    check exactly like a real browser would, with no per-call-site changes
    needed beyond the ~5 helper-function bodies and 3 direct
    `req.AddCookie` call sites.
- **CORS** (`internal/server/server.go`, `internal/config/config.go`): an
  explicit allow-list, `Config.AllowedOrigins` (`GLYPHUX_ALLOWED_ORIGINS`,
  comma-separated, or `allowed_origins` in the JSON config). Empty/unset by
  default — the daemon sets no `Access-Control-Allow-Origin` at all,
  preserving today's posture exactly. When configured, only a listed origin
  gets `Access-Control-Allow-Origin`/`-Credentials` on real requests and a
  full preflight response (`Allow-Methods`/`Allow-Headers`) on `OPTIONS`. No
  wildcard support — every origin must be named explicitly.
  `server.Handler`'s variadic `graphqlHandler ...http.Handler` parameter
  became a functional-option pattern (`WithGraphQL`, `WithCORS`) to carry
  this without breaking the many zero-option callers; the one caller that
  passed a GraphQL handler (`cmd/glyphuxd/main.go`) was updated.
- **Secrets provider** (`internal/config/secrets.go`): resolved the open
  design question in favor of the doc's own minimal option. Today's
  env-var-only loading was already clean with nothing to retire, so this is
  a single named function, `secret(key string) (string, bool)`, not a
  multi-implementation provider interface. `GLYPHUX_DB_DSN` now reads
  through it instead of a bare `os.Getenv`. If a real secrets manager is
  ever needed, only this function's body changes.
- **Input sanitization** (`internal/content/sanitize.go`): `contract.Field`
  validation only ever checked that a rich-text value was a string — no
  HTML sanitization existed at all. `sanitizeRichText` now runs every
  rich-text field's value (including per-locale values for localized
  fields) through `bluemonday.UGCPolicy()` after `validate()` confirms kind,
  in `Create`, `Update`, and `Rollback` — so every stored value is already
  safe by the time anything reads it back. Proven by a test that submits
  `<script>…</script><img onerror=…>` and asserts the read-back value keeps
  safe markup (`<p>`, `<img src>`) but not the script or the event handler.
- **Session hardening** (reviewed, no code change): `internal/identity`
  already had `HttpOnly`, HTTPS-conditional `Secure`, `SameSite=Lax` cookie
  flags, hashed (SHA-256) session tokens, an absolute 30-day expiry checked
  on every `Lookup`, and cascade-delete of sessions when a user is deleted
  (`ON DELETE CASCADE` in the sessions migration). No role/password-change
  endpoint exists yet anywhere in the codebase (that's parallel
  identity/MFA work), so "rotation on privilege change" has nothing to
  rotate against right now — added as a follow-up note rather than invented
  code against a mutation that doesn't exist. Conclusion: the existing
  state already meets a reasonable hardening bar; no gap was found worth
  closing without inventing a requirement the PRD doesn't ask for.
- **General API rate limiting** (`internal/server/server.go`): the open
  question was resolved in favor of including it. A new, small
  `requestLimiter` (distinct from `internal/api/ratelimit.go`'s
  `loginLimiter` — that one counts only *failed* logins for brute-force
  defense; this one counts *every* request, a materially different
  bookkeeping rule, so it's a separate type rather than a forced
  generalization) caps every remote address at `server.RequestsPerWindow`
  (300) requests/minute across the whole daemon surface, with `/healthz`
  and `/readyz` exempt so orchestrator liveness polling is never mistaken
  for abuse.

## Verification

- `go build ./... && go vet ./... && go test -race ./...` green throughout
  (ran after every commit, not just once at the end).
- Real end-to-end pass against the compiled `glyphuxd` binary (fresh
  temp-dir SQLite DB, real `/setup` HTML form via curl, not `go test`):
  - Setup: `POST /setup` with form-encoded `site_name`/`admin_email`/
    `admin_password`/`database=sqlite` → 200, wizard locked.
  - Login: `POST /api/v0/auth/login` → `Set-Cookie: glyphux_session=...;
    HttpOnly; SameSite=Lax` and `Set-Cookie: glyphux_csrf=...; SameSite=Lax`
    (no `HttpOnly` on the CSRF cookie, confirmed by inspection).
  - CSRF attack simulated: `PUT /api/v0/content-types/article` with only
    the session cookie (`-b cookies.txt`, no header) → `403
    {"error":"invalid CSRF token"}`. Same request with `-H "X-CSRF-Token:
    <cookie value>"` → `200`.
  - Bearer exemption: `POST /api/v0/content/article` with `Authorization:
    Bearer <token>` and a `<script>`/`onerror` payload, no cookies at all →
    `201`, and the response/read-back both show
    `<p>safe</p><img src="x">` — no `<script>`, no `onerror`.
  - CORS off by default: `curl -H "Origin: https://evil.example"
    /healthz` on a daemon booted with no `GLYPHUX_ALLOWED_ORIGINS` → no
    `Access-Control-*` header at all.
  - CORS on when configured: daemon rebooted with
    `GLYPHUX_ALLOWED_ORIGINS=https://trusted.example`; unlisted origin →
    still no CORS headers; listed origin GET → `Access-Control-Allow-Origin:
    https://trusted.example` + `-Allow-Credentials: true`; listed-origin
    `OPTIONS` preflight → `204` with `-Allow-Methods`/`-Allow-Headers`.
  - General rate limit: 310 rapid `GET /api/v0/content/ping` requests from
    one simulated remote address against a daemon configured with
    `GLYPHUX_ALLOWED_ORIGINS` → last request `429`; `/healthz` from the same
    address still `200` immediately after.
- `sdk-js`: `npm test` (30/30 passing, against the real compiled binary via
  its own global-setup harness) and `npm run build` — both green, no
  changes needed.
- `admin-ui`: `npm test` (12/12 passing) and `npm run build` — both green,
  no changes needed (confirms the bearer-only auth pattern already exempts
  it from CSRF).

## Files/Modules Touched

- `internal/api/csrf.go` (new), `internal/api/csrf_test.go` (new)
- `internal/api/auth.go`, `internal/api/api.go` — CSRF cookie issuance +
  route wiring
- `internal/api/auth_test.go`, `internal/api/content_test.go`,
  `internal/api/media_test.go` — test-helper `authCreds` refactor
- `internal/server/server.go`, `internal/server/server_test.go` — CORS
  middleware, functional-option `Handler`, general rate limiter
- `internal/config/config.go`, `internal/config/config_test.go` (new) —
  `AllowedOrigins`
- `internal/config/secrets.go` (new) — the one named secrets seam
- `internal/content/sanitize.go` (new), `internal/content/content.go`,
  `internal/content/content_test.go` — rich-text sanitization
- `cmd/glyphuxd/main.go` — updated `server.Handler` call to the new
  `WithGraphQL`/`WithCORS` options
- `go.mod`/`go.sum` — added `github.com/microcosm-cc/bluemonday`

## Acceptance Criteria

- [x] CSRF protection demonstrably blocks a cross-origin cookie-riding
      state-changing request and demonstrably allows the real admin SPA's
      own requests through (tested at the HTTP seam, not just unit-level).
- [x] CORS is configurable (opt-in, off by default) and demonstrably applies
      correct headers only when configured.
- [x] Secrets loading has one clear, documented seam (even if today's only
      implementation is env vars).
- [x] Rich-text content input is sanitized against stored-script injection,
      proven by a test that submits a malicious payload and asserts it's
      neutralized on read-back.
- [x] `go build/vet/test ./...` (+ `-race` on touched packages) green.
- [x] Real end-to-end verification against the compiled binary: confirm
      CSRF/CORS/sanitization behavior with curl against a real running
      instance, not just `go test`.
- [x] Admin UI's own test suite and build still pass if CSRF requires SPA
      changes (no changes were required; both suites re-run and green).
- [x] Tracking doc moved to completed with an honest, specific account of
      what's implemented.

## Open Questions Resolved

- General (non-login) API rate limiting: included, per a new
  `internal/server` per-remote-address limiter (see above), kept separate
  from `loginLimiter` rather than generalizing it, since the two count
  fundamentally different things (every request vs. only failures).

## Follow-ups (not part of this slice)

- GraphQL's `POST /graphql` endpoint also supports cookie auth
  (`internal/graphql/auth.go`) and carries mutations, but every GraphQL
  request — reads and mutations alike — arrives as `POST`, so a
  method-based CSRF gate can't distinguish them without parsing the query.
  The admin SPA doesn't use GraphQL (confirmed by inspection), so this was
  out of scope here; flagged for whoever next touches `internal/graphql`'s
  mutation surface for a cookie-authenticated external client.
- Session "rotation on privilege change" has no privilege-change mutation
  to hook yet (no password/role-change endpoint exists in the codebase
  today). Revisit once one lands.
