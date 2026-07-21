import type { HttpClient } from "./http.js";
import type { BundleImportResult, BundleRecord, CompatResult, CompositionBundle } from "./types.js";

/** Wraps /api/v0/bundles (internal/api/bundles.go) — Composition Bundles
 * (PRD §13.2/§13.3, Ticket P4.6): a preset collection at site scale (pages +
 * presets + sample content + a theme reference), "import this starter site"
 * made real. save()/import() require the presets:manage capability (admin
 * only); list()/get()/check() are public reads. */
export class BundlesResource {
  constructor(private readonly http: HttpClient) {}

  /** Returns every saved bundle. */
  async list(): Promise<BundleRecord[]> {
    const { bundles } = await this.http.requestJSON<{ bundles: BundleRecord[] }>("GET", "/api/v0/bundles");
    return bundles;
  }

  /** Returns the bundle saved under id. Throws GlyphuxApiError(404) if none
   * exists. */
  async get(id: string): Promise<BundleRecord> {
    return this.http.requestJSON<BundleRecord>("GET", `/api/v0/bundles/${encodeURIComponent(id)}`);
  }

  /** Validates then persists bundle under a new id, mirroring
   * PresetsResource.save's identical "authored locally, so every declared/
   * used block must already be registered" requirement. */
  async save(bundle: CompositionBundle): Promise<BundleRecord> {
    return this.http.requestJSON<BundleRecord>("POST", "/api/v0/bundles", { json: bundle });
  }

  /** Runs the compatibility contract for the bundle saved under id — see
   * PresetsResource.check's identical doc comment. */
  async check(id: string, themeRegions?: string[]): Promise<CompatResult> {
    return this.http.requestJSON<CompatResult>("GET", `/api/v0/bundles/${encodeURIComponent(id)}/check`, {
      query: themeRegions?.length ? { theme_regions: themeRegions.join(",") } : undefined,
    });
  }

  /** Checks the bundle saved under id against the running registry and
   * themeRegions, and — only if compatible — merges every page's Layout and
   * creates every sample content item. Always resolves with a
   * BundleImportResult, even when incompatible (`.compat.compatible ===
   * false`, `.imported_pages`/`.created_content` empty) — read `.compat`
   * first, the same "explainable, not a bare rejection" contract
   * PresetsResource.import follows. */
  async import(id: string, themeRegions?: string[]): Promise<BundleImportResult> {
    return this.http.requestJSON<BundleImportResult>("POST", `/api/v0/bundles/${encodeURIComponent(id)}/import`, {
      json: { theme_regions: themeRegions },
    });
  }
}
