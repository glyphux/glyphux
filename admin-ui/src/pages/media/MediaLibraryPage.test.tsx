import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import type { MediaItem } from "@glyphux/sdk";
import { MediaLibraryPage } from "./MediaLibraryPage";
import { AuthProvider } from "@/lib/auth-context";
import { ToastProvider } from "@/lib/toast-context";
import { client } from "@/lib/client";

// Seam: the media library page as a user sees it — grid, search, the
// detail/preview dialog, and its inline edit form — against a mocked SDK
// client (never a raw fetch), matching the other admin page test seams.
vi.mock("@/lib/client", () => ({
  client: {
    token: "tok_admin",
    auth: { login: vi.fn(), logout: vi.fn(), me: vi.fn() },
    media: { list: vi.fn(), upload: vi.fn(), delete: vi.fn(), updateMetadata: vi.fn() },
  },
  setToken: vi.fn(),
}));

function item(overrides: Partial<MediaItem> = {}): MediaItem {
  return {
    id: "m1",
    filename: "cover.png",
    mime_type: "image/png",
    size_bytes: 2048,
    width: 800,
    height: 600,
    alt_text: "",
    tags: [],
    source: "",
    attribution: "",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/media"]}>
      <ToastProvider>
        <AuthProvider>
          <MediaLibraryPage />
        </AuthProvider>
      </ToastProvider>
    </MemoryRouter>,
  );
}

describe("MediaLibraryPage", () => {
  beforeEach(() => {
    vi.mocked(client.auth.me).mockResolvedValue({ id: 1, email: "admin@example.com", role: "admin" });
    vi.mocked(client.media.list).mockReset();
    vi.mocked(client.media.delete).mockReset();
    vi.mocked(client.media.updateMetadata).mockReset();
  });

  it("shows an empty state when there is no media", async () => {
    vi.mocked(client.media.list).mockResolvedValue([]);
    renderPage();
    expect(await screen.findByText(/no media yet/i)).toBeInTheDocument();
  });

  it("lists uploaded items with their tags", async () => {
    vi.mocked(client.media.list).mockResolvedValue([item({ tags: ["hero", "stock"] })]);
    renderPage();
    expect(await screen.findByText("cover.png")).toBeInTheDocument();
    expect(screen.getByText("hero")).toBeInTheDocument();
    expect(screen.getByText("stock")).toBeInTheDocument();
  });

  it("filters the grid by the search input across filename, alt text, and tags", async () => {
    vi.mocked(client.media.list).mockResolvedValue([
      item({ id: "m1", filename: "cover.png", tags: ["hero"] }),
      item({ id: "m2", filename: "logo.svg", mime_type: "image/svg+xml", alt_text: "brand logo" }),
    ]);
    const user = userEvent.setup();
    renderPage();

    await screen.findByText("cover.png");
    expect(screen.getByText("logo.svg")).toBeInTheDocument();

    await user.type(screen.getByLabelText(/search media/i), "hero");
    expect(screen.getByText("cover.png")).toBeInTheDocument();
    expect(screen.queryByText("logo.svg")).not.toBeInTheDocument();

    await user.clear(screen.getByLabelText(/search media/i));
    await user.type(screen.getByLabelText(/search media/i), "brand");
    expect(screen.getByText("logo.svg")).toBeInTheDocument();
    expect(screen.queryByText("cover.png")).not.toBeInTheDocument();
  });

  it("shows a no-matches state when the search query matches nothing", async () => {
    vi.mocked(client.media.list).mockResolvedValue([item()]);
    const user = userEvent.setup();
    renderPage();

    await screen.findByText("cover.png");
    await user.type(screen.getByLabelText(/search media/i), "nonexistent");
    expect(await screen.findByText(/no matches/i)).toBeInTheDocument();
  });

  it("opens a preview dialog with a larger image on click, and edits metadata", async () => {
    const uploaded = item({ tags: ["hero"], alt_text: "a cover" });
    vi.mocked(client.media.list).mockResolvedValue([uploaded]);
    vi.mocked(client.media.updateMetadata).mockResolvedValue({
      ...uploaded,
      alt_text: "updated alt",
      tags: ["hero", "new-tag"],
      source: "https://example.com/photo",
      attribution: "Photo by Jane Doe",
    });
    const user = userEvent.setup();
    renderPage();

    await screen.findByText("cover.png");
    await user.click(screen.getByRole("button", { name: /view cover.png/i }));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("cover.png")).toBeInTheDocument();
    // The preview image request is the full-size (unresized) file route,
    // not the grid's w=240&h=240 thumbnail.
    const img = within(dialog).getByAltText("a cover") as HTMLImageElement;
    expect(img.src).toContain("/api/v0/media/m1/file");
    expect(img.src).not.toContain("w=240");

    const altInput = within(dialog).getByLabelText(/alt text/i);
    await user.clear(altInput);
    await user.type(altInput, "updated alt");
    const tagsInput = within(dialog).getByLabelText(/tags/i);
    await user.clear(tagsInput);
    await user.type(tagsInput, "hero, new-tag");
    const sourceInput = within(dialog).getByLabelText(/source/i);
    await user.type(sourceInput, "https://example.com/photo");
    const attributionInput = within(dialog).getByLabelText(/attribution/i);
    await user.type(attributionInput, "Photo by Jane Doe");

    await user.click(within(dialog).getByRole("button", { name: /save/i }));

    await waitFor(() =>
      expect(client.media.updateMetadata).toHaveBeenCalledWith("m1", {
        alt_text: "updated alt",
        tags: ["hero", "new-tag"],
        source: "https://example.com/photo",
        attribution: "Photo by Jane Doe",
      }),
    );
  });

  it("deletes a media item after confirming in the dialog", async () => {
    vi.mocked(client.media.list).mockResolvedValue([item()]);
    vi.mocked(client.media.delete).mockResolvedValue(undefined);
    const user = userEvent.setup();
    renderPage();

    await screen.findByText("cover.png");
    await user.click(screen.getByRole("button", { name: /delete/i }));
    await user.click(await screen.findByRole("button", { name: "Delete" }));

    await waitFor(() => expect(client.media.delete).toHaveBeenCalledWith("m1"));
  });
});
