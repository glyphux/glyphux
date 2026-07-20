import type { HttpClient } from "./http.js";
import type { Layout } from "./types.js";

/** The result of LayoutsResource.preview(): a rendered HTML document plus
 * the MIME type the theme reported for it (always "text/html; charset=utf-8"
 * for themes/starter today, but carried through rather than assumed — see
 * pkg/theme.Theme.Render's own doc comment on why contentType travels with
 * the bytes). */
export interface LayoutPreview {
  html: string;
  contentType: string;
}

/** Wraps /api/v0/layouts/{route} (internal/api/layouts.go) — the Layer-2
 * Layout document (blocks arranged into a template's named regions) saved
 * for one route, e.g. "home" or "blog/index". Requires the layouts:manage
 * capability (admin only) for save() and preview(). */
export class LayoutsResource {
  constructor(private readonly http: HttpClient) {}

  /** Returns the Layout saved for route. Throws GlyphuxApiError(404) if none
   * has been saved yet. */
  async get(route: string): Promise<Layout> {
    return this.http.requestJSON<Layout>("GET", `/api/v0/layouts/${encodePathSegments(route)}`);
  }

  /** Validates (structurally, and every block's type against the running
   * daemon's real block registry) then persists layout for route, replacing
   * whatever was previously saved for it. Throws GlyphuxApiError(422) with
   * `.issues` on a validation failure (e.g. an unregistered block type). */
  async save(route: string, layout: Layout): Promise<Layout> {
    return this.http.requestJSON<Layout>("PUT", `/api/v0/layouts/${encodePathSegments(route)}`, { json: layout });
  }

  /** Renders layout — which may be an UNSAVED draft, never passed to save()
   * at all — through the real themes/starter theme and returns the
   * resulting HTML (Ticket P4.5: "editor renders the same composition the
   * API serves"). Performs no persistence; this is a pure read of what
   * layout would render as. Validated exactly like save() before rendering,
   * so throws the identical GlyphuxApiError(422) with `.issues` on a
   * structural or unregistered-block-type failure. */
  async preview(layout: Layout): Promise<LayoutPreview> {
    const res = await this.http.requestJSON<{ html: string; content_type: string }>(
      "POST",
      "/api/v0/layouts/preview",
      { json: layout },
    );
    return { html: res.html, contentType: res.content_type };
  }
}

/** Encodes each "/"-delimited segment of route independently, so a
 * multi-segment route like "blog/index" produces the path
 * ".../layouts/blog/index" (matching Go's {route...} wildcard route
 * pattern) rather than encoding the slash itself into "%2F", which would
 * make the whole route one opaque segment the server-side route matcher
 * wouldn't split. */
function encodePathSegments(route: string): string {
  return route.split("/").map(encodeURIComponent).join("/");
}
