import { describe, expect, it } from "vitest";
import { GlyphuxClient } from "../src/client.js";
import { GlyphuxApiError } from "../src/errors.js";
import type { Layout } from "../src/types.js";
import { testEnv } from "./testenv.js";

async function adminClient(): Promise<GlyphuxClient> {
  const env = testEnv();
  const client = new GlyphuxClient({ baseUrl: env.baseUrl });
  await client.auth.login(env.adminEmail, env.adminPassword);
  return client;
}

function headingLayout(text: string): Layout {
  return {
    contract_version: "layout-composition/v1",
    regions: {
      main: { blocks: [{ type: "heading", props: { text } }] },
    },
  };
}

describe("layouts", () => {
  it("get() throws a typed 404 for an unsaved route", async () => {
    const client = await adminClient();
    let caught: unknown;
    try {
      await client.layouts.get(`no-such-route-${Date.now()}`);
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(GlyphuxApiError);
    expect((caught as GlyphuxApiError).status).toBe(404);
  });

  it("save() then get() round-trips the layout", async () => {
    const client = await adminClient();
    const route = `home-${Date.now()}`;

    const saved = await client.layouts.save(route, headingLayout("Welcome"));
    expect(saved.regions.main.blocks[0].props?.text).toBe("Welcome");

    const loaded = await client.layouts.get(route);
    expect(loaded.regions.main.blocks[0].type).toBe("heading");
  });

  it("save() accepts a multi-segment route", async () => {
    const client = await adminClient();
    const route = `blog/index-${Date.now()}`;

    await client.layouts.save(route, headingLayout("Blog"));
    const loaded = await client.layouts.get(route);

    expect(loaded.regions.main.blocks[0].props?.text).toBe("Blog");
  });

  it("save() with an unregistered block type throws a typed 422 error", async () => {
    const client = await adminClient();
    const route = `broken-${Date.now()}`;

    let caught: unknown;
    try {
      await client.layouts.save(route, {
        contract_version: "layout-composition/v1",
        regions: { main: { blocks: [{ type: "does-not-exist" }] } },
      });
    } catch (err) {
      caught = err;
    }

    expect(caught).toBeInstanceOf(GlyphuxApiError);
    expect((caught as GlyphuxApiError).status).toBe(422);
    expect((caught as GlyphuxApiError).issues?.length).toBeGreaterThan(0);
  });

  it("save() without authentication throws a typed 401 error", async () => {
    const env = testEnv();
    const client = new GlyphuxClient({ baseUrl: env.baseUrl });

    let caught: unknown;
    try {
      await client.layouts.save(`anon-${Date.now()}`, headingLayout("x"));
    } catch (err) {
      caught = err;
    }

    expect(caught).toBeInstanceOf(GlyphuxApiError);
    expect((caught as GlyphuxApiError).status).toBe(401);
  });

  it("get() requires no authentication", async () => {
    const admin = await adminClient();
    const route = `public-read-${Date.now()}`;
    await admin.layouts.save(route, headingLayout("Public"));

    const anon = new GlyphuxClient({ baseUrl: testEnv().baseUrl });
    const loaded = await anon.layouts.get(route);
    expect(loaded.regions.main.blocks[0].props?.text).toBe("Public");
  });

  describe("preview()", () => {
    it("renders an unsaved draft through the real starter theme", async () => {
      const client = await adminClient();
      const preview = await client.layouts.preview(headingLayout("Draft heading"));
      expect(preview.html).toContain("<h1>Draft heading</h1>");
      expect(preview.contentType).toBe("text/html; charset=utf-8");
    });

    it("does not persist the previewed draft", async () => {
      const client = await adminClient();
      const route = `never-saved-${Date.now()}`;

      let caught: unknown;
      try {
        await client.layouts.get(route);
      } catch (err) {
        caught = err;
      }
      expect(caught).toBeInstanceOf(GlyphuxApiError);
      expect((caught as GlyphuxApiError).status).toBe(404);

      await client.layouts.preview(headingLayout("Not persisted"));

      let caughtAfter: unknown;
      try {
        await client.layouts.get(route);
      } catch (err) {
        caughtAfter = err;
      }
      expect(caughtAfter).toBeInstanceOf(GlyphuxApiError);
      expect((caughtAfter as GlyphuxApiError).status).toBe(404);
    });

    it("with an unregistered block type throws a typed 422 error", async () => {
      const client = await adminClient();

      let caught: unknown;
      try {
        await client.layouts.preview({
          contract_version: "layout-composition/v1",
          regions: { main: { blocks: [{ type: "does-not-exist" }] } },
        });
      } catch (err) {
        caught = err;
      }

      expect(caught).toBeInstanceOf(GlyphuxApiError);
      expect((caught as GlyphuxApiError).status).toBe(422);
      expect((caught as GlyphuxApiError).issues?.length).toBeGreaterThan(0);
    });

    it("without authentication throws a typed 401 error", async () => {
      const env = testEnv();
      const client = new GlyphuxClient({ baseUrl: env.baseUrl });

      let caught: unknown;
      try {
        await client.layouts.preview(headingLayout("x"));
      } catch (err) {
        caught = err;
      }

      expect(caught).toBeInstanceOf(GlyphuxApiError);
      expect((caught as GlyphuxApiError).status).toBe(401);
    });
  });
});
