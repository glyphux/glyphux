import { describe, expect, it } from "vitest";
import { GlyphuxClient } from "../src/client.js";
import { GlyphuxApiError } from "../src/errors.js";
import type { CompositionPreset } from "../src/types.js";
import { testEnv } from "./testenv.js";

async function adminClient(): Promise<GlyphuxClient> {
  const env = testEnv();
  const client = new GlyphuxClient({ baseUrl: env.baseUrl });
  await client.auth.login(env.adminEmail, env.adminPassword);
  return client;
}

function heroPreset(name: string): CompositionPreset {
  return {
    contract_version: "composition-preset/v1",
    name,
    layout: {
      contract_version: "layout-composition/v1",
      regions: { main: { blocks: [{ type: "heading", props: { text: "Hi" } }] } },
    },
    manifest: {
      requires_contract: "layout-composition/v1",
      blocks: ["heading"],
      slots: ["main"],
    },
  };
}

describe("presets", () => {
  it("save() then get() round-trips the preset", async () => {
    const client = await adminClient();
    const saved = await client.presets.save(heroPreset(`hero-${Date.now()}`));
    expect(saved.id).toBeTruthy();

    const loaded = await client.presets.get(saved.id);
    expect(loaded.layout.regions.main.blocks[0].type).toBe("heading");
  });

  it("list() includes a saved preset", async () => {
    const client = await adminClient();
    const name = `hero-list-${Date.now()}`;
    const saved = await client.presets.save(heroPreset(name));

    const list = await client.presets.list();
    expect(list.some((p) => p.id === saved.id)).toBe(true);
  });

  it("save() with an unregistered block type throws a typed 422 error", async () => {
    const client = await adminClient();
    const bad = heroPreset(`bad-${Date.now()}`);
    bad.layout.regions.main.blocks[0].type = "does-not-exist";
    bad.manifest.blocks = ["does-not-exist"];

    let caught: unknown;
    try {
      await client.presets.save(bad);
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(GlyphuxApiError);
    expect((caught as GlyphuxApiError).status).toBe(422);
  });

  it("save() without authentication throws a typed 401 error", async () => {
    const env = testEnv();
    const anon = new GlyphuxClient({ baseUrl: env.baseUrl });

    let caught: unknown;
    try {
      await anon.presets.save(heroPreset(`anon-${Date.now()}`));
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(GlyphuxApiError);
    expect((caught as GlyphuxApiError).status).toBe(401);
  });

  it("check() reports a missing slot for a theme that doesn't declare it", async () => {
    const client = await adminClient();
    const saved = await client.presets.save(heroPreset(`check-${Date.now()}`));

    const result = await client.presets.check(saved.id, ["header", "footer"]);
    expect(result.compatible).toBe(false);
    expect(result.missing_slots).toContain("main");
  });

  it("check() is compatible when the theme declares the required slot", async () => {
    const client = await adminClient();
    const saved = await client.presets.save(heroPreset(`check-ok-${Date.now()}`));

    const result = await client.presets.check(saved.id, ["main"]);
    expect(result.compatible).toBe(true);
  });

  it("import() merges a compatible preset into the target route's layout", async () => {
    const client = await adminClient();
    const saved = await client.presets.save(heroPreset(`import-${Date.now()}`));
    const route = `preset-import-${Date.now()}`;

    const result = await client.presets.import(saved.id, route, ["main"]);
    expect(result.compatible).toBe(true);

    const layout = await client.layouts.get(route);
    expect(layout.regions.main.blocks[0].type).toBe("heading");
  });

  it("import() declines without merging when the theme lacks the declared slot", async () => {
    const client = await adminClient();
    const saved = await client.presets.save(heroPreset(`import-decline-${Date.now()}`));
    const route = `preset-decline-${Date.now()}`;

    const result = await client.presets.import(saved.id, route, ["header"]);
    expect(result.compatible).toBe(false);
    expect(result.missing_slots).toContain("main");

    let caught: unknown;
    try {
      await client.layouts.get(route);
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(GlyphuxApiError);
    expect((caught as GlyphuxApiError).status).toBe(404);
  });
});
