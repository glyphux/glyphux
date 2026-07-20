import type { HttpClient } from "./http.js";
import type { BlockDefinition } from "./types.js";

/** Wraps GET /api/v0/blocks (internal/api/layouts.go handleBlocksList) — the
 * shared, running daemon's registered Layer-2 block types. Read-only: blocks
 * are registered by plugins/themes server-side (pkg/blocks.Registry), not
 * created over HTTP. */
export class BlocksResource {
  constructor(private readonly http: HttpClient) {}

  /** Returns every block type currently registered in the running daemon,
   * in no particular order. */
  async list(): Promise<BlockDefinition[]> {
    const { blocks } = await this.http.requestJSON<{ blocks: BlockDefinition[] }>("GET", "/api/v0/blocks");
    return blocks;
  }
}
