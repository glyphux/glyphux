import type { HttpClient } from "./http.js";
import type { ContentType } from "./types.js";

/** Wraps /api/v0/content-types (internal/api/contenttypes.go) — structural
 * schema management, distinct from ContentResource's per-item CRUD. Requires
 * the content_types:manage capability (admin only) for define()/delete(). */
export class ContentTypesResource {
  constructor(private readonly http: HttpClient) {}

  /** Returns every currently declared content type. */
  async list(): Promise<Record<string, ContentType>> {
    const { content_types } = await this.http.requestJSON<{ content_types: Record<string, ContentType> }>(
      "GET",
      "/api/v0/content-types",
    );
    return content_types;
  }

  /** Creates a new content type or replaces an existing one's field set. */
  async define(name: string, contentType: ContentType): Promise<ContentType> {
    return this.http.requestJSON<ContentType>("PUT", `/api/v0/content-types/${encodeURIComponent(name)}`, {
      json: contentType,
    });
  }

  /** Deletes a content type. Throws GlyphuxApiError(409) if items of that
   * type still exist, or GlyphuxApiError(404) if the type is unknown. */
  async delete(name: string): Promise<void> {
    await this.http.requestVoid("DELETE", `/api/v0/content-types/${encodeURIComponent(name)}`);
  }
}
