import { beforeAll, describe, expect, it } from "vitest";
import { GlyphuxClient } from "../src/client.js";
import { GlyphuxApiError } from "../src/errors.js";
import { testEnv } from "./testenv.js";

// The daemon's composition (seeded by sdk-js/testdata/seedcomposition, run
// from test/global-setup.ts) declares one content type, "post", with fields
// title (string, required), body (string), featured (boolean), and
// hero_image (media).
async function adminClient(): Promise<GlyphuxClient> {
  const env = testEnv();
  const client = new GlyphuxClient({ baseUrl: env.baseUrl });
  await client.auth.login(env.adminEmail, env.adminPassword);
  return client;
}

describe("content", () => {
  it("create() persists a new draft item at version 1", async () => {
    const client = await adminClient();

    const item = await client.content.create("post", { title: "Hello world" });

    expect(item.type).toBe("post");
    expect(item.data).toEqual({ title: "Hello world" });
    expect(item.status).toBe("draft");
    expect(item.version).toBe(1);
    expect(typeof item.id).toBe("string");
    expect(item.id.length).toBeGreaterThan(0);
  });

  it("create() rejects an unknown content type as a typed 404 error", async () => {
    const client = await adminClient();

    let caught: unknown;
    try {
      await client.content.create("no-such-type", { title: "x" });
    } catch (err) {
      caught = err;
    }

    expect(caught).toBeInstanceOf(GlyphuxApiError);
    expect((caught as InstanceType<typeof GlyphuxApiError>).status).toBe(404);
  });

  it("create() reports validation failures as a 422 with field issues", async () => {
    const client = await adminClient();

    let caught: unknown;
    try {
      // "post" requires "title" (see composition seeded above).
      await client.content.create("post", { body: "no title here" });
    } catch (err) {
      caught = err;
    }

    const err = caught as InstanceType<typeof GlyphuxApiError>;
    expect(err).toBeInstanceOf(GlyphuxApiError);
    expect(err.status).toBe(422);
    expect(err.issues).toBeDefined();
    expect(err.issues!.length).toBeGreaterThan(0);
    expect(err.issues![0]).toContain("title");
  });

  it("get() returns a previously created item by id", async () => {
    const client = await adminClient();
    const created = await client.content.create("post", { title: "Fetch me" });

    const fetched = await client.content.get("post", created.id);

    expect(fetched.id).toBe(created.id);
    expect(fetched.data).toEqual({ title: "Fetch me" });
  });

  it("get() reports a missing item as a typed 404 error", async () => {
    const client = await adminClient();

    let caught: unknown;
    try {
      await client.content.get("post", "no-such-id");
    } catch (err) {
      caught = err;
    }

    expect((caught as InstanceType<typeof GlyphuxApiError>).status).toBe(404);
  });

  it("list() includes a created item for its type", async () => {
    const client = await adminClient();
    const created = await client.content.create("post", { title: "Listed item" });

    const items = await client.content.list("post");

    expect(items.some((i) => i.id === created.id)).toBe(true);
  });

  it("update() overwrites the item's data and bumps its version", async () => {
    const client = await adminClient();
    const created = await client.content.create("post", { title: "Original" });

    const updated = await client.content.update("post", created.id, { title: "Changed" });

    expect(updated.data).toEqual({ title: "Changed" });
    expect(updated.version).toBe(2);
  });

  it("delete() removes the item so a later get() 404s", async () => {
    const client = await adminClient();
    const created = await client.content.create("post", { title: "Doomed" });

    await client.content.delete("post", created.id);

    let caught: unknown;
    try {
      await client.content.get("post", created.id);
    } catch (err) {
      caught = err;
    }
    expect((caught as InstanceType<typeof GlyphuxApiError>).status).toBe(404);
  });

  it("publish() then unpublish() toggle an item's status", async () => {
    const client = await adminClient();
    const created = await client.content.create("post", { title: "Publish me" });

    const published = await client.content.publish("post", created.id);
    expect(published.status).toBe("published");

    const unpublished = await client.content.unpublish("post", created.id);
    expect(unpublished.status).toBe("draft");
  });

  it("listVersions() and rollback() expose an item's version history", async () => {
    const client = await adminClient();
    const created = await client.content.create("post", { title: "v1" });
    await client.content.update("post", created.id, { title: "v2" });

    const versions = await client.content.listVersions("post", created.id);
    expect(versions.map((v) => v.version).sort()).toEqual([1, 2]);
    expect(versions.find((v) => v.version === 1)?.data).toEqual({ title: "v1" });

    const rolledBack = await client.content.rollback("post", created.id, 1);
    expect(rolledBack.data).toEqual({ title: "v1" });
    expect(rolledBack.version).toBe(3);
  });
});
