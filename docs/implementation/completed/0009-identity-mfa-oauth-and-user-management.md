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

Completed

## Current Decisions

- MFA: TOTP (RFC 6238), implemented from scratch in
  `internal/identity/totp.go` (HMAC-SHA1, 6-digit codes, 30s step, ±1 step
  clock-skew tolerance) — no external dependency, works with any
  authenticator app. Enrollment is two steps (`BeginMFAEnrollment` stores
  an unconfirmed secret and returns it plus an `otpauth://` URI for a QR
  code; `ConfirmMFAEnrollment` verifies the first real code before flipping
  `mfa_enabled` on). Login is two steps once enabled: password success
  returns an MFA challenge token instead of a session
  (`BeginMFAChallenge`/`ResolveMFAChallenge`), which the client resolves
  with a TOTP or recovery code at `POST /api/v0/auth/mfa/verify`. Opt-in
  per account, not global.
- MFA recovery codes: **in scope, not cut.** 8 one-time codes are minted at
  `ConfirmMFAEnrollment`, shown exactly once, stored only as SHA-256
  hashes, and each is consumed (removed from the stored set) the one time
  it's used via `VerifyMFA`.
- OAuth/social provider chosen: **GitHub**, not Google. Rationale: a
  self-hosted instance can register a GitHub OAuth App with zero
  review/verification friction — no OAuth consent-screen configuration, no
  domain-ownership proof, and it works immediately with an
  `http://localhost` redirect URI during development/testing. Google gates
  meaningful scopes behind a consent-screen verification process aimed at
  production public apps, which is real friction for a self-hosted
  instance with no fixed public domain. `internal/identity/oauth.go`'s
  `OAuthProvider` is a small config struct (client id/secret,
  authorize/token/userinfo endpoints, optional GitHub-shaped emails
  endpoint) so a second provider is a registration, not a rewrite.
- Account linking rule (implemented in
  `identity.Service.FindOrCreateOAuthUser`): an already-linked
  provider+subject resolves directly to its account. Otherwise, match the
  provider's reported email (case-insensitively) against an existing
  account and link it (sets `oauth_provider`/`oauth_subject` on that row)
  going forward. Otherwise create a new account with the **viewer** role
  (least privilege; an admin promotes it via `UpdateRole` like any other
  account). The provider's email **must** be reported verified —
  `FindOrCreateOAuthUser` refuses outright (`ErrOAuthEmailUnverified`) on an
  unverified email, since trusting one would let anyone claiming an
  arbitrary unverified address take over or shadow-create an account for
  it. A v1 limitation: one linked OAuth identity per account (no
  multi-provider linking) — not required by this slice, noted as a
  possible future enhancement.
- OAuth verification method: **no live-provider network egress was
  available in this sandbox** (no real GitHub OAuth App credentials, no
  outbound network access to github.com from the test/dev environment).
  The flow is verified two ways instead: (1) `internal/identity/oauth_test.go`
  and `internal/api/oauth_test.go` drive the full authorization-code
  exchange against a protocol-accurate fake GitHub server
  (`httptest.Server` serving GitHub's exact endpoint shapes: POST
  `/login/oauth/access_token` with `Accept: application/json`, GET `/user`,
  GET `/user/emails`) through the real production code path
  (`OAuthManager.Exchange`, `handleOAuthStart`/`handleOAuthCallback`); (2)
  the real compiled `glyphuxd` binary was started with fake
  `GLYPHUX_OAUTH_GITHUB_CLIENT_ID/_SECRET` and `GET
  /api/v0/auth/oauth/github/start` was confirmed to redirect to the real
  `https://github.com/login/oauth/authorize` with the correct
  `client_id`/`redirect_uri`/`scope`/`state` query parameters and set the
  CSRF state cookie — i.e. everything up to (not including) an actual
  browser/GitHub round trip was verified against the real thing. Clicking
  through GitHub's own consent screen and confirming the callback resolves
  with a real GitHub account was **not** performed — that would require a
  registered GitHub OAuth App and a real GitHub account, neither available
  here. This is a documented gap, not a silent one.
