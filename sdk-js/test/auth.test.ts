import { describe, expect, it } from "vitest";
import { GlyphuxClient } from "../src/client.js";
import { GlyphuxApiError } from "../src/errors.js";
import { testEnv } from "./testenv.js";

describe("auth", () => {
  it("login returns the authenticated user and a bearer token", async () => {
    const env = testEnv();
    const client = new GlyphuxClient({ baseUrl: env.baseUrl });

    const result = await client.auth.login(env.adminEmail, env.adminPassword);

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
    const { token } = await client.auth.login(env.adminEmail, env.adminPassword);

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
});
