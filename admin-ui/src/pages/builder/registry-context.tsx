import { createContext, useContext } from "react";
import type { BlockDefinition } from "@glyphux/sdk";

/** The live block registry (BlocksResource.list()'s result), shared via
 * context rather than prop-drilled — the Craft.js node components below
 * (BlockNode in particular) are mounted internally by <Frame>'s own
 * rendering pipeline from the SerializedNodes graph, not from BuilderPage's
 * JSX tree, so there is no prop-drilling path to reach them; context is the
 * only way every node in the canvas can look up its own block's
 * DisplayName/Props/Slots. */
const BlockRegistryContext = createContext<BlockDefinition[]>([]);

export function BlockRegistryProvider({ registry, children }: { registry: BlockDefinition[]; children: React.ReactNode }) {
  return <BlockRegistryContext.Provider value={registry}>{children}</BlockRegistryContext.Provider>;
}

export function useBlockRegistry(): BlockDefinition[] {
  return useContext(BlockRegistryContext);
}

export function useBlockDefinition(type: string): BlockDefinition | undefined {
  const registry = useBlockRegistry();
  return registry.find((d) => d.name === type);
}
