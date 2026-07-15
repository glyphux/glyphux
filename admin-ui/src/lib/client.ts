import { GlyphuxClient } from "@glyphux/sdk";

/** localStorage key the bearer token is persisted under. This is an admin
 * operator's own browser (PRD brief: "this is an admin operator's own
 * browser, not a public surface"), so localStorage is an acceptable place
 * to keep it across reloads. */
const TOKEN_KEY = "glyphux.admin.token";

/** The admin shell is always served by the same glyphuxd instance it
 * manages (embedded via go:embed at /admin/*), so the API lives at this
 * same origin — never a cross-origin call, matching server.go's "no CORS,
 * ever" posture (slice 1.9). */
const client = new GlyphuxClient({
  baseUrl: window.location.origin,
  token: loadToken() ?? undefined,
});

function loadToken(): string | null {
  try {
    return window.localStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

/** Persists (or clears, when `token` is undefined) the bearer token and
 * keeps the shared client's in-memory token in sync with it. Every call
 * site that changes auth state (login, logout) goes through this instead
 * of touching `client.token` or localStorage directly, so the two never
 * drift apart. */
export function setToken(token: string | undefined): void {
  client.token = token;
  try {
    if (token) {
      window.localStorage.setItem(TOKEN_KEY, token);
    } else {
      window.localStorage.removeItem(TOKEN_KEY);
    }
  } catch {
    // localStorage unavailable (private browsing, etc.) — the in-memory
    // token still works for the current tab's lifetime.
  }
}

/** The single shared GlyphuxClient instance every admin page talks to the
 * server through. Admin code must never call `fetch` against `/api/v0/*`
 * directly — this client (and only this client) is the transport. */
export { client };
