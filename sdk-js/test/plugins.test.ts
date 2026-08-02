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

// These tests run in declaration order against the shared long-lived test
// daemon (global-setup.ts boots exactly one), so they deliberately extend
// shared consent state rather than reset it: "undecided on a fresh daemon"
// must be the first test, and later tests assert only on the plugins they
// themselves decided (or on state that is order-independent, like the
// exceeds-request 422 and the non-admin 403).

describe("plugins", () => {
  it("list() surfaces every first-party plugin as undecided on a fresh daemon", async () => {
    const client = await adminClient();

    const plugins = await client.plugins.list();

    const names = plugins.map((p) => p.name).sort();
    expect(names).toEqual(["commerce", "forms", "membership", "notifications", "seo"]);
    for (const p of plugins) {
      expect(p.status).toBe("undecided");
      expect(p.granted).toBeUndefined();
      expect(p.fingerprint).toMatch(/^[0-9a-f]{64}$/);
    }
    // The commerce plugin requests payments charge/refund — its manifest's
    // declared surface is what the screen must present.
    const commerce = plugins.find((p) => p.name === "commerce")!;
    expect(commerce.requested.api).toContainEqual({
      capability: "payments",
      scopes: ["charge", "refund"],
    });
  });

  it("decide() with decision=denied records an explicit denial, and list() distinguishes denied from undecided", async () => {
    const client = await adminClient();

    const decision = await client.plugins.decide("commerce", { decision: "denied" });

    expect(decision.status).toBe("denied");
    expect(decision.plugin_name).toBe("commerce");
    expect(decision.granted_api).toEqual([]);
    expect(decision.decided_by).toBeGreaterThan(0);

    const commerce = (await client.plugins.list()).find((p) => p.name === "commerce")!;
    expect(commerce.status).toBe("denied"); // a decision exists — not "undecided"
    expect(commerce.granted).toBeUndefined(); // but nothing was granted
  });

  it("decide() with decision=partial grants exactly the requested subset", async () => {
    const client = await adminClient();

    const decision = await client.plugins.decide("forms", {
      decision: "partial",
      granted_api: [{ capability: "content", scopes: ["read"] }],
    });

    expect(decision.status).toBe("partial");
    expect(decision.granted_api).toEqual([{ capability: "content", scopes: ["read"] }]);

    const forms = (await client.plugins.list()).find((p) => p.name === "forms")!;
    expect(forms.status).toBe("partial");
    expect(forms.granted).toEqual({
      api: [{ capability: "content", scopes: ["read"] }],
      permissions: [],
    });
  });

  it("consentRequests() lists only plugins without a live decision (denied stays pending)", async () => {
    const client = await adminClient();

    const requests = await client.plugins.consentRequests();

    const names = requests.map((r) => r.name).sort();
    // commerce was denied above → still needs re-consent; forms has a live
    // partial decision → not listed.
    expect(names).toContain("commerce");
    expect(names).not.toContain("forms");
    const commerce = requests.find((r) => r.name === "commerce")!;
    expect(commerce.api).toContainEqual({ capability: "payments", scopes: ["charge", "refund"] });
  });

  it("decide() rejects a grant exceeding the request as a typed 422", async () => {
    const client = await adminClient();

    // commerce never declared the seo capability — granting it must fail
    // with ErrGrantExceedsRequest (422), not silently persist.
    let caught: unknown;
    try {
      await client.plugins.decide("commerce", {
        decision: "partial",
        granted_api: [{ capability: "seo", scopes: ["read"] }],
      });
    } catch (err) {
      caught = err;
    }

    expect(caught).toBeInstanceOf(GlyphuxApiError);
    expect((caught as InstanceType<typeof GlyphuxApiError>).status).toBe(422);
  });

  it("every plugins endpoint rejects a non-admin caller as a typed 403", async () => {
    const admin = await adminClient();
    const email = `plugins-editor-${Date.now()}@example.com`;
    await admin.users.create(email, "a decent password", "editor");

    const env = testEnv();
    const editor = new GlyphuxClient({ baseUrl: env.baseUrl });
    await editor.auth.login(email, "a decent password");

    await expect(editor.plugins.list()).rejects.toMatchObject({ status: 403 });
    await expect(editor.plugins.consentRequests()).rejects.toMatchObject({ status: 403 });
    await expect(editor.plugins.decide("forms", { decision: "denied" })).rejects.toMatchObject({ status: 403 });
  });
});
