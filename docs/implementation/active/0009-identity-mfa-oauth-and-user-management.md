# Implementation: Identity MFA, OAuth/social providers, and full user management

## Goal

PRD §1.7 explicitly lists "accounts, sessions, credentials, OAuth/social
providers, MFA" as Phase-1 scope. A full audit (2026-07-18) found only
username/password + PBKDF2 + opaque bearer/cookie sessions exist —
`internal/identity/identity.go` still carries a stale comment "OAuth, and
MFA arrive in slice 1.7" despite 1.7 being marked done. No TOTP, no OAuth
client, no social login anywhere in the repo.

Separately, the same audit found user management (REST, GraphQL, both
SDKs, and the admin UI) only supports create + list — there is no way to
change a user's role or remove/deactivate an account through any surface,
which the Phase-1 DoD ("manage... identity") requires. Bundled into this
slice since both are identity-surface work touching the same files
(`internal/identity`, `internal/api`'s user endpoints, `internal/graphql`'s
user resolvers, both SDKs' users modules, `admin-ui/src/pages/users`).

## Owning Contexts

- (no CONTEXT.md yet — core-kernel: `internal/identity`; transports:
  `internal/api`, `internal/graphql`; clients: `pkg/client`, `sdk-js`,
  `admin-ui`)

## Status

In-flight

## Current Decisions

- MFA: TOTP (RFC 6238) is the standard, provider-agnostic choice (no
  external dependency, works with any authenticator app) — implement
  enrollment (generate secret + QR-encodable URI, verify first code to
  confirm enrollment), and a second login step (password succeeds ->
  TOTP challenge -> session issued) gated behind a per-user
  "MFA enabled" flag. Don't require MFA globally; it's opt-in per account
  for this slice (admin-forced MFA policy is a reasonable future
  enhancement, not required now).
- OAuth/social: implement the standard OAuth2 authorization-code flow
  against at least one real provider (e.g. Google or GitHub — pick
  whichever has the simplest well-documented OAuth2 endpoint set) as the
  proof, with the provider list designed to be extensible (a small
  per-provider config struct: client ID/secret, authorize/token endpoints,
  userinfo endpoint) rather than hardcoding just one provider inline.
  Account linking: match by verified email to an existing account, or
  create a new account if none exists — decide and document the exact
  rule here once implemented.
- User management completion: add role-change and delete/deactivate
  endpoints. Prefer deactivate (revoke all sessions + block future auth)
  over hard delete for accounts with content-authorship history (avoids
  orphaning `created_by`-style references if any exist — check
  `internal/content` for whether content items reference a user ID
  anywhere before deciding); if no such references exist, a real delete is
  fine and simpler. Both REST and GraphQL need the new endpoints; both
  SDKs need matching methods; the admin UI's Users page needs edit-role and
  deactivate/delete controls, admin-only (mirrors existing users:manage
  capability gating).
- All new domain operations must include their own capability checks per
  whatever `internal/identity`/`internal/permission` boundary-enforcement
  pattern slice 0006 established — check `dev`'s state for 0006 before
  starting; if 0006 hasn't merged, add checks at the transport layer for
  now and note in Open Questions that domain-layer enforcement needs a
  follow-up once 0006 lands, rather than blocking this whole slice on it.

## Open Questions

- Exact OAuth provider to implement first (Google vs GitHub vs something
  else) — pick based on which has the least implementation friction (no
  paid tier requirement, straightforward redirect URI setup for
  self-hosted instances), record the choice and why here.
- Whether MFA recovery codes are in scope for this slice (losing the TOTP
  device with no recovery path is a real support burden) — include a basic
  recovery-code mechanism (generate N one-time codes at enrollment) unless
  it turns out to meaningfully bloat scope, in which case note it as a
  explicit near-term follow-up rather than silently omitting it.

## Files/Modules Expected

- `internal/identity/identity.go`, `sessions.go`, new `mfa.go`, `oauth.go`
  (+ tests) — TOTP enrollment/verification, OAuth2 flow, migrations for any
  new columns/tables (mfa_secret, mfa_enabled, oauth_provider/oauth_subject,
  recovery codes)
- `internal/api/users.go` (or wherever user endpoints live), `auth.go` —
  MFA challenge step in login, OAuth redirect/callback routes, role-change
  and delete/deactivate endpoints
- `internal/graphql/*.go` — matching mutations/queries
- `pkg/client/users.go`, `pkg/client/auth.go` (+ tests) — matching methods
- `sdk-js/src/users.ts`, `sdk-js/src/auth.ts` (+ tests) — matching methods
- `admin-ui/src/pages/users/UsersPage.tsx` — edit-role, deactivate/delete
  UI; login flow updated for an MFA challenge step if enabled
- Tracking doc moved to `docs/implementation/completed/` when done

## Acceptance Criteria

- [ ] A user can enroll in TOTP MFA and subsequent logins require the code;
      verified via a real end-to-end login flow test (compute a valid TOTP
      code with the enrolled secret, submit it, confirm session issued;
      confirm a wrong/missing code is rejected).
- [ ] At least one real OAuth2 provider flow works end-to-end against that
      provider's actual endpoints in a test (using a test/sandbox app
      registration, or a faked-but-protocol-accurate OAuth server in tests
      if a live provider isn't practical in CI — document which approach
      was used and why).
- [ ] A user's role can be changed and an account can be
      deactivated/deleted through REST, GraphQL, both SDKs, and the admin
      UI, admin-only, with existing sessions revoked on deactivation.
- [ ] `go build/vet/test ./...` (+ `-race` on touched packages), `sdk-js`
      and `admin-ui` test suites, all green.
- [ ] Real end-to-end verification against the compiled binary and, where
      feasible, a real browser pass of the admin UI's login/MFA/OAuth and
      user-management flows.
- [ ] Tracking doc moved to completed with an honest account of what's
      verified vs. assumed.

## Risks

- Real OAuth testing against a live third-party provider from an
  autonomous background agent may not be practical (no real client
  credentials, no network egress in the sandbox) — if so, implement the
  flow against the standard protocol shape with a realistic test double
  and say so explicitly rather than claiming a live-provider verification
  that didn't happen.
- Overlaps with 0011 (media/library) not expected, but overlaps with 0006
  (domain-boundary enforcement) on `internal/identity` if both are
  in flight simultaneously — check `dev`'s log for 0006 merged before
  starting to minimize conflict.
