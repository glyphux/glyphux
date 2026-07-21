import { ROOT_NODE, type SerializedNode, type SerializedNodes } from "@craftjs/core";
import type { BlockDefinition, Layout, LayoutBlock } from "@glyphux/sdk";

/** The Layer-2 contract version this builder writes — matches
 * pkg/contract.LayoutCompositionV1 (there is exactly one supported version
 * today, so this is a plain constant, not a negotiated value). */
const CONTRACT_VERSION = "layout-composition/v1";

/** A fresh, structurally valid Layout with no regions yet — what the
 * builder starts from when LayoutsResource.get(route) 404s (no layout has
 * been saved for this route). */
export function emptyLayout(): Layout {
  return { contract_version: CONTRACT_VERSION, regions: {} };
}

/** node "type" identifiers used in the SerializedNodes graph this builder
 * maintains. These are resolver keys (see ./nodes.tsx's `resolver` export),
 * not contract.Block types — kept separate from the block-registry
 * namespace so a first-party block never collides with a builder-internal
 * node kind. */
const PAGE_TYPE = "Page";
const REGION_TYPE = "RegionCanvas";
const BLOCK_TYPE = "BlockNode";
const SLOT_TYPE = "SlotCanvas";

function regionNodeId(name: string): string {
  return `region:${name}`;
}

function slotCanvasNodeId(blockId: string, slotName: string): string {
  return `${blockId}.slot:${slotName}`;
}

function blockNodeId(parentCanvasId: string, index: number): string {
  return `${parentCanvasId}[${index}]`;
}

function lookupDefinition(registry: BlockDefinition[], type: string): BlockDefinition | undefined {
  return registry.find((d) => d.name === type);
}

function baseNode(overrides: Partial<SerializedNode> & Pick<SerializedNode, "type" | "parent">): SerializedNode {
  const defaultDisplayName = typeof overrides.type === "string" ? overrides.type : overrides.type.resolvedName;
  return {
    isCanvas: false,
    props: {},
    custom: {},
    hidden: false,
    nodes: [],
    linkedNodes: {},
    displayName: defaultDisplayName,
    ...overrides,
  };
}

/** Builds a full Craft.js SerializedNodes graph — a ROOT "Page" node
 * containing one RegionCanvas per region (sorted by name, so output is
 * deterministic regardless of the Layout.Regions map's own iteration
 * order), each holding BlockNode children; container-shaped blocks get one
 * SlotCanvas per declared slot (from the live block registry, falling back
 * to whatever slot keys the saved Layout itself already has, so an
 * unregistered/removed block type's existing nested content is preserved
 * rather than silently dropped — see serialize.test.ts). Node ids are
 * derived purely from tree position, not randomly generated, so the same
 * Layout always produces the same graph. */
export function layoutToNodeTree(layout: Layout, registry: BlockDefinition[]): SerializedNodes {
  const nodes: SerializedNodes = {};

  const regionIds = Object.keys(layout.regions)
    .sort()
    .map((name) => {
      const id = regionNodeId(name);
      const region = layout.regions[name];
      const blockIds = region.blocks.map((block, i) => buildBlockNode(nodes, block, id, i, registry));
      nodes[id] = baseNode({
        type: { resolvedName: REGION_TYPE },
        isCanvas: true,
        props: { name },
        parent: ROOT_NODE,
        nodes: blockIds,
      });
      return id;
    });

  nodes[ROOT_NODE] = baseNode({
    type: { resolvedName: PAGE_TYPE },
    isCanvas: true,
    parent: null,
    nodes: regionIds,
  });

  return nodes;
}

function buildBlockNode(
  nodes: SerializedNodes,
  block: LayoutBlock,
  parentCanvasId: string,
  index: number,
  registry: BlockDefinition[],
): string {
  const id = blockNodeId(parentCanvasId, index);
  const def = lookupDefinition(registry, block.type);
  const slotNames = def?.slots ?? Object.keys(block.slots ?? {});

  const linkedNodes: Record<string, string> = {};
  for (const slotName of slotNames) {
    const canvasId = slotCanvasNodeId(id, slotName);
    const slotBlocks = block.slots?.[slotName] ?? [];
    const childIds = slotBlocks.map((child, i) => buildBlockNode(nodes, child, canvasId, i, registry));
    nodes[canvasId] = baseNode({
      type: { resolvedName: SLOT_TYPE },
      isCanvas: true,
      props: { slotName },
      parent: id,
      nodes: childIds,
    });
    linkedNodes[slotName] = canvasId;
  }

  nodes[id] = baseNode({
    type: { resolvedName: BLOCK_TYPE },
    isCanvas: false,
    props: { blockType: block.type, blockProps: block.props ?? {} },
    displayName: def?.display_name ?? block.type,
    parent: parentCanvasId,
    linkedNodes,
  });

  return id;
}

/** The inverse of layoutToNodeTree: walks the ROOT node's RegionCanvas
 * children, and each BlockNode's linkedNodes (one per declared slot), to
 * rebuild the exact contract.Layout JSON shape (`{contract_version,
 * regions: {name: {blocks: [{type, props, slots}]}}}`) LayoutsResource.save
 * persists. Mirrors contract.Block's own `props,omitempty`/
 * `slots,omitempty` — an empty props object or a slot with nothing dropped
 * into it round-trips back to no key at all, not an empty object/array. */
export function nodeTreeToLayout(nodes: SerializedNodes): Layout {
  const root = nodes[ROOT_NODE];
  const regions: Layout["regions"] = {};

  for (const regionId of root?.nodes ?? []) {
    const regionNode = nodes[regionId];
    const name = String(regionNode.props.name);
    regions[name] = { blocks: regionNode.nodes.map((id) => nodeToBlock(nodes, id)) };
  }

  return { contract_version: CONTRACT_VERSION, regions };
}

function nodeToBlock(nodes: SerializedNodes, id: string): LayoutBlock {
  const node = nodes[id];
  const type = String(node.props.blockType);
  const props = (node.props.blockProps as Record<string, unknown>) ?? {};

  const slots: Record<string, LayoutBlock[]> = {};
  for (const [slotName, canvasId] of Object.entries(node.linkedNodes)) {
    const canvas = nodes[canvasId];
    const children = (canvas?.nodes ?? []).map((childId) => nodeToBlock(nodes, childId));
    if (children.length > 0) slots[slotName] = children;
  }

  const block: LayoutBlock = { type };
  if (Object.keys(props).length > 0) block.props = props;
  if (Object.keys(slots).length > 0) block.slots = slots;
  return block;
}
