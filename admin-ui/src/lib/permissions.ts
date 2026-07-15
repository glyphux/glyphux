/** Mirrors internal/permission/permission.go's fixed v1 role→capability
 * matrix, for UI-side affordance only (hide/disable actions a role can't
 * perform). This is a UX nicety, never the security boundary — the server
 * enforces every capability authoritatively and a determined user can
 * always hit the API directly; the SDK surfaces that as a typed 403
 * GlyphuxApiError regardless of what this file says. */
export type Capability =
  | "content:read"
  | "content:read_drafts"
  | "content:write"
  | "content:publish"
  | "media:write"
  | "users:manage"
  | "content_types:manage";

const ROLE_CAPABILITIES: Record<string, Set<Capability>> = {
  admin: new Set([
    "content:read",
    "content:read_drafts",
    "content:write",
    "content:publish",
    "media:write",
    "users:manage",
    "content_types:manage",
  ]),
  editor: new Set(["content:read", "content:read_drafts", "content:write", "media:write"]),
  viewer: new Set(["content:read"]),
};

/** Reports whether `role` holds `capability` under v1's fixed matrix.
 * Unknown roles hold nothing. */
export function allows(role: string | undefined, capability: Capability): boolean {
  if (!role) return false;
  return ROLE_CAPABILITIES[role]?.has(capability) ?? false;
}
