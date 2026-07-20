import type { HttpClient } from "./http.js";
import type { ContentItem, ContentVersion } from "./types.js";

/** Wraps /api/v0/content/{type}[/...] (internal/api/api.go content routes). */
export class ContentResource {
  constructor(private readonly http: HttpClient) {}

  /** Creates a new item of the given content type as a draft at version 1.
   * Requires the content:write capability. */
  async create(type: string, data: Record<string, unknown>): Promise<ContentItem> {
    return this.http.requestJSON<ContentItem>("POST", `/api/v0/content/${encodeURIComponent(type)}`, {
      json: data,
    });
  }

  /** Returns a single item by type and id. `locale`, if given, resolves
   * localized field values for that locale. Throws GlyphuxApiError(404) if
   * the type or item is unknown. */
  async get(type: string, id: string, locale?: string): Promise<ContentItem> {
    return this.http.requestJSON<ContentItem>(
      "GET",
      `/api/v0/content/${encodeURIComponent(type)}/${encodeURIComponent(id)}`,
      { query: { locale } },
    );
  }

  /** Lists every item of the given type. `locale`, if given, resolves
   * localized field values for that locale. */
  async list(type: string, locale?: string): Promise<ContentItem[]> {
    const { items } = await this.http.requestJSON<{ items: ContentItem[] }>(
      "GET",
      `/api/v0/content/${encodeURIComponent(type)}`,
      { query: { locale } },
    );
    return items;
  }

  /** Replaces an item's data, validating against its content type and
   * incrementing its version. Requires content:write. */
  async update(type: string, id: string, data: Record<string, unknown>): Promise<ContentItem> {
    return this.http.requestJSON<ContentItem>(
      "PUT",
      `/api/v0/content/${encodeURIComponent(type)}/${encodeURIComponent(id)}`,
      { json: data },
    );
  }

  /** Deletes an item permanently. Requires content:write. */
  async delete(type: string, id: string): Promise<void> {
    await this.http.requestVoid("DELETE", `/api/v0/content/${encodeURIComponent(type)}/${encodeURIComponent(id)}`);
  }

  /** Marks a draft item published. Requires content:publish. */
  async publish(type: string, id: string): Promise<ContentItem> {
    return this.http.requestJSON<ContentItem>(
      "POST",
      `/api/v0/content/${encodeURIComponent(type)}/${encodeURIComponent(id)}/publish`,
    );
  }

  /** Reverts a published item to draft. Requires content:publish. */
  async unpublish(type: string, id: string): Promise<ContentItem> {
    return this.http.requestJSON<ContentItem>(
      "POST",
      `/api/v0/content/${encodeURIComponent(type)}/${encodeURIComponent(id)}/unpublish`,
    );
  }

  /** Returns an item's full version history, oldest first. */
  async listVersions(type: string, id: string): Promise<ContentVersion[]> {
    const { versions } = await this.http.requestJSON<{ versions: ContentVersion[] }>(
      "GET",
      `/api/v0/content/${encodeURIComponent(type)}/${encodeURIComponent(id)}/versions`,
    );
    return versions;
  }

  /** Restores an item's data from a prior version, recorded as a new version
   * (it does not rewrite history). Requires content:write. */
  async rollback(type: string, id: string, version: number): Promise<ContentItem> {
    return this.http.requestJSON<ContentItem>(
      "POST",
      `/api/v0/content/${encodeURIComponent(type)}/${encodeURIComponent(id)}/rollback/${version}`,
    );
  }
}
