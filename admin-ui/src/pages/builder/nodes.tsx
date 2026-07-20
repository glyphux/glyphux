import { Element, useEditor, useNode, type Resolver } from "@craftjs/core";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { useBlockDefinition } from "./registry-context";

/** The single root node every Frame hydrates into — one canvas holding a
 * RegionCanvas per region. Never placed/removed by the user directly (it's
 * the tree's fixed anchor), so it renders no chrome of its own. */
export function Page({ children }: { children?: React.ReactNode }) {
  return <div className="flex flex-col gap-6">{children}</div>;
}
Page.craft = { isCanvas: true, displayName: "Page" };

/** One named top-level placement area (contract.Layout.Regions' keys) —
 * always a canvas, always present even when empty, so the user always has
 * somewhere in that region to drop a first block. */
export function RegionCanvas({ name, children }: { name: string; children?: React.ReactNode }) {
  const {
    connectors: { connect },
  } = useNode();
  return (
    <div
      ref={(el) => {
        if (el) connect(el);
      }}
      className="border-border bg-card rounded-lg border p-4"
      data-region={name}
    >
      <p className="text-muted-foreground text-small mb-3 font-semibold tracking-wide uppercase">{name}</p>
      <div className="flex flex-col gap-2">
        {children}
        {!children && <p className="text-muted-foreground text-small italic">Drop blocks here</p>}
      </div>
    </div>
  );
}
RegionCanvas.craft = { isCanvas: true, displayName: "RegionCanvas" };

/** One placed instance of a registered block type (contract.Block). Not
 * itself a canvas — a container-shaped block's nested content lives in a
 * SlotCanvas per declared slot, rendered as this component's own children
 * below (a Craft.js "linked node" per slot, keyed by slot name, matching
 * contract.Block.Slots' own map-by-slot-name shape). Selecting this node
 * (click) is what the props editor sidebar reflects. */
export function BlockNode({ blockType }: { blockType: string; blockProps?: Record<string, unknown> }) {
  const {
    connectors: { connect, drag },
    id,
    selected,
  } = useNode((node) => ({ selected: node.events.selected }));
  const { actions } = useEditor();
  const def = useBlockDefinition(blockType);

  return (
    <div
      ref={(el) => {
        if (el) connect(drag(el));
      }}
      onClick={(e) => {
        e.stopPropagation();
        actions.selectNode(id);
      }}
      className={cn(
        "border-border bg-background flex flex-col gap-2 rounded-md border p-3",
        selected && "ring-primary ring-2",
      )}
    >
      <div className="flex items-center gap-2">
        <Badge variant={def ? "secondary" : "destructive"}>{def?.display_name ?? `${blockType} (unregistered)`}</Badge>
      </div>
      <BlockSlots def={def} />
    </div>
  );
}
BlockNode.craft = { isCanvas: false, displayName: "BlockNode" };

function BlockSlots({ def }: { def: { slots?: string[] } | undefined }) {
  if (!def?.slots || def.slots.length === 0) return null;
  // Named slots are declared as JSX children here so Craft.js's element
  // parser registers each as a linked node keyed by `id` (the slot name) —
  // this is what makes layoutToNodeTree/nodeTreeToLayout's linkedNodes
  // walk line up with contract.Block.Slots' own keys.
  return (
    <div className="flex flex-col gap-2 pl-3">
      {def.slots.map((slotName) => (
        <Element key={slotName} id={slotName} canvas is={SlotCanvas} slotName={slotName} />
      ))}
    </div>
  );
}

/** One named slot's drop area within a container-shaped block. */
export function SlotCanvas({ slotName, children }: { slotName: string; children?: React.ReactNode }) {
  const {
    connectors: { connect },
  } = useNode();
  return (
    <div
      ref={(el) => {
        if (el) connect(el);
      }}
      className="border-border rounded border border-dashed p-2"
      data-slot={slotName}
    >
      <p className="text-muted-foreground text-small mb-1">{slotName}</p>
      <div className="flex flex-col gap-2">
        {children}
        {!children && <p className="text-muted-foreground text-small italic">Drop into "{slotName}"</p>}
      </div>
    </div>
  );
}
SlotCanvas.craft = { isCanvas: true, displayName: "SlotCanvas" };

/** Resolver map handed to <Editor resolver={...}> — every node "type" this
 * builder's SerializedNodes graph can reference (see ./serialize.ts's
 * PAGE_TYPE/REGION_TYPE/BLOCK_TYPE/SLOT_TYPE constants) must have an entry
 * here with the exact same resolvedName, or Frame hydration throws. */
export const resolver: Resolver = {
  Page,
  RegionCanvas,
  BlockNode,
  SlotCanvas,
};
