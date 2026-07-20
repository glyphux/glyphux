/** An account known to Glyphux (internal/identity.User's JSON shape). */
export interface User {
  id: number;
  email: string;
  role: string;
  /** Whether TOTP MFA is enabled for this account. */
  mfaEnabled: boolean;
  /** Whether this account can currently authenticate. */
  active: boolean;
}

/** The body of a successful POST /api/v0/auth/login — the authenticated
 * user plus the bearer token to send as `Authorization: Bearer <token>` on
 * subsequent requests (internal/api/auth.go loginResponse). */
export interface AuthenticatedSession extends User {
  token: string;
}

/** The body POST /api/v0/auth/login returns instead, when the account has
 * TOTP MFA enabled: the password verified, but no session is issued yet —
 * complete the login with AuthResource.verifyMfa(mfaToken, code)
 * (internal/api/auth.go mfaChallengeResponse). */
export interface MfaChallenge {
  mfaRequired: true;
  mfaToken: string;
}

/** POST /api/v0/auth/login's response is one of these two shapes — check
 * `"mfaRequired" in result` to discriminate. */
export type LoginResult = AuthenticatedSession | MfaChallenge;

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
  tags: string[];
  source: string;
  attribution: string;
  created_at: string;
  updated_at: string;
}

/** The editable metadata on a media item — alt text, tags, and
 * source/attribution (PRD §11.4). A full replace: send back every field
 * you want kept (internal/media.MetadataUpdate). */
export interface MediaMetadataUpdate {
  alt_text: string;
  tags: string[];
  source: string;
  attribution: string;
}

/** An image transform pipeline — crop, then rotate, then resize, then
 * re-encode in an explicit format (internal/media.TransformOptions) — sent
 * as query params on GET .../file. Omitting a field skips that stage;
 * rotate must be 0, 90, 180, or 270 if given. */
export interface MediaTransform {
  cropX?: number;
  cropY?: number;
  cropW?: number;
  cropH?: number;
  rotate?: number;
  width?: number;
  height?: number;
  format?: "jpeg" | "png";
}

/** A registered Layer-2 block type's prop schema and, for a container-shaped
 * block, its named slots (pkg/blocks.Definition, PRD §14 slice 4.4a). A leaf
 * block (e.g. "heading") simply has no `slots`. */
export interface BlockDefinition {
  name: string;
  display_name: string;
  props?: Record<string, ContentTypeField>;
  slots?: string[];
}

/** One placed instance of a registered block type within a Layout
 * (pkg/contract.Block). `slots`, for container-shaped blocks, holds nested
 * blocks per named slot; a leaf block has no `slots`. */
export interface LayoutBlock {
  type: string;
  props?: Record<string, unknown>;
  slots?: Record<string, LayoutBlock[]>;
}

/** A named placement area within a layout/template — e.g. "header", "main",
 * "sidebar", "footer" (pkg/contract.Region). */
export interface LayoutRegion {
  blocks: LayoutBlock[];
}

/** The Layer-2 root document: the arrangement of blocks across a
 * route/template's named regions (pkg/contract.Layout, PRD §14 slice 4.4a).
 * Always send back the full document — PUT is a whole-document replace, not
 * a per-region patch. */
export interface Layout {
  contract_version: string;
  regions: Record<string, LayoutRegion>;
}

/** The compatibility-contract declaration a Composition Preset or Bundle
 * carries (pkg/contract.Manifest, PRD §13.3): what the artifact assumes
 * about the host it's imported into — a required Layer-2 contract version,
 * the block types it references, the theme regions it targets, and
 * (optionally) which theme(s) it was designed for. */
export interface Manifest {
  requires_contract: string;
  blocks?: string[];
  slots?: string[];
  themes?: string[];
}

/** A saved, importable fragment of Layer-2 composition (pkg/contract.
 * CompositionPreset, PRD §13.2 Ticket P4.6): a named arrangement of blocks
 * with placeholder content, at any scale from a section to a full page.
 * "Import a template" = merge `layout` into a target route's Layout. */
export interface CompositionPreset {
  contract_version: string;
  name: string;
  description?: string;
  layout: Layout;
  manifest: Manifest;
}

/** A saved CompositionPreset plus the server-assigned identity/timestamps
 * around it (internal/preset.Record) — what GET/POST /api/v0/presets
 * return; `id` is what save()/check()/import() reference a preset by,
 * distinct from its human-chosen `name`. */
export interface PresetRecord extends CompositionPreset {
  id: string;
  created_at: string;
  updated_at: string;
}

/** One Layer-1 content item a Composition Bundle seeds on import
 * (pkg/contract.SampleContentItem) — the same content type name + field
 * data shape internal/content.API.Create already accepts. */
export interface SampleContentItem {
  type: string;
  data: Record<string, unknown>;
}

/** A preset collection at site scale: pages + presets + sample content + a
 * theme reference (pkg/contract.CompositionBundle, PRD §13.2 Ticket P4.6) —
 * "import this starter site / demo," expressed as typed composition. */
export interface CompositionBundle {
  contract_version: string;
  name: string;
  description?: string;
  theme?: string;
  pages: Record<string, Layout>;
  presets?: CompositionPreset[];
  sample_content?: SampleContentItem[];
  manifest: Manifest;
}

/** A saved CompositionBundle plus the server-assigned identity/timestamps
 * around it (internal/bundle.Record). */
export interface BundleRecord extends CompositionBundle {
  id: string;
  created_at: string;
  updated_at: string;
}

/** The compatibility contract's structured outcome (pkg/compat.Result, PRD
 * §13.3): never a bare boolean — enough detail to render "this preset needs
 * the `pricing-table` block — install it?" rather than a blind rejection. */
export interface CompatResult {
  compatible: boolean;
  missing_blocks?: string[];
  missing_slots?: string[];
  unsupported_contract?: string;
}

/** One Layer-1 content item a bundle Import actually created
 * (internal/bundle.ContentRef). */
export interface ContentRef {
  type: string;
  id: string;
}

/** The result of importing a Composition Bundle (internal/bundle.
 * ImportResult): the compatibility Result, which page routes were merged,
 * and which sample content items were created (or failed to create —
 * best-effort, see `content_errors`). Empty/absent import_pages and
 * created_content mean the bundle was incompatible and nothing was
 * imported — check `compat.compatible` first. */
export interface BundleImportResult {
  compat: CompatResult;
  imported_pages?: string[];
  created_content?: ContentRef[];
  content_errors?: string[];
}
