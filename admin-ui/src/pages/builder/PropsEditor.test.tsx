import { describe, expect, it } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Editor, Frame } from "@craftjs/core";
import type { BlockDefinition, Layout } from "@glyphux/sdk";
import { PropsEditor } from "./PropsEditor";
import { BlockRegistryProvider } from "./registry-context";
import { resolver } from "./nodes";
import { layoutToNodeTree } from "./serialize";

// Seam: the props editor is driven entirely by a block's registered
// Definition.Props, rendered one FieldControl per field the exact same way
// pages/content/fields.tsx already does for Layer-1 fields (reused, not
// reinvented). Selecting a block in the canvas is what makes its own props
// appear here; editing a field must update the same node the Save button
// later reads.

const registry: BlockDefinition[] = [
  { name: "heading", display_name: "Heading", props: { text: { type: "string", required: true } } },
  { name: "container", display_name: "Container", slots: ["content"] },
];

function renderEditor(layout: Layout) {
  render(
    <Editor resolver={resolver}>
      <BlockRegistryProvider registry={registry}>
        <Frame data={layoutToNodeTree(layout, registry)} />
        <PropsEditor />
      </BlockRegistryProvider>
    </Editor>,
  );
}

describe("PropsEditor", () => {
  it("prompts to select a block when nothing is selected", () => {
    renderEditor({ contract_version: "layout-composition/v1", regions: { main: { blocks: [] } } });
    expect(screen.getByText("Select a block to edit its properties.")).toBeInTheDocument();
  });

  it("renders one field per the selected block's registered props, and edits flow through onChange", async () => {
    const user = userEvent.setup();
    renderEditor({
      contract_version: "layout-composition/v1",
      regions: { main: { blocks: [{ type: "heading", props: { text: "Hello" } }] } },
    });

    // Select the placed heading block by clicking its badge in the canvas.
    await user.click(screen.getByText("Heading"));

    const card = screen.getByRole("heading", { name: "Heading" }).closest("div")!.parentElement as HTMLElement;
    const editor = within(card);
    const input = editor.getByLabelText(/text/i) as HTMLInputElement;
    expect(input.value).toBe("Hello");

    await user.clear(input);
    await user.type(input, "Updated");
    expect(input.value).toBe("Updated");
  });

  it("flags a block whose type is no longer registered", async () => {
    const user = userEvent.setup();
    renderEditor({
      contract_version: "layout-composition/v1",
      regions: { main: { blocks: [{ type: "vanished" }] } },
    });
    await user.click(screen.getByText("vanished (unregistered)"));
    expect(screen.getByText("This block type is no longer registered on the server.")).toBeInTheDocument();
  });
});
