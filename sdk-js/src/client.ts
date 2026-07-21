import { AiResource } from "./ai.js";
import { AuthResource } from "./auth.js";
import { BlocksResource } from "./blocks.js";
import { BundlesResource } from "./bundles.js";
import { ContentResource } from "./content.js";
import { ContentTypesResource } from "./content-types.js";
import { HttpClient } from "./http.js";
import { LayoutsResource } from "./layouts.js";
import { MediaResource } from "./media.js";
import { PresetsResource } from "./presets.js";
import { UsersResource } from "./users.js";

export interface GlyphuxClientOptions {
  /** Base URL of the running glyphuxd instance, e.g. "http://localhost:8080". */
  baseUrl: string;
  /** An existing bearer token to authenticate with, if already known (e.g.
   * from a prior login persisted by the caller). Optional — call
   * client.auth.login() instead to obtain one. */
  token?: string;
}

/** The typed Glyphux API client — the SDK's single entry point. */
export class GlyphuxClient {
  private readonly http: HttpClient;
  readonly auth: AuthResource;
  readonly content: ContentResource;
  readonly contentTypes: ContentTypesResource;
  readonly media: MediaResource;
  readonly users: UsersResource;
  readonly blocks: BlocksResource;
  readonly layouts: LayoutsResource;
  readonly presets: PresetsResource;
  readonly bundles: BundlesResource;
  readonly ai: AiResource;

  constructor(options: GlyphuxClientOptions) {
    this.http = new HttpClient(options.baseUrl, options.token);
    this.auth = new AuthResource(this.http);
    this.content = new ContentResource(this.http);
    this.contentTypes = new ContentTypesResource(this.http);
    this.media = new MediaResource(this.http);
    this.users = new UsersResource(this.http);
    this.blocks = new BlocksResource(this.http);
    this.layouts = new LayoutsResource(this.http);
    this.presets = new PresetsResource(this.http);
    this.bundles = new BundlesResource(this.http);
    this.ai = new AiResource(this.http);
  }

  /** The bearer token currently in use, if any (set by auth.login() or the
   * constructor's `token` option). */
  get token(): string | undefined {
    return this.http.token;
  }

  /** Overrides the bearer token used for subsequent requests. */
  set token(value: string | undefined) {
    this.http.token = value;
  }
}
