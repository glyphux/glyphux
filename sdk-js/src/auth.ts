import type { HttpClient } from "./http.js";
import type { LoginResult, User } from "./types.js";

/** Wraps POST/GET /api/v0/auth/* (internal/api/auth.go). */
export class AuthResource {
  constructor(private readonly http: HttpClient) {}

  /** Authenticates with email/password. On success the client's internal
   * bearer token is updated automatically, so subsequent calls on the same
   * client are authenticated without further setup. */
  async login(email: string, password: string): Promise<LoginResult> {
    const result = await this.http.requestJSON<LoginResult>("POST", "/api/v0/auth/login", {
      json: { email, password },
    });
    this.http.token = result.token;
    return result;
  }

  /** Revokes the current session (both the bearer token and, if present, the
   * session cookie). Idempotent, per the server's handling of an unknown
   * token. */
  async logout(): Promise<void> {
    await this.http.requestVoid("POST", "/api/v0/auth/logout");
    this.http.token = undefined;
  }

  /** Returns the currently authenticated user, or throws GlyphuxApiError(401)
   * if unauthenticated. */
  async me(): Promise<User> {
    return this.http.requestJSON<User>("GET", "/api/v0/auth/me");
  }
}
