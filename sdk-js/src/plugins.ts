import type { HttpClient } from "./http.js";

/** One (capability, scopes) pair of a plugin's requested or granted API
 * surface (internal/api/consent.go apiScopeWire). */
export interface ConsentScope {
  capability: string;
  scopes: string[];
}

/** One named permission of a plugin's requested or granted permission
 * surface (internal/api/consent.go permissionWire). */
export interface ConsentPermission {
  name: string;
  args?: string[];
}

/** A requested or granted subset: API scopes plus permissions. */
export interface ConsentGrant {
  api: ConsentScope[];
  permissions: ConsentPermission[];
}

/** Live consent state of one registered plugin, as served by
 * GET /api/v0/plugins. `status` is the most recent decision's status, or
 * "undecided" when no decision has ever been recorded against the plugin's
 * exact manifest shape — the consent screen must show "denied" (a decision
 * exists, grants nothing) distinctly from "undecided" (never asked).
 * `granted` is present only for a live (approved/partial) decision and is
 * then exactly the subset the admin approved. */
export interface PluginConsentStatus {
  name: string;
  version: string;
  fingerprint: string;
  status: "approved" | "partial" | "denied" | "undecided";
  requested: ConsentGrant;
  granted?: ConsentGrant;
}

/** One pending consent request (undecided, denied, or stale-fingerprint
 * — i.e. every registered plugin WITHOUT a live decision), as served by
 * GET /api/v0/plugins/consent-requests. */
export interface ConsentRequestInfo {
  name: string;
  version: string;
  fingerprint: string;
  api: ConsentScope[];
  permissions: ConsentPermission[];
}

/** The decision outcome accepted by the decide endpoint. */
export type ConsentDecisionKind = "approved" | "partial" | "denied";

/** The admin's decision body for POST .../consent-requests/{plugin}/decide.
 * For "approved", the grant may be omitted (it means the full request); for
 * "denied" it must be omitted; for "partial" it must name the exact subset
 * granted. */
export interface ConsentDecisionInput {
  decision: ConsentDecisionKind;
  granted_api?: ConsentScope[];
  granted_permissions?: ConsentPermission[];
}

/** One recorded consent decision, as returned by the decide endpoint
 * (internal/api/consent.go decisionWire). */
export interface ConsentDecision {
  id: number;
  plugin_name: string;
  plugin_version: string;
  fingerprint: string;
  status: ConsentDecisionKind;
  granted_api: ConsentScope[];
  granted_permissions: ConsentPermission[];
  decided_by: number;
  decided_at: string;
}

/** Wraps /api/v0/plugins (internal/api/consent.go) — the install-time
 * consent surface (PRD §10.2 mechanism #2, gap 2 / Ticket T4). Admin-only:
 * every method requires the plugins:manage capability, so a caller without
 * it gets a typed 403 GlyphuxApiError. */
export class PluginsResource {
  constructor(private readonly http: HttpClient) {}

  /** Lists every registered plugin with its consent status: the full
   * requested permission surface plus, when a live decision exists, exactly
   * the granted subset. */
  async list(): Promise<PluginConsentStatus[]> {
    const { plugins } = await this.http.requestJSON<{ plugins: PluginConsentStatus[] }>("GET", "/api/v0/plugins");
    return plugins;
  }

  /** Lists every plugin that currently needs a consent decision: undecided,
   * explicitly denied, or with a stale fingerprint (its manifest's
   * API/permissions surface changed since the last decision). */
  async consentRequests(): Promise<ConsentRequestInfo[]> {
    const { requests } = await this.http.requestJSON<{ requests: ConsentRequestInfo[] }>(
      "GET",
      "/api/v0/plugins/consent-requests",
    );
    return requests;
  }

  /** Records the admin's decision for plugin. Throws GlyphuxApiError(422)
   * when the grant exceeds the plugin's request (a capability/scope the
   * manifest never declared), and GlyphuxApiError(404) for an unknown
   * plugin. */
  async decide(plugin: string, input: ConsentDecisionInput): Promise<ConsentDecision> {
    return this.http.requestJSON<ConsentDecision>("POST", `/api/v0/plugins/consent-requests/${encodeURIComponent(plugin)}/decide`, {
      json: input,
    });
  }
}
