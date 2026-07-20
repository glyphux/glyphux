import type { HttpClient } from "./http.js";
import type { AIComposeResult } from "./types.js";

/** One POST /api/v0/ai/compose request: what the user typed, and which
 * route/theme context the proposed fragment is destined for. `model` names
 * whichever model string the daemon's configured provider Adapter expects
 * (e.g. "claude-3-5-sonnet-20241022") — this SDK passes it through opaquely,
 * exactly like capabilities/ai.GenerateRequest.Model does server-side. */
export interface AIComposeRequest {
  prompt: string;
  route: string;
  themeRegions?: string[];
  model: string;
}

/** Wraps /api/v0/ai/compose (internal/api/ai.go, Ticket P4.8, PRD §14.1
 * Surface 2): "describe what you want, get a composition fragment back."
 * Requires layouts:manage AND presets:manage (admin only) plus the daemon's
 * `ai` capability actually being configured (WithAI) — 404s otherwise.
 * Never persists anything itself: accepting a returned fragment is the
 * caller's job, via the ordinary PresetsResource.save() then
 * PresetsResource.import() — the same path an ordinary preset import
 * already uses, never a second insert path. */
export class AiResource {
  constructor(private readonly http: HttpClient) {}

  /** Calls the configured AI provider with req.prompt, engineered
   * server-side to return a Layer-2 composition fragment, and validates the
   * result through the exact same path an ordinary preset save/import
   * already uses. Resolves (never throws) with `.compatible: false` when
   * the model's output is structurally well-formed JSON but references a
   * block/slot this daemon doesn't have — mirroring
   * PresetsResource.import()'s own "declined, not an error" convention.
   * Throws GlyphuxApiError(422) with `.issues` if the model's response
   * isn't valid JSON or fails CompositionPreset's own structural checks
   * (the same shape PresetsResource.save() throws for a malformed preset),
   * GlyphuxApiError(400) for a missing prompt/model, and the usual
   * auth/permission/5xx shapes otherwise. */
  async compose(req: AIComposeRequest): Promise<AIComposeResult> {
    return this.http.requestJSON<AIComposeResult>("POST", "/api/v0/ai/compose", {
      json: {
        prompt: req.prompt,
        route: req.route,
        theme_regions: req.themeRegions,
        model: req.model,
      },
    });
  }
}
