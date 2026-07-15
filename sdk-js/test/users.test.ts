import { describe, expect, it } from "vitest";
import { GlyphuxClient } from "../src/client.js";
import { GlyphuxApiError } from "../src/errors.js";
import { testEnv } from "./testenv.js";

async function adminClient(): Promise<GlyphuxClient> {
  const env = testEnv();
  const client = new GlyphuxClient({ baseUrl: env.baseUrl });
  await client.auth.login(env.adminEmail, env.adminPassword);
  return client;
}

describe("users", () => {
  it("create() provisions a new account with the given role", async () => {
    const client = await adminClient();

    const user = await client.users.create(`editor-${Date.now()}@example.com`, "a decent password", "editor");

    expect(user.role).toBe("editor");
    expect(typeof user.id).toBe("number");
  });

  it("list() includes a newly created user", async () => {
    const client = await adminClient();
    const email = `viewer-${Date.now()}@example.com`;
    const created = await client.users.create(email, "a decent password", "viewer");

    const users = await client.users.list();

    expect(users.some((u) => u.id === created.id && u.email === email)).toBe(true);
  });

  it("create() is rejected for a non-admin caller as a typed 403 error", async () => {
    const admin = await adminClient();
    const email = `norole-${Date.now()}@example.com`;
    await admin.users.create(email, "a decent password", "viewer");

    const env = testEnv();
    const viewerClient = new GlyphuxClient({ baseUrl: env.baseUrl });
    await viewerClient.auth.login(email, "a decent password");

    let caught: unknown;
    try {
      await viewerClient.users.create(`blocked-${Date.now()}@example.com`, "a decent password", "viewer");
    } catch (err) {
      caught = err;
    }

    expect(caught).toBeInstanceOf(GlyphuxApiError);
    expect((caught as InstanceType<typeof GlyphuxApiError>).status).toBe(403);
  });
});
