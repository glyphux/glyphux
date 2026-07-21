import { describe, expect, it } from "vitest";
import { GlyphuxClient } from "../src/client.js";
import { GlyphuxApiError } from "../src/errors.js";
import type { CompositionBundle } from "../src/types.js";
import { testEnv } from "./testenv.js";

async function adminClient(): Promise<GlyphuxClient> {
  const env = testEnv();
  const client = new GlyphuxClient({ baseUrl: env.baseUrl });
  await client.auth.login(env.adminEmail, env.adminPassword);
  return client;
}

function starterSite(homeRoute: string): CompositionBundle {
  return {
    contract_version: "composition-bundle/v1",
    name: `starter-site-${Date.now()}`,
    theme: "starter",
    pages: {
      [homeRoute]: {
        contract_version: "layout-composition/v1",
        regions: { main: { blocks: [{ type: "heading", props: { text: "Hi" } }] } },
      },
    },
    sample_content: [{ type: "post", data: { title: "Hello world" } }],
    manifest: {
      requires_contract: "layout-composition/v1",
      blocks: ["heading"],
      slots: ["main"],
    },
  };
}

describe("bundles", () => {
  it("save() then get() round-trips the bundle", async () => {
    const client = await adminClient();
    const saved = await client.bundles.save(starterSite(`home-${Date.now()}`));
    expect(saved.id).toBeTruthy();

    const loaded = await client.bundles.get(saved.id);
    expect(loaded.name).toBe(saved.name);
  });

  it("import() merges pages and creates sample content when compatible", async () => {
    const client = await adminClient();
    const route = `bundle-home-${Date.now()}`;
    const saved = await client.bundles.save(starterSite(route));

    const result = await client.bundles.import(saved.id, ["main"]);
    expect(result.compat.compatible).toBe(true);
    expect(result.imported_pages).toContain(route);
    expect(result.created_content?.length).toBe(1);

    const layout = await client.layouts.get(route);
    expect(layout.regions.main.blocks[0].type).toBe("heading");
  });

  it("import() declines to merge or create anything when incompatible", async () => {
    const client = await adminClient();
    const route = `bundle-decline-${Date.now()}`;
    const saved = await client.bundles.save(starterSite(route));

    const result = await client.bundles.import(saved.id, ["header", "footer"]);
    expect(result.compat.compatible).toBe(false);
    expect(result.imported_pages ?? []).toHaveLength(0);

    let caught: unknown;
    try {
      await client.layouts.get(route);
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(GlyphuxApiError);
    expect((caught as GlyphuxApiError).status).toBe(404);
  });

  it("save() without authentication throws a typed 401 error", async () => {
    const env = testEnv();
    const anon = new GlyphuxClient({ baseUrl: env.baseUrl });

    let caught: unknown;
    try {
      await anon.bundles.save(starterSite(`anon-${Date.now()}`));
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(GlyphuxApiError);
    expect((caught as GlyphuxApiError).status).toBe(401);
  });
});
