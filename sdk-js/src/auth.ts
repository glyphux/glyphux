import type { HttpClient } from "./http.js";
import type { AuthenticatedSession, LoginResult, User } from "./types.js";

/** Wraps POST/GET /api/v0/auth/* (internal/api/auth.go, internal/api/mfa.go). */
export class AuthResource {
  constructor(private readonly http: HttpClient) {}

  /** Authenticates with email/password. If the account has TOTP MFA
   * enabled, the returned LoginResult is a `{ mfaRequired: true, mfaToken }`
   * challenge instead of a session — check `"mfaRequired" in result` and, if
   * so, complete the login with verifyMfa. Otherwise the client's internal
   * bearer token is updated automatically, so subsequent calls on the same
   * client are authenticated without further setup. */
  async login(email: string, password: string): Promise<LoginResult> {
    const result = await this.http.requestJSON<LoginResult>("POST", "/api/v0/auth/login", {
      json: { email, password },
    });
    if (!("mfaRequired" in result)) {
      this.http.token = result.token;
    }
    return result;
  }

  /** Completes a login that returned an MfaChallenge: mfaToken is that
   * challenge's token, code is a TOTP code from the enrolled authenticator
   * app (or an unused recovery code). On success the client's bearer token
   * is updated, same as a plain login. */
  async verifyMfa(mfaToken: string, code: string): Promise<AuthenticatedSession> {
    const result = await this.http.requestJSON<AuthenticatedSession>("POST", "/api/v0/auth/mfa/verify", {
      json: { mfaToken, code },
    });
    this.http.token = result.token;
    return result;
  }

  /** Starts TOTP enrollment for the currently authenticated account:
   * returns the raw secret and its otpauth:// URI (render as a QR code).
   * Enrollment isn't active yet — call confirmMfaEnrollment with a code
   * generated from the secret to turn MFA on. */
  async beginMfaEnrollment(): Promise<{ secret: string; otpauthUrl: string }> {
    return this.http.requestJSON<{ secret: string; otpauthUrl: string }>("POST", "/api/v0/auth/mfa/enroll");
  }

  /** Verifies code against the secret beginMfaEnrollment returned; on
   * success MFA is enabled and the returned recovery codes are shown
   * exactly once — store them somewhere safe. */
  async confirmMfaEnrollment(code: string): Promise<{ recoveryCodes: string[] }> {
    return this.http.requestJSON<{ recoveryCodes: string[] }>("POST", "/api/v0/auth/mfa/confirm", {
      json: { code },
    });
  }

  /** Turns MFA off for the currently authenticated account. */
  async disableMfa(): Promise<void> {
    await this.http.requestVoid("POST", "/api/v0/auth/mfa/disable");
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
