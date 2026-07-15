import { GlyphuxApiError } from "./errors.js";

/** Shared HTTP plumbing for every resource module: builds requests against
 * the daemon's /api/v0 surface, attaches the bearer token when one is set,
 * and maps non-2xx responses to a typed GlyphuxApiError. */
export class HttpClient {
  baseUrl: string;
  token: string | undefined;

  constructor(baseUrl: string, token?: string) {
    this.baseUrl = baseUrl.replace(/\/+$/, "");
    this.token = token;
  }

  private headers(extra?: Record<string, string>): Record<string, string> {
    const headers: Record<string, string> = { ...extra };
    if (this.token) headers["Authorization"] = `Bearer ${this.token}`;
    return headers;
  }

  /** Issues a request with an optional JSON body and returns the parsed JSON
   * response. Throws GlyphuxApiError for any non-2xx status. */
  async requestJSON<T>(
    method: string,
    path: string,
    options?: { json?: unknown; query?: Record<string, string | undefined> },
  ): Promise<T> {
    const url = this.buildUrl(path, options?.query);
    const headers = this.headers(options?.json !== undefined ? { "Content-Type": "application/json" } : undefined);
    const res = await fetch(url, {
      method,
      headers,
      body: options?.json !== undefined ? JSON.stringify(options.json) : undefined,
    });
    return this.parseJSONOrThrow<T>(res);
  }

  /** Issues a request with no body and no response payload (e.g. DELETE,
   * logout). Throws GlyphuxApiError for any non-2xx status. */
  async requestVoid(method: string, path: string): Promise<void> {
    const res = await fetch(this.buildUrl(path), { method, headers: this.headers() });
    if (!res.ok) {
      await this.throwError(res);
    }
  }

  /** Issues a multipart/form-data request (media upload) and returns the
   * parsed JSON response. */
  async requestMultipart<T>(method: string, path: string, form: FormData): Promise<T> {
    const res = await fetch(this.buildUrl(path), { method, headers: this.headers(), body: form });
    return this.parseJSONOrThrow<T>(res);
  }

  /** Issues a request and returns the raw Response, without JSON parsing or
   * error mapping — for binary endpoints like the media file route. */
  async requestRaw(method: string, path: string, query?: Record<string, string | undefined>): Promise<Response> {
    return fetch(this.buildUrl(path, query), { method, headers: this.headers() });
  }

  private async parseJSONOrThrow<T>(res: Response): Promise<T> {
    if (!res.ok) {
      await this.throwError(res);
    }
    if (res.status === 204) {
      return undefined as T;
    }
    return (await res.json()) as T;
  }

  private async throwError(res: Response): Promise<never> {
    let body: unknown;
    try {
      body = await res.json();
    } catch {
      body = undefined;
    }
    const message =
      body && typeof body === "object" && "error" in body && typeof (body as { error: unknown }).error === "string"
        ? (body as { error: string }).error
        : `request failed with status ${res.status}`;
    const issues =
      body && typeof body === "object" && Array.isArray((body as { issues?: unknown }).issues)
        ? ((body as { issues: string[] }).issues)
        : undefined;
    throw new GlyphuxApiError(message, res.status, { issues, body });
  }

  private buildUrl(path: string, query?: Record<string, string | undefined>): string {
    const url = new URL(this.baseUrl + path);
    if (query) {
      for (const [key, value] of Object.entries(query)) {
        if (value !== undefined) url.searchParams.set(key, value);
      }
    }
    return url.toString();
  }
}
