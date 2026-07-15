import type { HttpClient } from "./http.js";
import type { User } from "./types.js";

/** Wraps /api/v0/users (internal/api/users.go). Admin-only: every method
 * requires the users:manage capability, so a caller without it gets a typed
 * 403 GlyphuxApiError. */
export class UsersResource {
  constructor(private readonly http: HttpClient) {}

  /** Creates an account with one of v1's fixed roles: "admin", "editor", or
   * "viewer" (internal/permission). */
  async create(email: string, password: string, role: string): Promise<User> {
    return this.http.requestJSON<User>("POST", "/api/v0/users", { json: { email, password, role } });
  }

  /** Lists every account. */
  async list(): Promise<User[]> {
    const { users } = await this.http.requestJSON<{ users: User[] }>("GET", "/api/v0/users");
    return users;
  }
}
