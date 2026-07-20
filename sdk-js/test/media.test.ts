import { beforeAll, describe, expect, it } from "vitest";
import { GlyphuxClient } from "../src/client.js";
import { GlyphuxApiError } from "../src/errors.js";
import { testEnv } from "./testenv.js";
import { encodePNG, decodePNG } from "./png.js";

// A 4x2 image, left half red / right half blue, so crop/rotate correctness
// can be checked against real output pixels and dimensions, not just a 200
// status.
function quadPng(): Buffer {
  return encodePNG(4, 2, (x) => (x < 2 ? { r: 255, g: 0, b: 0, a: 255 } : { r: 0, g: 0, b: 255, a: 255 }));
}

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

  it("file() with crop params extracts the requested rect", async () => {
    const client = await adminClient();
    const uploaded = await client.media.upload(quadPng(), "quad.png");

    const res = await client.media.file(uploaded.id, { cropX: 2, cropY: 0, cropW: 2, cropH: 2 });

    expect(res.status).toBe(200);
    const img = decodePNG(Buffer.from(await res.arrayBuffer()));
    expect(img.width).toBe(2);
    expect(img.height).toBe(2);
    // Cropped the right (blue) half.
    expect(img.at(0, 0)).toEqual({ r: 0, g: 0, b: 255, a: 255 });
  });

  it("file() with an out-of-bounds crop rect is a typed 400 error", async () => {
    const client = await adminClient();
    const uploaded = await client.media.upload(quadPng(), "quad.png");

    const res = await client.media.file(uploaded.id, { cropX: 0, cropY: 0, cropW: 99, cropH: 99 });

    expect(res.status).toBe(400);
  });

  it("file() with rotate=90 swaps dimensions and moves the left half to the top", async () => {
    const client = await adminClient();
    const uploaded = await client.media.upload(quadPng(), "quad.png");

    const res = await client.media.file(uploaded.id, { rotate: 90 });

    expect(res.status).toBe(200);
    const img = decodePNG(Buffer.from(await res.arrayBuffer()));
    expect(img.width).toBe(2);
    expect(img.height).toBe(4);
    expect(img.at(0, 0)).toEqual({ r: 255, g: 0, b: 0, a: 255 });
    expect(img.at(0, 3)).toEqual({ r: 0, g: 0, b: 255, a: 255 });
  });

  it("file() with an unsupported rotate angle is a typed 400 error", async () => {
    const client = await adminClient();
    const uploaded = await client.media.upload(quadPng(), "quad.png");

    const res = await client.media.file(uploaded.id, { rotate: 45 });

    expect(res.status).toBe(400);
  });

  it("file() with format=jpeg re-encodes as a real JPEG", async () => {
    const client = await adminClient();
    const uploaded = await client.media.upload(quadPng(), "quad.png");

    const res = await client.media.file(uploaded.id, { format: "jpeg" });

    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toBe("image/jpeg");
    const bytes = new Uint8Array(await res.arrayBuffer());
    // JPEG magic bytes (SOI marker).
    expect(bytes[0]).toBe(0xff);
    expect(bytes[1]).toBe(0xd8);
  });

  it("updateMetadata() persists tags, source, and attribution", async () => {
    const client = await adminClient();
    const uploaded = await client.media.upload(onePixelPng(), "tag-me.png");

    const updated = await client.media.updateMetadata(uploaded.id, {
      alt_text: "a pixel",
      tags: ["stock", "hero"],
      source: "https://example.com/photo",
      attribution: "Photo by Jane Doe",
    });

    expect(updated.alt_text).toBe("a pixel");
    expect(updated.tags).toEqual(["stock", "hero"]);
    expect(updated.source).toBe("https://example.com/photo");
    expect(updated.attribution).toBe("Photo by Jane Doe");

    const fetched = await client.media.get(uploaded.id);
    expect(fetched.tags).toEqual(["stock", "hero"]);
    expect(fetched.source).toBe("https://example.com/photo");
  });

  it("upload() defaults tags to an empty array, not null", async () => {
    const client = await adminClient();
    const uploaded = await client.media.upload(onePixelPng(), "untagged.png");

    expect(Array.isArray(uploaded.tags)).toBe(true);
    expect(uploaded.tags).toEqual([]);
  });
});
