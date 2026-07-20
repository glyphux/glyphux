import type { HttpClient } from "./http.js";
import type { CompatResult, CompositionPreset, PresetRecord } from "./types.js";

/** Wraps /api/v0/presets (internal/api/presets.go) — Composition Presets
 * (PRD §13.2/§13.3, Ticket P4.6): saved, importable fragments of Layer-2
 * composition. save()/import() require the presets:manage capability
 * (admin only); list()/get()/check() are public reads. */
export class PresetsResource {
  constructor(private readonly http: HttpClient) {}

  /** Returns every saved preset. */
  async list(): Promise<PresetRecord[]> {
    const { presets } = await this.http.requestJSON<{ presets: PresetRecord[] }>("GET", "/api/v0/presets");
    return presets;
  }

  /** Returns the preset saved under id. Throws GlyphuxApiError(404) if none
   * exists. */
  async get(id: string): Promise<PresetRecord> {
    return this.http.requestJSON<PresetRecord>("GET", `/api/v0/presets/${encodeURIComponent(id)}`);
  }

  /** Validates (structurally, and every declared/used block type against
   * the running daemon's real block registry) then persists preset under a
   * new id. Throws GlyphuxApiError(422) with `.issues` on a validation or
   * compatibility failure — a preset saved this way is authored locally, so
   * every block it uses must already be registered. */
  async save(preset: CompositionPreset): Promise<PresetRecord> {
    return this.http.requestJSON<PresetRecord>("POST", "/api/v0/presets", { json: preset });
  }

  /** Runs the compatibility contract for the preset saved under id against
   * the running block registry and, optionally, a destination theme's
   * declared regions (themeRegions — pass the theme's Theme.Regions(), or
   * omit for "no declared restriction," e.g. a JSON-only headless theme).
   * Never mutates anything — the read-only "would importing this work?"
   * query a UI calls before committing to import(). */
  async check(id: string, themeRegions?: string[]): Promise<CompatResult> {
    return this.http.requestJSON<CompatResult>("GET", `/api/v0/presets/${encodeURIComponent(id)}/check`, {
      query: themeRegions?.length ? { theme_regions: themeRegions.join(",") } : undefined,
    });
  }

  /** Checks the preset saved under id against the running registry and
   * themeRegions, and — only if compatible — merges its Layout fragment
   * into route's saved Layout. Always resolves with the CompatResult, even
   * when incompatible (the import is simply declined, not an error) — read
   * `.compatible` to tell "merged" from "declined, here's why" (PRD §13.3's
   * explainable-degradation requirement); `merge into Layout` never throws
   * just because a block/slot dependency was missing. */
  async import(id: string, route: string, themeRegions?: string[]): Promise<CompatResult> {
    return this.http.requestJSON<CompatResult>("POST", `/api/v0/presets/${encodeURIComponent(id)}/import`, {
      json: { route, theme_regions: themeRegions },
    });
  }
}
