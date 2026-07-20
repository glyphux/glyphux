import { Element, useEditor } from "@craftjs/core";
import type { BlockDefinition } from "@glyphux/sdk";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/layout/EmptyState";
import { BlockNode, SlotCanvas } from "./nodes";

/** The block palette: one draggable entry per registered block type
 * (BlocksResource.list()'s result). Dragging an entry onto any region or
 * slot canvas creates a new BlockNode there via Craft.js's own `create`
 * connector — no dnd-kit needed for this: Craft.js's connector already
 * performs real HTML5 drag-and-drop from an arbitrary source element onto
 * any registered canvas, which is exactly "drag blocks from the palette
 * into named regions/slots" (see the tracking doc's Craft.js-vs-Puck
 * decision for why layering dnd-kit on top would just be a second,
 * redundant DnD engine). */
export function Palette({ blocks }: { blocks: BlockDefinition[] }) {
  if (blocks.length === 0) {
    return (
      <EmptyState
        title="No blocks registered"
        description="No Layer-2 block types are registered on this server yet — nothing to place."
      />
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Blocks</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        {blocks.map((def) => (
          <PaletteItem key={def.name} def={def} />
        ))}
      </CardContent>
    </Card>
  );
}

function PaletteItem({ def }: { def: BlockDefinition }) {
  const { connectors } = useEditor();

  return (
    <div
      ref={(el) => {
        if (!el) return;
        connectors.create(
          el,
          <Element is={BlockNode} blockType={def.name} blockProps={{}} canvas={false}>
            {(def.slots ?? []).map((slotName) => (
              <Element key={slotName} id={slotName} canvas is={SlotCanvas} slotName={slotName} />
            ))}
          </Element>,
        );
      }}
      className="border-border bg-background hover:border-primary flex cursor-grab flex-col gap-1 rounded-md border p-2 active:cursor-grabbing"
      role="button"
      aria-label={`Drag ${def.display_name} onto the canvas`}
    >
      <span className="text-body font-medium">{def.display_name}</span>
      {def.slots && def.slots.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {def.slots.map((slot) => (
            <Badge key={slot} variant="outline">
              {slot}
            </Badge>
          ))}
        </div>
      )}
    </div>
  );
}
