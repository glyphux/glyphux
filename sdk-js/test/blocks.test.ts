import { describe, expect, it } from "vitest";
import { GlyphuxClient } from "../src/client.js";
import { testEnv } from "./testenv.js";

describe("blocks", () => {
  it("list() includes the first-party block types", async () => {
    const env = testEnv();
    const client = new GlyphuxClient({ baseUrl: env.baseUrl });

    const blocks = await client.blocks.list();

    const names = blocks.map((b) => b.name);
    expect(names).toEqual(expect.arrayContaining(["heading", "paragraph", "image", "container"]));

    const heading = blocks.find((b) => b.name === "heading");
    expect(heading?.display_name).toBe("Heading");
    expect(heading?.props?.text.type).toBe("string");

    const container = blocks.find((b) => b.name === "container");
    expect(container?.slots).toEqual(["content"]);
  });

  it("list() requires no authentication", async () => {
    const env = testEnv();
    // Deliberately no login — blocks listing is a public read.
    const client = new GlyphuxClient({ baseUrl: env.baseUrl });
    const blocks = await client.blocks.list();
    expect(blocks.length).toBeGreaterThan(0);
  });
});
