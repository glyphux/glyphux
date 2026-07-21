import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Editor, Frame } from "@craftjs/core";
import type { BlockDefinition, Layout, MediaItem } from "@glyphux/sdk";
import { PropsEditor } from "./PropsEditor";
import { BlockRegistryProvider } from "./registry-context";
import { resolver } from "./nodes";
import { layoutToNodeTree } from "./serialize";
import { ToastProvider } from "@/lib/toast-context";
import { client } from "@/lib/client";

// Seam: the props editor is driven entirely by a block's registered
// Definition.Props, rendered one FieldControl per field the exact same way
// pages/content/fields.tsx already does for Layer-1 fields (reused, not
// reinvented). Selecting a block in the canvas is what makes its own props
// appear here; editing a field must update the same node the Save button
// later reads. This includes a FieldMedia-kind prop (the `image` block's
// `src`, contract.FieldMedia) — the same FieldControl/MediaField/
// MediaPicker path Ticket P4.9 built, which this file must exercise too
// since it's the builder's own regression seam for that path (a bug
// specific to rendering FieldMedia inside Craft.js's Editor/Frame tree,
// e.g. the ToastProvider dependency MediaPicker has via useToast, would
// otherwise go uncaught).
vi.mock("@/lib/client", () => ({
  client: {
    media: { list: vi.fn(), get: vi.fn(), upload: vi.fn() },
  },
}));

function mediaItem(overrides: Partial<MediaItem> = {}): MediaItem {
  return {
    id: "img1",
    filename: "photo.png",
    mime_type: "image/png",
    size_bytes: 1024,
    width: 400,
    height: 300,
    alt_text: "",
    tags: [],
    source: "",
    attribution: "",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

const registry: BlockDefinition[] = [
  { name: "heading", display_name: "Heading", props: { text: { type: "string", required: true } } },
  { name: "container", display_name: "Container", slots: ["content"] },
  { name: "image", display_name: "Image", props: { src: { type: "media", required: true } } },
];

function renderEditor(layout: Layout) {
  render(
    <ToastProvider>
      <Editor resolver={resolver}>
        <BlockRegistryProvider registry={registry}>
          <Frame data={layoutToNodeTree(layout, registry)} />
          <PropsEditor />
        </BlockRegistryProvider>
      </Editor>
    </ToastProvider>,
  );
}

describe("PropsEditor", () => {
  beforeEach(() => {
    vi.mocked(client.media.list).mockReset();
    vi.mocked(client.media.get).mockReset();
  });

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

  it("a FieldMedia-kind prop (the image block's src) opens the real media picker, and selecting an asset updates the node", async () => {
    vi.mocked(client.media.list).mockResolvedValue([mediaItem()]);
    vi.mocked(client.media.get).mockResolvedValue(mediaItem());
    const user = userEvent.setup();
    renderEditor({
      contract_version: "layout-composition/v1",
      regions: { main: { blocks: [{ type: "image", props: {} }] } },
    });

    // Select the placed image block by clicking its badge in the canvas.
    await user.click(screen.getByText("Image"));

    await user.click(screen.getByRole("button", { name: /choose media/i }));
    await waitFor(() => expect(screen.getByText("photo.png")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /select photo.png/i }));

    // The picker closes and the field now resolves + displays the chosen
    // asset (via client.media.get, driven by the id actions.setProp wrote
    // onto the node) instead of the empty "No media selected" state.
    await waitFor(() => expect(client.media.get).toHaveBeenCalledWith("img1"));
    await waitFor(() => expect(screen.getByRole("button", { name: /^change$/i })).toBeInTheDocument());
    expect(screen.queryByText("No media selected")).not.toBeInTheDocument();
  });
});
