import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Editor, Frame } from "@craftjs/core";
import type { BlockDefinition } from "@glyphux/sdk";
import { Palette } from "./Palette";
import { resolver } from "./nodes";
import { emptyLayout, layoutToNodeTree } from "./serialize";

// Seam: the palette is the entry point for "drag blocks from the palette"
// — this proves it lists every registered block's display name and slot
// badges, and gives a real empty state rather than a blank panel when
// nothing is registered yet.

const registry: BlockDefinition[] = [
  { name: "heading", display_name: "Heading", props: { text: { type: "string", required: true } } },
  { name: "container", display_name: "Container", slots: ["content"] },
];

function renderPalette(blocks: BlockDefinition[]) {
  return render(
    <Editor resolver={resolver}>
      <Frame data={layoutToNodeTree(emptyLayout(), blocks)} />
      <Palette blocks={blocks} />
    </Editor>,
  );
}

describe("Palette", () => {
  it("lists every registered block's display name", () => {
    renderPalette(registry);
    expect(screen.getByText("Heading")).toBeInTheDocument();
    expect(screen.getByText("Container")).toBeInTheDocument();
  });

  it("shows a slot badge for container-shaped blocks, and none for leaf blocks", () => {
    renderPalette(registry);
    const containerItem = screen.getByRole("button", { name: /drag container/i });
    expect(containerItem.textContent).toContain("content");
    const headingItem = screen.getByRole("button", { name: /drag heading/i });
    expect(headingItem.textContent).not.toContain("content");
  });

  it("renders a real empty state when no blocks are registered", () => {
    renderPalette([]);
    expect(screen.getByText("No blocks registered")).toBeInTheDocument();
  });
});
