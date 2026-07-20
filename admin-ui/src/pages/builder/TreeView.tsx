import { useEditor } from "@craftjs/core";
import type { Layout, LayoutBlock } from "@glyphux/sdk";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { nodeTreeToLayout } from "./serialize";
import { useBlockRegistry } from "./registry-context";

/** A plain structural read-out of the current in-editor draft — what's
 * placed where, region by region, slot by slot. Explicitly NOT a
 * theme-accurate preview (that's P4.5, out of scope here per the ticket) —
 * this exists purely so an author can see the shape of what they've built
 * without leaving the builder. Derived straight from the same
 * nodeTreeToLayout the Save button calls, so what's shown here and what
 * gets persisted can never disagree with each other. */
export function TreeView() {
  // The collector re-runs on every editor state change, so `serialized`
  // (and therefore `layout` below) always reflects the current draft.
  const { serialized } = useEditor((_state, query) => ({ serialized: query.getSerializedNodes() }));
  const registry = useBlockRegistry();

  const layout: Layout = nodeTreeToLayout(serialized);

  const regionNames = Object.keys(layout.regions).sort();

  return (
    <Card>
      <CardHeader>
        <CardTitle>Draft structure</CardTitle>
      </CardHeader>
      <CardContent>
        {regionNames.length === 0 && <p className="text-muted-foreground text-body">No regions yet.</p>}
        <ul className="flex flex-col gap-3">
          {regionNames.map((name) => (
            <li key={name}>
              <p className="text-small font-semibold tracking-wide uppercase">{name}</p>
              {layout.regions[name].blocks.length === 0 ? (
                <p className="text-muted-foreground text-small pl-3 italic">empty</p>
              ) : (
                <BlockList blocks={layout.regions[name].blocks} depth={0} />
              )}
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  );

  function BlockList({ blocks, depth }: { blocks: LayoutBlock[]; depth: number }) {
    return (
      <ul className="flex flex-col gap-1" style={{ paddingLeft: `${(depth + 1) * 0.75}rem` }}>
        {blocks.map((block, i) => {
          const def = registry.find((d) => d.name === block.type);
          return (
            <li key={i} className="text-body">
              <span>{def?.display_name ?? `${block.type} (unregistered)`}</span>
              {block.slots &&
                Object.entries(block.slots).map(([slotName, children]) => (
                  <div key={slotName} className="pl-3">
                    <p className="text-muted-foreground text-small">{slotName}:</p>
                    <BlockList blocks={children} depth={depth + 1} />
                  </div>
                ))}
            </li>
          );
        })}
      </ul>
    );
  }
}
