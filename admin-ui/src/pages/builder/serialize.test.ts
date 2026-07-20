import { describe, expect, it } from "vitest";
import type { BlockDefinition, Layout } from "@glyphux/sdk";
import { emptyLayout, layoutToNodeTree, nodeTreeToLayout } from "./serialize";

// Seam: this module is the highest-value thing to unit test in this slice
// (per the ticket's own TDD guidance) — the drag-and-drop interaction
// itself is Craft.js's problem to get right, but the translation between
// its SerializedNodes node-graph and the exact contract.Layout JSON shape
// this app must round-trip through sdk-js's LayoutsResource is ours, and a
// silent mismatch here would corrupt every saved layout. No mocks: these
// are plain functions over plain data, exercised directly.

const registry: BlockDefinition[] = [
  { name: "heading", display_name: "Heading", props: { text: { type: "string", required: true } } },
  { name: "paragraph", display_name: "Paragraph", props: { text: { type: "richtext", required: true } } },
  { name: "container", display_name: "Container", slots: ["content"] },
];

describe("emptyLayout", () => {
  it("produces a structurally valid Layout with no regions", () => {
    expect(emptyLayout()).toEqual<Layout>({ contract_version: "layout-composition/v1", regions: {} });
  });
});

describe("layoutToNodeTree / nodeTreeToLayout round-trip", () => {
  it("round-trips an empty layout to a bare ROOT node and back", () => {
    const layout = emptyLayout();
    const nodes = layoutToNodeTree(layout, registry);
    expect(nodes.ROOT).toBeDefined();
    expect(nodes.ROOT.nodes).toEqual([]);
    expect(nodeTreeToLayout(nodes)).toEqual(layout);
  });

  it("round-trips a single region with one leaf block", () => {
    const layout: Layout = {
      contract_version: "layout-composition/v1",
      regions: {
        main: { blocks: [{ type: "heading", props: { text: "Hello", level: 1 } }] },
      },
    };
    const nodes = layoutToNodeTree(layout, registry);
    expect(nodeTreeToLayout(nodes)).toEqual(layout);
  });

  it("round-trips a container block with nested slot content", () => {
    const layout: Layout = {
      contract_version: "layout-composition/v1",
      regions: {
        main: {
          blocks: [
            {
              type: "container",
              slots: {
                content: [
                  { type: "heading", props: { text: "Nested" } },
                  { type: "paragraph", props: { text: "Body copy" } },
                ],
              },
            },
          ],
        },
      },
    };
    const nodes = layoutToNodeTree(layout, registry);
    expect(nodeTreeToLayout(nodes)).toEqual(layout);
  });

  it("round-trips multiple regions deterministically regardless of key order", () => {
    const layout: Layout = {
      contract_version: "layout-composition/v1",
      regions: {
        footer: { blocks: [{ type: "heading", props: { text: "Footer" } }] },
        header: { blocks: [{ type: "heading", props: { text: "Header" } }] },
      },
    };
    const a = nodeTreeToLayout(layoutToNodeTree(layout, registry));
    const b = nodeTreeToLayout(layoutToNodeTree({ ...layout, regions: { header: layout.regions.header, footer: layout.regions.footer } }, registry));
    expect(a).toEqual(b);
    expect(a).toEqual(layout);
  });

  it("gives every placed block a stable, path-derived node id (no randomness)", () => {
    const layout: Layout = {
      contract_version: "layout-composition/v1",
      regions: { main: { blocks: [{ type: "heading", props: { text: "A" } }] } },
    };
    const first = layoutToNodeTree(layout, registry);
    const second = layoutToNodeTree(layout, registry);
    expect(Object.keys(first).sort()).toEqual(Object.keys(second).sort());
  });

  it("creates a canvas placeholder for every slot a container declares, even with nothing dropped into it yet", () => {
    const layout: Layout = {
      contract_version: "layout-composition/v1",
      regions: { main: { blocks: [{ type: "container" }] } },
    };
    const nodes = layoutToNodeTree(layout, registry);
    const blockId = nodes.ROOT.nodes
      .flatMap((regionId) => nodes[regionId].nodes)
      .find((id) => (nodes[id].props as { blockType?: string }).blockType === "container")!;
    expect(blockId).toBeDefined();
    const slotCanvasId = nodes[blockId].linkedNodes.content;
    expect(slotCanvasId).toBeDefined();
    expect(nodes[slotCanvasId].isCanvas).toBe(true);
    expect(nodes[slotCanvasId].nodes).toEqual([]);
    // An empty declared slot round-trips back with no slots key at all,
    // since nodeTreeToLayout omits empty slot maps to match contract.Block's
    // own `slots,omitempty` semantics.
    expect(nodeTreeToLayout(nodes)).toEqual(layout);
  });

  it("preserves an unregistered block type's existing nested slot content on load (no silent data loss)", () => {
    const layout: Layout = {
      contract_version: "layout-composition/v1",
      regions: {
        main: {
          blocks: [
            {
              type: "removed-block",
              slots: { content: [{ type: "heading", props: { text: "Still here" } }] },
            },
          ],
        },
      },
    };
    // registry no longer knows "removed-block" — simulates a stale layout
    // saved against an older registry snapshot.
    const nodes = layoutToNodeTree(layout, registry);
    expect(nodeTreeToLayout(nodes)).toEqual(layout);
  });

  it("omits an empty props object on round-trip, matching contract.Block's props,omitempty", () => {
    const layout: Layout = {
      contract_version: "layout-composition/v1",
      regions: { main: { blocks: [{ type: "container" }] } },
    };
    const nodes = layoutToNodeTree(layout, registry);
    expect(nodeTreeToLayout(nodes)).toEqual(layout);
  });
});
