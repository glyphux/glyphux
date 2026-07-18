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

  it("updateRole() changes an account's role", async () => {
    const admin = await adminClient();
    const email = `role-${Date.now()}@example.com`;
    const created = await admin.users.create(email, "a decent password", "editor");

    const updated = await admin.users.updateRole(created.id, "viewer");

    expect(updated.role).toBe("viewer");
  });

  it("updateRole() is rejected for a non-admin caller as a typed 403 error", async () => {
    const admin = await adminClient();
    const targetEmail = `role-target-${Date.now()}@example.com`;
    const target = await admin.users.create(targetEmail, "a decent password", "viewer");
    const callerEmail = `role-caller-${Date.now()}@example.com`;
    await admin.users.create(callerEmail, "a decent password", "editor");

    const env = testEnv();
    const editorClient = new GlyphuxClient({ baseUrl: env.baseUrl });
    await editorClient.auth.login(callerEmail, "a decent password");

    await expect(editorClient.users.updateRole(target.id, "admin")).rejects.toMatchObject({ status: 403 });
  });

  it("deactivate() blocks future logins and revokes existing sessions; reactivate() restores it", async () => {
    const admin = await adminClient();
    const env = testEnv();
    const email = `deactivate-${Date.now()}@example.com`;
    const password = "a decent password";
    const created = await admin.users.create(email, password, "editor");

    const target = new GlyphuxClient({ baseUrl: env.baseUrl });
    await target.auth.login(email, password);
    // The session works before deactivation.
    await target.auth.me();

    await admin.users.deactivate(created.id);

    // The existing session is now dead.
    await expect(target.auth.me()).rejects.toMatchObject({ status: 401 });
    // And a fresh login attempt is blocked.
    const relogin = new GlyphuxClient({ baseUrl: env.baseUrl });
    await expect(relogin.auth.login(email, password)).rejects.toMatchObject({ status: 403 });

    await admin.users.reactivate(created.id);
    const afterReactivation = await relogin.auth.login(email, password);
    if ("mfaRequired" in afterReactivation) throw new Error("this account has no MFA enabled");
    expect(afterReactivation.email).toBe(email);
  });
});
