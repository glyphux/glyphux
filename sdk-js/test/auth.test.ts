import { describe, expect, it } from "vitest";
import { GlyphuxClient } from "../src/client.js";
import { GlyphuxApiError } from "../src/errors.js";
import { testEnv } from "./testenv.js";
import { totpCode } from "./totp.js";

describe("auth", () => {
  it("login returns the authenticated user and a bearer token", async () => {
    const env = testEnv();
    const client = new GlyphuxClient({ baseUrl: env.baseUrl });

    const result = await client.auth.login(env.adminEmail, env.adminPassword);
    if ("mfaRequired" in result) throw new Error("admin fixture has no MFA enabled; expected a session");

    expect(result.email).toBe(env.adminEmail);
    expect(result.role).toBe("admin");
    expect(typeof result.id).toBe("number");
    expect(typeof result.token).toBe("string");
    expect(result.token.length).toBeGreaterThan(0);
  });

  it("me() returns 401 as a typed GlyphuxApiError when unauthenticated", async () => {
    const env = testEnv();
    const client = new GlyphuxClient({ baseUrl: env.baseUrl });

    await expect(client.auth.me()).rejects.toMatchObject({
      name: "GlyphuxApiError",
      status: 401,
    });
  });

  it("me() returns the current user once logged in", async () => {
    const env = testEnv();
    const client = new GlyphuxClient({ baseUrl: env.baseUrl });
    await client.auth.login(env.adminEmail, env.adminPassword);

    const me = await client.auth.me();

    expect(me.email).toBe(env.adminEmail);
    expect(me.role).toBe("admin");
  });

  it("logout() revokes the session token server-side", async () => {
    const env = testEnv();
    const client = new GlyphuxClient({ baseUrl: env.baseUrl });
    const result = await client.auth.login(env.adminEmail, env.adminPassword);
    if ("mfaRequired" in result) throw new Error("admin fixture has no MFA enabled; expected a session");
    const { token } = result;

    await client.auth.logout();

    // A fresh client presenting the now-revoked token must be rejected —
    // proves the server revoked it, not just that this client forgot it.
    const staleClient = new GlyphuxClient({ baseUrl: env.baseUrl, token });
    await expect(staleClient.auth.me()).rejects.toMatchObject({ status: 401 });
  });

  // A single failed-login assertion only: the daemon rate-limits failed
  // logins per remote address (loginMaxAttempts = 10 / minute, see
  // internal/api/ratelimit.go) and every test in this suite shares one
  // address (127.0.0.1), so repeated invalid-credential attempts across the
  // file would eventually 429 instead of 401.
  it("login() rejects invalid credentials as a typed 401 error", async () => {
    const env = testEnv();
    const client = new GlyphuxClient({ baseUrl: env.baseUrl });

    let caught: unknown;
    try {
      await client.auth.login(env.adminEmail, "wrong password");
    } catch (err) {
      caught = err;
    }

    expect(caught).toBeInstanceOf(GlyphuxApiError);
    expect((caught as InstanceType<typeof GlyphuxApiError>).status).toBe(401);
  });

  // A dedicated account, not the shared admin fixture: enabling MFA on the
  // admin account would break every other test file's plain-login
  // assumption against the same long-lived daemon.
  it("drives a full TOTP MFA round trip: enroll, confirm, login challenge", async () => {
    const env = testEnv();
    const admin = new GlyphuxClient({ baseUrl: env.baseUrl });
    await admin.auth.login(env.adminEmail, env.adminPassword);
    const email = `mfa-${Date.now()}@example.com`;
    const password = "a decent password";
    await admin.users.create(email, password, "viewer");

    const client = new GlyphuxClient({ baseUrl: env.baseUrl });
    await client.auth.login(email, password);

    const { secret, otpauthUrl } = await client.auth.beginMfaEnrollment();
    expect(secret.length).toBeGreaterThan(0);
    expect(otpauthUrl).toContain("otpauth://totp/");

    // A wrong code does not confirm enrollment.
    await expect(client.auth.confirmMfaEnrollment("000000")).rejects.toMatchObject({ status: 401 });

    const { recoveryCodes } = await client.auth.confirmMfaEnrollment(totpCode(secret));
    expect(recoveryCodes.length).toBeGreaterThan(0);

    // A fresh login for this account now returns an MFA challenge, not a
    // session.
    const fresh = new GlyphuxClient({ baseUrl: env.baseUrl });
    const loginResult = await fresh.auth.login(email, password);
    if (!("mfaRequired" in loginResult)) throw new Error("expected an MFA challenge");
    expect(loginResult.mfaToken.length).toBeGreaterThan(0);

    // A wrong code does not resolve the challenge.
    await expect(fresh.auth.verifyMfa(loginResult.mfaToken, "000000")).rejects.toMatchObject({ status: 401 });

    // The right code does, issuing a real session.
    const session = await fresh.auth.verifyMfa(loginResult.mfaToken, totpCode(secret));
    expect(session.email).toBe(email);
    expect(session.token.length).toBeGreaterThan(0);
    const me = await fresh.auth.me();
    expect(me.email).toBe(email);
  });
});
