/** An account known to Glyphux (internal/identity.User's JSON shape). */
export interface User {
  id: number;
  email: string;
  role: string;
}

/** The body of a successful POST /api/v0/auth/login — the authenticated
 * user plus the bearer token to send as `Authorization: Bearer <token>` on
 * subsequent requests (internal/api/auth.go loginResponse). */
export interface LoginResult extends User {
  token: string;
}

/** A content item's publication status (internal/content/content.go). */
export type ContentStatus = "draft" | "published";

/** A single piece of content: a typed, identified JSON document with a
 * publication status, a version number, and lifecycle timestamps
 * (internal/content/content.go Item). */
export interface ContentItem {
  id: string;
  type: string;
  data: Record<string, unknown>;
  status: ContentStatus;
  version: number;
  created_at: string;
  updated_at: string;
}

/** One immutable historical snapshot of a content item
 * (internal/content/content.go Version). */
export interface ContentVersion {
  version: number;
  data: Record<string, unknown>;
  status: ContentStatus;
  created_at: string;
}

/** A field kind Layer 1 understands (pkg/contract FieldType). */
export type FieldType = "string" | "richtext" | "number" | "boolean" | "date" | "relation" | "media";

/** One typed field declared on a content type (pkg/contract Field). */
export interface ContentTypeField {
  type: FieldType;
  required?: boolean;
  localized?: boolean;
  /** Target content type name; only meaningful (and required) for "relation" fields. */
  to?: string;
}

/** A content type's declared shape (pkg/contract ContentType) — the schema
 * content items of this type are validated against, not an item itself. */
export interface ContentType {
  fields: Record<string, ContentTypeField>;
}

/** A stored media asset and its metadata (internal/media/media.go Item). */
export interface MediaItem {
  id: string;
  filename: string;
  mime_type: string;
  size_bytes: number;
  width: number;
  height: number;
  alt_text: string;
  created_at: string;
  updated_at: string;
}
