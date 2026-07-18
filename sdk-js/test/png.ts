// A minimal, dependency-free PNG encoder/decoder for tests only — just
// enough to build an asymmetric test image (so crop/rotate correctness can
// be checked against real output pixels/dimensions, not just HTTP 200) and
// read a transformed result back. Real projects would pull in a PNG
// library; this repo's sdk-js has none, and pulling one in solely for test
// fixtures isn't worth the dependency.
import { deflateSync, inflateSync } from "node:zlib";

const CRC_TABLE = (() => {
  const table = new Uint32Array(256);
  for (let n = 0; n < 256; n++) {
    let c = n;
    for (let k = 0; k < 8; k++) {
      c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    }
    table[n] = c >>> 0;
  }
  return table;
})();

function crc32(buf: Uint8Array): number {
  let c = 0xffffffff;
  for (const byte of buf) {
    c = CRC_TABLE[(c ^ byte) & 0xff] ^ (c >>> 8);
  }
  return (c ^ 0xffffffff) >>> 0;
}

function chunk(type: string, data: Uint8Array): Buffer {
  const typeBytes = Buffer.from(type, "ascii");
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length, 0);
  const body = Buffer.concat([typeBytes, data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(body), 0);
  return Buffer.concat([len, body, crc]);
}

export interface RGBA {
  r: number;
  g: number;
  b: number;
  a: number;
}

/** Encodes a solid-per-pixel RGBA image as an uncompressed (zlib level 0)
 * PNG — `pixelAt` supplies each pixel's color so callers can build
 * asymmetric fixtures (e.g. left half red, right half blue). */
export function encodePNG(width: number, height: number, pixelAt: (x: number, y: number) => RGBA): Buffer {
  const signature = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);

  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(width, 0);
  ihdr.writeUInt32BE(height, 4);
  ihdr[8] = 8; // bit depth
  ihdr[9] = 6; // color type: RGBA
  ihdr[10] = 0;
  ihdr[11] = 0;
  ihdr[12] = 0;

  const raw = Buffer.alloc(height * (1 + width * 4));
  let o = 0;
  for (let y = 0; y < height; y++) {
    raw[o++] = 0; // no filter
    for (let x = 0; x < width; x++) {
      const { r, g, b, a } = pixelAt(x, y);
      raw[o++] = r;
      raw[o++] = g;
      raw[o++] = b;
      raw[o++] = a;
    }
  }
  const idat = deflateSync(raw);

  return Buffer.concat([signature, chunk("IHDR", ihdr), chunk("IDAT", idat), chunk("IEND", new Uint8Array(0))]);
}

export interface DecodedPNG {
  width: number;
  height: number;
  at(x: number, y: number): RGBA;
}

// paethPredictor is the PNG spec's Paeth filter predictor (§9.2).
function paethPredictor(a: number, b: number, c: number): number {
  const p = a + b - c;
  const pa = Math.abs(p - a);
  const pb = Math.abs(p - b);
  const pc = Math.abs(p - c);
  if (pa <= pb && pa <= pc) return a;
  if (pb <= pc) return b;
  return c;
}

// unfilterScanlines reverses PNG's per-scanline filtering (None/Sub/Up/
// Average/Paeth, spec §9.2-9.3). Go's image/png encoder picks whichever
// filter is cheapest per row (not always "None"), so a decoder that only
// strips the filter byte without reversing it silently corrupts most rows —
// this is required, not optional, for anything beyond a single-row fixture.
function unfilterScanlines(raw: Buffer, width: number, height: number, bpp: number): Buffer {
  const stride = width * bpp;
  const out = Buffer.alloc(height * stride);
  let rawOffset = 0;
  for (let y = 0; y < height; y++) {
    const filterType = raw[rawOffset];
    rawOffset += 1;
    const rowStart = y * stride;
    const prevRowStart = rowStart - stride;
    for (let x = 0; x < stride; x++) {
      const rawByte = raw[rawOffset + x];
      const a = x >= bpp ? out[rowStart + x - bpp] : 0;
      const b = y > 0 ? out[prevRowStart + x] : 0;
      const c = y > 0 && x >= bpp ? out[prevRowStart + x - bpp] : 0;
      let value: number;
      switch (filterType) {
        case 0:
          value = rawByte;
          break;
        case 1:
          value = rawByte + a;
          break;
        case 2:
          value = rawByte + b;
          break;
        case 3:
          value = rawByte + Math.floor((a + b) / 2);
          break;
        case 4:
          value = rawByte + paethPredictor(a, b, c);
          break;
        default:
          throw new Error(`unsupported PNG filter type ${filterType}`);
      }
      out[rowStart + x] = value & 0xff;
    }
    rawOffset += stride;
  }
  return out;
}

/** Decodes an 8-bit-depth, non-interlaced, non-palette PNG (RGB, RGBA, or
 * grayscale) — good enough for reading back internal/media's transform
 * output in tests, not a general-purpose PNG decoder. Handles color type 2
 * (RGB, no alpha channel) as well as 6 (RGBA): Go's image/png encoder drops
 * the alpha channel and encodes plain RGB whenever every pixel in the
 * source image is fully opaque, which every transform in this test suite's
 * fixtures is. Reverses per-scanline filtering (see unfilterScanlines). */
export function decodePNG(buf: Buffer): DecodedPNG {
  let offset = 8; // skip signature
  let width = 0;
  let height = 0;
  let colorType = 6;
  const idatChunks: Buffer[] = [];
  while (offset < buf.length) {
    const len = buf.readUInt32BE(offset);
    const type = buf.toString("ascii", offset + 4, offset + 8);
    const data = buf.subarray(offset + 8, offset + 8 + len);
    if (type === "IHDR") {
      width = data.readUInt32BE(0);
      height = data.readUInt32BE(4);
      colorType = data[9];
    } else if (type === "IDAT") {
      idatChunks.push(Buffer.from(data));
    }
    offset += 8 + len + 4; // length + type + data + crc
  }
  const raw = inflateSync(Buffer.concat(idatChunks));
  // Channels per color type: 0 = grayscale (1), 2 = RGB (3), 4 = gray+alpha
  // (2), 6 = RGBA (4). Palette (3) isn't produced by encodePNG or by
  // internal/media's encoder for these fixtures, so it's not handled here.
  const channels: Record<number, number> = { 0: 1, 2: 3, 4: 2, 6: 4 };
  const bpp = channels[colorType] ?? 4;
  const pixels = unfilterScanlines(raw, width, height, bpp);
  const stride = width * bpp;
  return {
    width,
    height,
    at(x: number, y: number): RGBA {
      const i = y * stride + x * bpp;
      switch (colorType) {
        case 0:
          return { r: pixels[i], g: pixels[i], b: pixels[i], a: 255 };
        case 2:
          return { r: pixels[i], g: pixels[i + 1], b: pixels[i + 2], a: 255 };
        case 4:
          return { r: pixels[i], g: pixels[i], b: pixels[i], a: pixels[i + 1] };
        default:
          return { r: pixels[i], g: pixels[i + 1], b: pixels[i + 2], a: pixels[i + 3] };
      }
    },
  };
}
