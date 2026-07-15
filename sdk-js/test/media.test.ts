import { beforeAll, describe, expect, it } from "vitest";
import { GlyphuxClient } from "../src/client.js";
import { GlyphuxApiError } from "../src/errors.js";
import { testEnv } from "./testenv.js";

// A minimal valid 1x1 transparent PNG, so internal/media.Upload's
// image.DecodeConfig sniff succeeds and reports real width/height.
const ONE_PIXEL_PNG_BASE64 =
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=";

function onePixelPng(): Uint8Array {
  return new Uint8Array(Buffer.from(ONE_PIXEL_PNG_BASE64, "base64"));
}

async function adminClient(): Promise<GlyphuxClient> {
  const env = testEnv();
  const client = new GlyphuxClient({ baseUrl: env.baseUrl });
  await client.auth.login(env.adminEmail, env.adminPassword);
  return client;
}

describe("media", () => {
  it("upload() stores the file and returns its metadata", async () => {
    const client = await adminClient();

    const item = await client.media.upload(onePixelPng(), "pixel.png");

    expect(item.filename).toBe("pixel.png");
    expect(item.mime_type).toBe("image/png");
    expect(item.width).toBe(1);
    expect(item.height).toBe(1);
    expect(item.size_bytes).toBeGreaterThan(0);
    expect(typeof item.id).toBe("string");
  });

  it("get() returns a previously uploaded item by id", async () => {
    const client = await adminClient();
    const uploaded = await client.media.upload(onePixelPng(), "fetch-me.png");

    const fetched = await client.media.get(uploaded.id);

    expect(fetched.id).toBe(uploaded.id);
    expect(fetched.filename).toBe("fetch-me.png");
  });

  it("get() reports a missing item as a typed 404 error", async () => {
    const client = await adminClient();

    let caught: unknown;
    try {
      await client.media.get("no-such-id");
    } catch (err) {
      caught = err;
    }
    expect((caught as InstanceType<typeof GlyphuxApiError>).status).toBe(404);
  });

  it("list() includes an uploaded item", async () => {
    const client = await adminClient();
    const uploaded = await client.media.upload(onePixelPng(), "listed.png");

    const items = await client.media.list();

    expect(items.some((i) => i.id === uploaded.id)).toBe(true);
  });

  it("delete() removes the item so a later get() 404s", async () => {
    const client = await adminClient();
    const uploaded = await client.media.upload(onePixelPng(), "doomed.png");

    await client.media.delete(uploaded.id);

    let caught: unknown;
    try {
      await client.media.get(uploaded.id);
    } catch (err) {
      caught = err;
    }
    expect((caught as InstanceType<typeof GlyphuxApiError>).status).toBe(404);
  });

  it("file() returns the raw stored bytes with the right content type", async () => {
    const client = await adminClient();
    const uploaded = await client.media.upload(onePixelPng(), "raw.png");

    const res = await client.media.file(uploaded.id);

    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toBe("image/png");
    const bytes = new Uint8Array(await res.arrayBuffer());
    expect(bytes).toEqual(onePixelPng());
  });

  it("file() with width/height returns a resized image", async () => {
    const client = await adminClient();
    const uploaded = await client.media.upload(onePixelPng(), "resize-me.png");

    const res = await client.media.file(uploaded.id, { width: 4, height: 4 });

    expect(res.status).toBe(200);
    const bytes = new Uint8Array(await res.arrayBuffer());
    expect(bytes.length).toBeGreaterThan(0);
  });
});
