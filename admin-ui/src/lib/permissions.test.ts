import { describe, expect, it } from "vitest";
import { allows } from "./permissions";

// Seam: the pure `allows(role, capability)` function — the UI-side mirror
// of internal/permission/permission.go's v1 role matrix. Expected values
// come from that Go source (admin holds everything, editor holds
// content:read/read_drafts/write + media:write, viewer holds only
// content:read), not recomputed here.
describe("allows", () => {
  it("grants admin every capability", () => {
    expect(allows("admin", "content:write")).toBe(true);
    expect(allows("admin", "users:manage")).toBe(true);
    expect(allows("admin", "content_types:manage")).toBe(true);
  });

  it("grants editor content and media writes but not users or content-type management", () => {
    expect(allows("editor", "content:write")).toBe(true);
    expect(allows("editor", "media:write")).toBe(true);
    expect(allows("editor", "content:read_drafts")).toBe(true);
    expect(allows("editor", "users:manage")).toBe(false);
    expect(allows("editor", "content_types:manage")).toBe(false);
    expect(allows("editor", "content:publish")).toBe(false);
  });

  it("grants viewer only content:read", () => {
    expect(allows("viewer", "content:read")).toBe(true);
    expect(allows("viewer", "content:write")).toBe(false);
    expect(allows("viewer", "media:write")).toBe(false);
  });

  it("grants an unknown or missing role nothing", () => {
    expect(allows("superadmin", "content:read")).toBe(false);
    expect(allows(undefined, "content:read")).toBe(false);
  });
});
