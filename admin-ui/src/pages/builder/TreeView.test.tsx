import { describe, expect, it } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { Editor, Frame } from "@craftjs/core";
import type { BlockDefinition, Layout } from "@glyphux/sdk";
import { TreeView } from "./TreeView";
import { BlockRegistryProvider } from "./registry-context";
import { resolver } from "./nodes";
import { layoutToNodeTree } from "./serialize";

// Seam: TreeView is the "plain structural tree view" the ticket calls for
// in place of a theme-accurate live preview (explicitly out of scope here).
// It must reflect exactly what's in the draft, nested under the right
// region/slot.

const registry: BlockDefinition[] = [
  { name: "heading", display_name: "Heading", props: { text: { type: "string" } } },
  { name: "container", display_name: "Container", slots: ["content"] },
];

// Frame renders the same block names in the live canvas that TreeView
// reflects in its own read-out, so every assertion below is scoped to
// TreeView's own "Draft structure" card rather than the whole document.
function renderTree(layout: Layout) {
  render(
    <Editor resolver={resolver}>
      <BlockRegistryProvider registry={registry}>
        <Frame data={layoutToNodeTree(layout, registry)} />
        <TreeView />
      </BlockRegistryProvider>
    </Editor>,
  );
  const heading = screen.getByRole("heading", { name: "Draft structure" });
  return within(heading.closest("div")!.parentElement as HTMLElement);
}

describe("TreeView", () => {
  it("shows a real empty message when there are no regions yet", () => {
    const tree = renderTree({ contract_version: "layout-composition/v1", regions: {} });
    expect(tree.getByText("No regions yet.")).toBeInTheDocument();
  });

  it("lists a region name and its placed block's display name", () => {
    const tree = renderTree({
      contract_version: "layout-composition/v1",
      regions: { main: { blocks: [{ type: "heading", props: { text: "Hi" } }] } },
    });
    expect(tree.getByText("main")).toBeInTheDocument();
    expect(tree.getByText("Heading")).toBeInTheDocument();
  });

  it("nests a container's slot content under the slot name", () => {
    const tree = renderTree({
      contract_version: "layout-composition/v1",
      regions: {
        main: {
          blocks: [{ type: "container", slots: { content: [{ type: "heading", props: { text: "Nested" } }] } }],
        },
      },
    });
    expect(tree.getByText("Container")).toBeInTheDocument();
    expect(tree.getByText("content:")).toBeInTheDocument();
    expect(tree.getByText("Heading")).toBeInTheDocument();
  });

  it("flags an unregistered block type instead of silently hiding it", () => {
    const tree = renderTree({
      contract_version: "layout-composition/v1",
      regions: { main: { blocks: [{ type: "vanished-block" }] } },
    });
    expect(tree.getByText("vanished-block (unregistered)")).toBeInTheDocument();
  });
});
