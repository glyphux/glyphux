import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { GlyphuxApiError } from "@glyphux/sdk";
import { AIComposePanel } from "./AIComposePanel";
import { ToastProvider } from "@/lib/toast-context";
import { client } from "@/lib/client";

// Seam: AIComposePanel calls AiResource.compose() with the typed prompt,
// renders a compatible result's preview through the same iframe-srcDoc
// pattern LivePreview.tsx already established, renders an incompatible
// result's real compat diagnostics inline (never a generic error), and on
// accept calls PresetsResource.save() then PresetsResource.import() —
// exactly the existing preset-import path, never a second insert path.
vi.mock("@/lib/client", () => ({
  client: {
    ai: { compose: vi.fn() },
    presets: { save: vi.fn(), import: vi.fn() },
  },
}));

function renderPanel(onAccepted = vi.fn()) {
  return render(
    <ToastProvider>
      <AIComposePanel route="home" onAccepted={onAccepted} />
    </ToastProvider>,
  );
}

const compatibleResult = {
  compatible: true as const,
  fragment: {
    contract_version: "composition-preset/v1",
    name: "ai-hero",
    layout: {
      contract_version: "layout-composition/v1",
      regions: { main: { blocks: [{ type: "heading", props: { text: "Welcome" } }] } },
    },
    manifest: { requires_contract: "layout-composition/v1", blocks: ["heading"], slots: ["main"] },
  },
  preview: { html: "<h1>Welcome</h1>", content_type: "text/html; charset=utf-8" },
};

const incompatibleResult = {
  compatible: false as const,
  missing_blocks: ["does-not-exist"],
};

describe("AIComposePanel", () => {
  beforeEach(() => {
    vi.mocked(client.ai.compose).mockReset();
    vi.mocked(client.presets.save).mockReset();
    vi.mocked(client.presets.import).mockReset();
  });

  it("does not call compose() until Generate is clicked", () => {
    renderPanel();
    expect(client.ai.compose).not.toHaveBeenCalled();
  });

  it("disables Generate until a prompt is entered", () => {
    renderPanel();
    expect(screen.getByRole("button", { name: /generate/i })).toBeDisabled();
  });

  it("calls compose() with the typed prompt and route, and shows the preview on a compatible result", async () => {
    vi.mocked(client.ai.compose).mockResolvedValue(compatibleResult);
    const user = userEvent.setup();
    renderPanel();

    await user.type(screen.getByLabelText(/describe what you want/i), "a friendly hero section");
    await user.click(screen.getByRole("button", { name: /generate/i }));

    expect(client.ai.compose).toHaveBeenCalledWith(
      expect.objectContaining({ prompt: "a friendly hero section", route: "home" }),
    );
    await waitFor(() => {
      const frame = screen.getByTitle("AI compose preview") as HTMLIFrameElement;
      expect(frame.srcdoc).toBe("<h1>Welcome</h1>");
    });
  });

  it("renders the real compat diagnostics, not a generic error, for an incompatible result", async () => {
    vi.mocked(client.ai.compose).mockResolvedValue(incompatibleResult);
    const user = userEvent.setup();
    renderPanel();

    await user.type(screen.getByLabelText(/describe what you want/i), "something odd");
    await user.click(screen.getByRole("button", { name: /generate/i }));

    expect(await screen.findByText(/does-not-exist/)).toBeInTheDocument();
    expect(screen.queryByTitle("AI compose preview")).not.toBeInTheDocument();
  });

  it("shows a real error state when the compose request itself fails", async () => {
    vi.mocked(client.ai.compose).mockRejectedValue(new GlyphuxApiError("ai provider request failed", 502));
    const user = userEvent.setup();
    renderPanel();

    await user.type(screen.getByLabelText(/describe what you want/i), "anything");
    await user.click(screen.getByRole("button", { name: /generate/i }));

    expect(await screen.findByText("ai provider request failed")).toBeInTheDocument();
  });

  it("accept() saves then imports the fragment via the existing preset path, and calls onAccepted", async () => {
    vi.mocked(client.ai.compose).mockResolvedValue(compatibleResult);
    vi.mocked(client.presets.save).mockResolvedValue({
      ...compatibleResult.fragment,
      id: "abc123",
      created_at: "",
      updated_at: "",
    });
    vi.mocked(client.presets.import).mockResolvedValue({ compatible: true });
    const onAccepted = vi.fn();
    const user = userEvent.setup();
    renderPanel(onAccepted);

    await user.type(screen.getByLabelText(/describe what you want/i), "a friendly hero section");
    await user.click(screen.getByRole("button", { name: /generate/i }));
    await screen.findByTitle("AI compose preview");
    await user.click(screen.getByRole("button", { name: /accept/i }));

    await waitFor(() => {
      expect(client.presets.save).toHaveBeenCalledWith(compatibleResult.fragment);
      expect(client.presets.import).toHaveBeenCalledWith("abc123", "home");
      expect(onAccepted).toHaveBeenCalled();
    });
  });

  it("discard clears the proposal without calling presets.save", async () => {
    vi.mocked(client.ai.compose).mockResolvedValue(compatibleResult);
    const user = userEvent.setup();
    renderPanel();

    await user.type(screen.getByLabelText(/describe what you want/i), "a friendly hero section");
    await user.click(screen.getByRole("button", { name: /generate/i }));
    await screen.findByTitle("AI compose preview");
    await user.click(screen.getByRole("button", { name: /discard/i }));

    expect(screen.queryByTitle("AI compose preview")).not.toBeInTheDocument();
    expect(client.presets.save).not.toHaveBeenCalled();
  });
});
