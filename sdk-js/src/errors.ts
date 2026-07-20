/** Thrown for any non-2xx response from the Glyphux API. Carries enough of
 * the response to let callers branch on status/issues instead of re-parsing
 * the raw body themselves. */
export class GlyphuxApiError extends Error {
  /** The HTTP status code of the failing response. */
  readonly status: number;
  /** Field-level validation issues, present on 422 validation failures
   * (see content.ValidationError / writeContentError in internal/api/api.go). */
  readonly issues?: string[];
  /** The raw parsed JSON error body, for anything not covered above. */
  readonly body?: unknown;

  constructor(message: string, status: number, options?: { issues?: string[]; body?: unknown }) {
    super(message);
    this.name = "GlyphuxApiError";
    this.status = status;
    this.issues = options?.issues;
    this.body = options?.body;
  }
}
