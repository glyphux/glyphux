import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { Editor, Frame } from "@craftjs/core";
import { GlyphuxApiError, type BlockDefinition } from "@glyphux/sdk";
import { LivePreview, PREVIEW_DEBOUNCE_MS } from "./LivePreview";
import { resolver } from "./nodes";
import { emptyLayout, layoutToNodeTree } from "./serialize";
import { client } from "@/lib/client";

// Seam: LivePreview must render the CURRENT in-editor draft through the
// real server-side preview endpoint (LayoutsResource.preview), debounced,
// never a hand-rolled client-side reimplementation of block-to-HTML
// rendering — this only proves the wiring (serialize -> preview() call ->
// iframe srcDoc), since the actual HTML-correctness assertions belong to
// themes/starter's own Go tests and internal/api/layouts_test.go's preview
// coverage, not a duplicated client-side check here.
vi.mock("@/lib/client", () => ({
  client: {
    layouts: { preview: vi.fn() },
  },
}));

const registry: BlockDefinition[] = [
  { name: "heading", display_name: "Heading", props: { text: { type: "string", required: true } } },
];

function renderPreview() {
  return render(
    <Editor resolver={resolver}>
      <Frame data={layoutToNodeTree(emptyLayout(), registry)} />
      <LivePreview />
    </Editor>,
  );
}

describe("LivePreview", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.mocked(client.layouts.preview).mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("debounces before requesting a preview of the current draft", async () => {
    vi.mocked(client.layouts.preview).mockResolvedValue({
      html: "<h1>Hello</h1>",
      contentType: "text/html; charset=utf-8",
    });
    renderPreview();

    expect(client.layouts.preview).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(PREVIEW_DEBOUNCE_MS);

    expect(client.layouts.preview).toHaveBeenCalledTimes(1);
  });

  it("embeds the rendered HTML in an iframe via srcDoc", async () => {
    vi.mocked(client.layouts.preview).mockResolvedValue({
      html: "<h1>Hello</h1>",
      contentType: "text/html; charset=utf-8",
    });
    renderPreview();
    await vi.advanceTimersByTimeAsync(PREVIEW_DEBOUNCE_MS);

    await waitFor(() => {
      const frame = screen.getByTitle("Live preview") as HTMLIFrameElement;
      expect(frame.srcdoc).toBe("<h1>Hello</h1>");
    });
  });

  it("shows a real error state when the preview request fails", async () => {
    vi.mocked(client.layouts.preview).mockRejectedValue(new GlyphuxApiError("render failed", 500));
    renderPreview();
    await vi.advanceTimersByTimeAsync(PREVIEW_DEBOUNCE_MS);

    await waitFor(() => {
      expect(screen.getByText("render failed")).toBeInTheDocument();
    });
  });

  it("renders an unsandboxed-script-free iframe (sandbox attribute present)", () => {
    vi.mocked(client.layouts.preview).mockResolvedValue({ html: "", contentType: "text/html; charset=utf-8" });
    renderPreview();

    const frame = screen.getByTitle("Live preview") as HTMLIFrameElement;
    expect(frame.getAttribute("sandbox")).toBe("");
  });
});
