import type { HttpClient } from "./http.js";
import type { MediaItem, MediaMetadataUpdate, MediaTransform } from "./types.js";

/** Wraps /api/v0/media[/...] (internal/api/media.go). */
export class MediaResource {
  constructor(private readonly http: HttpClient) {}

  /** Uploads a file's bytes as a new media asset. `filename` is sent as the
   * multipart part's filename; the server sniffs the real MIME type from
   * content rather than trusting any content-type on the part. Requires
   * media:write. Only image/png, image/jpeg, and image/gif are accepted
   * (internal/media/media.go allowedTypes). */
  async upload(bytes: Uint8Array | Blob, filename: string): Promise<MediaItem> {
    const form = new FormData();
    // The cast works around TS 5.7's Uint8Array<ArrayBufferLike> vs.
    // Uint8Array<ArrayBuffer> BlobPart mismatch; any typed array is a valid
    // Blob part at runtime regardless of its backing buffer's generic type.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const blob = bytes instanceof Blob ? bytes : new Blob([bytes as any]);
    form.set("file", blob, filename);
    return this.http.requestMultipart<MediaItem>("POST", "/api/v0/media", form);
  }

  /** Returns a single media item's metadata. Throws GlyphuxApiError(404) if
   * unknown. */
  async get(id: string): Promise<MediaItem> {
    return this.http.requestJSON<MediaItem>("GET", `/api/v0/media/${encodeURIComponent(id)}`);
  }

  /** Lists every stored media item, oldest first. */
  async list(): Promise<MediaItem[]> {
    const { items } = await this.http.requestJSON<{ items: MediaItem[] }>("GET", "/api/v0/media");
    return items;
  }

  /** Deletes a media item and its stored file. Requires media:write. */
  async delete(id: string): Promise<void> {
    await this.http.requestVoid("DELETE", `/api/v0/media/${encodeURIComponent(id)}`);
  }

  /** Replaces a media item's editable metadata (alt text, tags,
   * source/attribution) and returns the updated item. Requires
   * media:write. */
  async updateMetadata(id: string, update: MediaMetadataUpdate): Promise<MediaItem> {
    return this.http.requestJSON<MediaItem>("PATCH", `/api/v0/media/${encodeURIComponent(id)}`, { json: update });
  }

  /** Returns the raw file bytes as a Response — the caller decides how to
   * consume it (`.blob()`, `.arrayBuffer()`, stream the body, etc.) and can
   * inspect `Content-Type`. Pass any combination of `width`/`height` (a
   * resize), `cropX`/`cropY`/`cropW`/`cropH` (a crop), `rotate` (90/180/270),
   * and `format` ("jpeg"/"png") to run the transform pipeline — crop, then
   * rotate, then resize, then re-encode — in one request
   * (internal/api/media.go handleMediaFile). Does not throw
   * GlyphuxApiError; check `response.ok` / `response.status` directly,
   * since a non-2xx body here is not guaranteed to be JSON. */
  async file(id: string, options?: MediaTransform): Promise<Response> {
    return this.http.requestRaw("GET", `/api/v0/media/${encodeURIComponent(id)}/file`, {
      w: options?.width !== undefined ? String(options.width) : undefined,
      h: options?.height !== undefined ? String(options.height) : undefined,
      crop_x: options?.cropX !== undefined ? String(options.cropX) : undefined,
      crop_y: options?.cropY !== undefined ? String(options.cropY) : undefined,
      crop_w: options?.cropW !== undefined ? String(options.cropW) : undefined,
      crop_h: options?.cropH !== undefined ? String(options.cropH) : undefined,
      rotate: options?.rotate !== undefined ? String(options.rotate) : undefined,
      format: options?.format,
    });
  }
}
