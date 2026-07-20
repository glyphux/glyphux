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

describe("content-types", () => {
  it("list() includes the seeded \"post\" type", async () => {
    const client = await adminClient();
    const types = await client.contentTypes.list();
    expect(types.post).toBeDefined();
    expect(types.post.fields.title.type).toBe("string");
  });

  it("define() creates a new content type", async () => {
    const client = await adminClient();

    const defined = await client.contentTypes.define("tag", {
      fields: { name: { type: "string", required: true } },
    });

    expect(defined.fields.name.type).toBe("string");
    expect(defined.fields.name.required).toBe(true);

    const types = await client.contentTypes.list();
    expect(types.tag).toBeDefined();
  });

  it("define() with an invalid shape throws a typed 422 error", async () => {
    const client = await adminClient();

    let caught: unknown;
    try {
      await client.contentTypes.define("broken", {
        fields: { linked: { type: "relation", to: "does-not-exist" } },
      });
    } catch (err) {
      caught = err;
    }

    expect(caught).toBeInstanceOf(GlyphuxApiError);
    expect((caught as InstanceType<typeof GlyphuxApiError>).status).toBe(422);
  });

  it("delete() removes an empty content type", async () => {
    const client = await adminClient();
    await client.contentTypes.define("scratch", { fields: { name: { type: "string" } } });

    await client.contentTypes.delete("scratch");

    const types = await client.contentTypes.list();
    expect(types.scratch).toBeUndefined();
  });

  it("delete() is blocked with a typed 409 error when items exist", async () => {
    const client = await adminClient();
    await client.content.create("post", { title: "keeps post alive" });

    let caught: unknown;
    try {
      await client.contentTypes.delete("post");
    } catch (err) {
      caught = err;
    }

    expect((caught as InstanceType<typeof GlyphuxApiError>).status).toBe(409);
  });
});
