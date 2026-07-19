import type { HttpClient } from "./http.js";
import type { Layout } from "./types.js";

/** Wraps /api/v0/layouts/{route} (internal/api/layouts.go) — the Layer-2
 * Layout document (blocks arranged into a template's named regions) saved
 * for one route, e.g. "home" or "blog/index". Requires the layouts:manage
 * capability (admin only) for save(). */
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
