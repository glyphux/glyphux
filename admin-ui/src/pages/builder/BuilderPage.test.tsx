import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { GlyphuxApiError, type BlockDefinition, type Layout } from "@glyphux/sdk";
import { BuilderPage } from "./BuilderPage";
import { AuthProvider } from "@/lib/auth-context";
import { ToastProvider } from "@/lib/toast-context";
import { client } from "@/lib/client";

// Seam: the page as a whole — loading/error/empty states, and the
// load-then-save round trip through the public sdk-js client (blocks.list
// + layouts.get/save), matching every other admin-ui page's test seam
// (mocked @/lib/client, no raw fetch).
vi.mock("@/lib/client", () => ({
  client: {
    token: "tok_admin",
    auth: { login: vi.fn(), logout: vi.fn(), me: vi.fn() },
    blocks: { list: vi.fn() },
    layouts: { get: vi.fn(), save: vi.fn(), preview: vi.fn() },
    presets: { save: vi.fn(), import: vi.fn() },
    ai: { compose: vi.fn() },
  },
  setToken: vi.fn(),
}));

const registry: BlockDefinition[] = [
  { name: "heading", display_name: "Heading", props: { text: { type: "string", required: true } } },
];

function renderPage(initialPath = "/builder") {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <ToastProvider>
        <AuthProvider>
          <Routes>
            <Route path="/builder" element={<BuilderPage />} />
            <Route path="/builder/*" element={<BuilderPage />} />
          </Routes>
        </AuthProvider>
      </ToastProvider>
    </MemoryRouter>,
  );
}

describe("BuilderPage", () => {
  beforeEach(() => {
    vi.mocked(client.auth.me).mockResolvedValue({ id: 1, email: "admin@example.com", role: "admin", mfaEnabled: false, active: true });
    vi.mocked(client.blocks.list).mockReset();
    vi.mocked(client.layouts.get).mockReset();
    vi.mocked(client.layouts.save).mockReset();
    vi.mocked(client.presets.save).mockReset();
    vi.mocked(client.layouts.preview).mockReset();
    vi.mocked(client.layouts.preview).mockResolvedValue({ html: "<p>preview</p>", contentType: "text/html; charset=utf-8" });
  });

  it("starts from an empty layout when none has been saved for the route yet (404)", async () => {
    vi.mocked(client.blocks.list).mockResolvedValue(registry);
    vi.mocked(client.layouts.get).mockRejectedValue(new GlyphuxApiError("not found", 404));
    renderPage();

    expect(await screen.findByText("Heading")).toBeInTheDocument();
    expect(screen.getByText("No regions yet.")).toBeInTheDocument();
  });

  it("surfaces a real error state when loading fails for a reason other than 404", async () => {
    vi.mocked(client.blocks.list).mockResolvedValue(registry);
    vi.mocked(client.layouts.get).mockRejectedValue(new GlyphuxApiError("server exploded", 500));
    renderPage();

    expect(await screen.findByText("server exploded")).toBeInTheDocument();
  });

  it("loads an existing layout's regions/blocks into the canvas", async () => {
    const layout: Layout = {
      contract_version: "layout-composition/v1",
      regions: { main: { blocks: [{ type: "heading", props: { text: "Welcome" } }] } },
    };
    vi.mocked(client.blocks.list).mockResolvedValue(registry);
    vi.mocked(client.layouts.get).mockResolvedValue(layout);
    renderPage();

    expect((await screen.findAllByText("main")).length).toBeGreaterThan(0);
    expect(screen.getAllByText("Heading").length).toBeGreaterThan(0);
  });

  it("saves the current draft as the exact contract.Layout shape via LayoutsResource.save", async () => {
    const layout: Layout = {
      contract_version: "layout-composition/v1",
      regions: { main: { blocks: [{ type: "heading", props: { text: "Welcome" } }] } },
    };
    vi.mocked(client.blocks.list).mockResolvedValue(registry);
    vi.mocked(client.layouts.get).mockResolvedValue(layout);
    vi.mocked(client.layouts.save).mockResolvedValue(layout);
    const user = userEvent.setup();
    renderPage();

    await screen.findAllByText("main");
    await user.click(screen.getByRole("button", { name: /save "home"/i }));

    expect(client.layouts.save).toHaveBeenCalledWith("home", layout);
    expect(await screen.findByText("Layout saved")).toBeInTheDocument();
  });

  it("saves the current draft as a Composition Preset via PresetsResource.save", async () => {
    const layout: Layout = {
      contract_version: "layout-composition/v1",
      regions: { main: { blocks: [{ type: "heading", props: { text: "Welcome" } }] } },
    };
    vi.mocked(client.blocks.list).mockResolvedValue(registry);
    vi.mocked(client.layouts.get).mockResolvedValue(layout);
    vi.mocked(client.presets.save).mockResolvedValue({
      id: "abc123",
      created_at: "",
      updated_at: "",
      contract_version: "composition-preset/v1",
      name: "hero-section",
      layout,
      manifest: { requires_contract: "layout-composition/v1", blocks: ["heading"], slots: ["main"] },
    });
    const user = userEvent.setup();
    renderPage();

    await screen.findAllByText("main");
    await user.type(screen.getByLabelText("Preset name"), "hero-section");
    await user.click(screen.getByRole("button", { name: /save as preset/i }));

    expect(client.presets.save).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "hero-section",
        layout,
        manifest: expect.objectContaining({ blocks: ["heading"], slots: ["main"] }),
      }),
    );
    expect(await screen.findByText('Preset "hero-section" saved')).toBeInTheDocument();
  });

  it("disables the save-as-preset button until a name is entered", async () => {
    vi.mocked(client.blocks.list).mockResolvedValue(registry);
    vi.mocked(client.layouts.get).mockRejectedValue(new GlyphuxApiError("not found", 404));
    renderPage();

    await screen.findByText("Heading");
    expect(screen.getByRole("button", { name: /save as preset/i })).toBeDisabled();
  });

  it("loads the route named in the URL, not just the default", async () => {
    vi.mocked(client.blocks.list).mockResolvedValue(registry);
    vi.mocked(client.layouts.get).mockRejectedValue(new GlyphuxApiError("not found", 404));
    renderPage("/builder/blog/index");

    await screen.findByText("Heading");
    expect(client.layouts.get).toHaveBeenCalledWith("blog/index");
  });
});