- User management completion: added `UpdateRole` and
  `Deactivate`/`Reactivate` to `internal/identity`. Checked
  `internal/content` for any `created_by`/user-id reference before
  deciding hard-delete vs. deactivate — found none (content items carry no
  author/user reference anywhere in the schema or code). Despite that,
  **deactivate was still chosen over hard delete**: it's the safer default
  (trivially reversible by an admin if done in error, and it revokes every
  live session + blocks future login, which a hard delete of the row would
  also require reasoning about session cleanup for anyway), and "delete" as
  a user-facing verb implies destroying history an admin might later want
  back. A `Reactivate` endpoint was added alongside so deactivation isn't a
  one-way door. Both REST (`PATCH /api/v0/users/{id}/role`, `POST
  .../deactivate`, `POST .../reactivate`) and GraphQL
  (`updateUserRole`/`deactivateUser`/`reactivateUser` mutations) got the
  new operations; both SDKs (`pkg/client`, `sdk-js`) got matching methods;
  the admin UI's Users page got an editable role `Select` per row (disabled
  for the caller's own account, to avoid self-lockout) and a
  Deactivate/Reactivate control (deactivate behind a confirm dialog,
  mirroring the media library's existing delete-confirmation pattern) —
  all admin-only via the existing `users:manage` capability gate.
- All new mutating `internal/identity` operations that are admin-acting-on-
  another-account (`UpdateRole`, `Deactivate`, `Reactivate`) take a
  `*permission.Principal` and check `permission.AllowsPrincipal` themselves,
  per slice 0006's domain-boundary pattern (defense-in-depth alongside each
  transport's own fast-fail `requireCapability` check). Self-service MFA
  operations (`BeginMFAEnrollment`, `ConfirmMFAEnrollment`, `VerifyMFA`,
  `DisableMFA`, `BeginMFAChallenge`, `ResolveMFAChallenge`) do **not** take
  a `Principal`: there is no "admin over someone else's account" capability
  to check for them — the transport layer only ever passes the
  authenticated caller's own user id, so the operation is inherently
  self-scoped rather than a capability-gated one. `CreateUser`/`ListUsers`
  (pre-existing, from before slice 0006) were left as-is — they still rely
  solely on the transport's `requireCapability(users:manage)` check, not a
  domain-layer one — noted below as an intentional non-goal for this slice
  rather than a silent gap.

## Open Questions (resolved)

- OAuth provider: **GitHub** (see Current Decisions above for the full
  rationale).
- MFA recovery codes: **in scope**, 8 codes per enrollment, SHA-256-hashed
  at rest, single-use.

## Follow-ups (not done in this slice)

- `identity.Service.CreateUser`/`ListUsers` still enforce capability only
  at the transport layer (REST/GraphQL `requireCapability`), not inside
  the domain method itself — unlike the new `UpdateRole`/`Deactivate`/
  `Reactivate`, which do. Retrofitting `CreateUser`/`ListUsers` to also
  take a `*permission.Principal` was out of scope for this slice (it
  predates slice 0006 and touches call sites across `internal/api`,
  `internal/graphql`, `internal/setup`, and every test that seeds an
  account) but is a reasonable near-term follow-up for full consistency
  with slice 0006's pattern.
- Multi-provider OAuth linking (more than one linked identity per account)
  is not supported — a v1 limitation, not required by this slice.
- A live-provider (real github.com, real registered OAuth App) click-through
  verification was not performed — see the OAuth verification method note
  above. Doing so requires a registered GitHub OAuth App and network egress
  neither available in this sandbox; the protocol-level verification
  (fake-server tests plus a real redirect to the real GitHub authorize URL
  from the compiled binary) is as far as this environment allows.
- No real browser (Playwright/Selenium) pass of the admin UI's MFA/OAuth/
  user-management flows was performed — verified instead via the admin
  UI's own component test suite (Vitest + Testing Library, mocking the SDK
  client per the project's existing seam) plus direct `curl` against the
  real compiled `glyphuxd` binary for every REST endpoint the UI calls.

## Files/Modules touched

- `internal/identity/totp.go` (+ `totp_test.go`) — RFC 6238 TOTP core.
- `internal/identity/mfa.go` (+ `mfa_test.go`) — enrollment, confirmation,
  verification, recovery codes, login-challenge issuance/resolution;
  migrations for `mfa_enabled`/`mfa_secret`/`mfa_recovery_codes` columns
  and the `mfa_challenges` table.
- `internal/identity/oauth.go` (+ `oauth_test.go`) — `OAuthProvider` config,
  `OAuthManager` (authorize URL, CSRF state, code exchange, userinfo/emails
  fetch), `FindOrCreateOAuthUser`; migration for `oauth_provider`/
  `oauth_subject` columns + partial unique index.
- `internal/identity/users_manage.go` (+ `users_manage_test.go`) —
  `UpdateRole`, `Deactivate`, `Reactivate`; migration for the `active`
  column.
- `internal/identity/identity.go` — `User` gained `MFAEnabled`/`Active`;
  `Authenticate` now rejects a deactivated account
  (`ErrAccountDeactivated`).
- `internal/identity/sessions.go` (+ test) — `RevokeAllForUser`; fixed a
  bug (see below) where `Lookup` never selected `mfa_enabled`/`active`.
- `internal/api/auth.go`, `mfa.go`, `oauth.go`, `users.go`, `api.go` — MFA
  challenge step wired into login; new MFA/OAuth/role/deactivate routes;
  `api.WithOAuth` option; config wiring for `GLYPHUX_OAUTH_GITHUB_CLIENT_ID/
  _SECRET`, `GLYPHUX_PUBLIC_URL` in `cmd/glyphuxd/main.go` and
  `internal/config/config.go`.
- `internal/graphql/schema/schema.graphql`, `schema.resolvers.go`,
  `models.go`, `errors.go` — `User.mfaEnabled`/`active`; `updateUserRole`,
  `deactivateUser`, `reactivateUser` mutations. (MFA/OAuth are REST-only,
  matching the existing precedent that login itself has no GraphQL
  mutation.)
- `pkg/client/auth.go`, `users.go` (+ tests) — `MFARequiredError`,
  `VerifyMFA`, `BeginMFAEnrollment`, `ConfirmMFAEnrollment`, `DisableMFA`;
  `UpdateRole`, `Deactivate`, `Reactivate`.
- `sdk-js/src/auth.ts`, `users.ts`, `types.ts`, `index.ts` (+ tests,
  `test/totp.ts`) — matching methods; `LoginResult` is now a discriminated
  union (`AuthenticatedSession | MfaChallenge`).
- `admin-ui/src/lib/auth-context.tsx`, `pages/LoginPage.tsx` (+ test) — MFA
  challenge step in the login UI.
- `admin-ui/src/pages/users/UsersPage.tsx` (+ new test) — editable role
  select, deactivate/reactivate controls, admin-only.

## Bugs found and fixed during end-to-end verification

Manual `curl`-driven verification against the real compiled `glyphuxd`
binary (not just `go test`) surfaced two real bugs neither the Go nor SDK
test suites had caught, because no existing assertion checked the `active`
field's value:

1. `identity.Service.userByID` selected `id, email, role, mfa_enabled` but
   not `active` — every response built from it (`ResolveMFAChallenge`,
   `UpdateRole`, `Deactivate`, `Reactivate`, `FindOrCreateOAuthUser`)
   reported `active: false` for accounts that were actually active.
2. `identity.Sessions.Lookup` (which backs every authenticated request —
   `requireUser`, `requireCapability`, `GET /api/v0/auth/me`, GraphQL's
   principal resolution) selected `id, email, role` but not
   `mfa_enabled`/`active` — every `/me` response and every authenticated
   handler's resolved principal always reported `mfaEnabled: false,
   active: false` regardless of the account's real state.

Both were fixed and regression assertions were added at the point of the
bug (`internal/identity/mfa_test.go`, `users_manage_test.go`,
`sessions_test.go`) and at the transport level
(`internal/api/users_manage_test.go`, `oauth_test.go`, `auth_test.go`).
Authorization itself was not affected by either bug (the `Role` field was
selected correctly throughout, and deactivation is enforced by revoking
sessions and blocking `Authenticate`, not a per-request `Active` check) —
these were response-payload correctness bugs, not security holes, but real
ones a live curl pass caught that `go test` alone had not.

## Acceptance Criteria

- [x] A user can enroll in TOTP MFA and subsequent logins require the code;
      verified via a real end-to-end login flow test (compute a valid TOTP
      code with the enrolled secret, submit it, confirm session issued;
      confirm a wrong/missing code is rejected). Verified at three levels:
      Go unit tests (`internal/identity/mfa_test.go`), HTTP-level Go tests
      with an independent TOTP implementation
      (`internal/api/mfa_test.go`), and a live `curl` pass against the
      compiled binary (enroll -> confirm with wrong code rejected -> confirm
      with right code -> login returns `mfaRequired` -> verify with wrong
      code rejected -> verify with right code issues a session).
- [x] At least one real OAuth2 provider flow works end-to-end — implemented
      against GitHub's real, documented endpoint shapes; verified against a
      protocol-accurate fake GitHub server in tests (both
      `internal/identity` and `internal/api`), plus a live curl pass
      against the compiled binary confirming the real redirect to
      `https://github.com/login/oauth/authorize` with correct parameters.
      A live-provider click-through was not performed (no registered
      GitHub OAuth App / network egress in this sandbox) — documented
      above, not silently skipped.
- [x] A user's role can be changed and an account can be
      deactivated/deleted through REST, GraphQL, both SDKs, and the admin
      UI, admin-only, with existing sessions revoked on deactivation.
- [x] `go build/vet/test ./...` (+ `-race`), `sdk-js` (34 tests) and
      `admin-ui` (17 tests) test suites, all green.
- [x] Real end-to-end verification against the compiled binary: setup,
      login, MFA enroll/confirm/challenge, role change, deactivate/
      reactivate, and the OAuth start redirect were all driven with `curl`
      against the actual `glyphuxd` binary (not just `go test`) — this is
      what surfaced and let us fix the two bugs above. A real
      Playwright/Selenium browser pass of the admin UI was not performed;
      the admin UI's own Vitest/Testing Library component suite plus
      direct REST verification of every endpoint it calls covers the same
      behavior at a different layer (documented above as a follow-up gap,
      not claimed as done).
- [x] Tracking doc moved to completed with an honest account of what's
      verified vs. assumed (this document).

## Risks (resolved)

- Real OAuth testing against a live third-party provider was not
  practical in this sandbox, as anticipated — implemented and verified
  against a protocol-accurate test double plus a live redirect check
  against the real GitHub endpoint, documented rather than claimed as a
  full live-provider verification.
- 0006 (domain-boundary enforcement) had already merged to `dev` before
  this slice's work began in earnest (this worktree was rebased onto
  `dev` at the start), so the new `UpdateRole`/`Deactivate`/`Reactivate`
  methods were written against that pattern from the start — no follow-up
  needed there.
