import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { MediaItem } from "@glyphux/sdk";
import { MediaPicker } from "./MediaPicker";
import { ToastProvider } from "@/lib/toast-context";
import { client } from "@/lib/client";

// Seam: the in-builder/content-field media picker as a user drives it — a
// dialog listing the existing media library (via the same sdk-js client
// the standalone Media page already uses, never a raw fetch), searchable,
// with an upload path that reuses client.media.upload, and a select action
// that hands the chosen item back to the caller (not just closes the
// dialog) so FieldControl's media case can store its id.
vi.mock("@/lib/client", () => ({
  client: {
    media: { list: vi.fn(), upload: vi.fn() },
  },
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

function renderPicker(props: Partial<React.ComponentProps<typeof MediaPicker>> = {}) {
  const onOpenChange = vi.fn();
  const onSelect = vi.fn();
  render(
    <ToastProvider>
      <MediaPicker open onOpenChange={onOpenChange} onSelect={onSelect} {...props} />
    </ToastProvider>,
  );
  return { onOpenChange, onSelect };
}

describe("MediaPicker", () => {
  beforeEach(() => {
    vi.mocked(client.media.list).mockReset();
    vi.mocked(client.media.upload).mockReset();
  });

  it("lists the existing media library's items with thumbnails when opened", async () => {
    vi.mocked(client.media.list).mockResolvedValue([item({ id: "a", filename: "one.png" }), item({ id: "b", filename: "two.png" })]);
    renderPicker();

    await waitFor(() => expect(screen.getByText("one.png")).toBeInTheDocument());
    expect(screen.getByText("two.png")).toBeInTheDocument();
    expect(screen.getAllByRole("img")).toHaveLength(2);
  });

  it("does not fetch the library while closed", () => {
    renderPicker({ open: false });
    expect(client.media.list).not.toHaveBeenCalled();
  });

  it("filters items by the search box", async () => {
    vi.mocked(client.media.list).mockResolvedValue([
      item({ id: "a", filename: "sunset.png" }),
      item({ id: "b", filename: "mountain.png" }),
    ]);
    const user = userEvent.setup();
    renderPicker();

    await waitFor(() => expect(screen.getByText("sunset.png")).toBeInTheDocument());
    await user.type(screen.getByLabelText(/search media/i), "sunset");

    expect(screen.getByText("sunset.png")).toBeInTheDocument();
    expect(screen.queryByText("mountain.png")).not.toBeInTheDocument();
  });

  it("selecting an item calls onSelect with that item and closes the dialog", async () => {
    vi.mocked(client.media.list).mockResolvedValue([item({ id: "a", filename: "one.png" })]);
    const user = userEvent.setup();
    const { onSelect, onOpenChange } = renderPicker();

    await waitFor(() => expect(screen.getByText("one.png")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /select one.png/i }));

    expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({ id: "a", filename: "one.png" }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("uploading a new file calls the same sdk upload method the Media page uses, then lists it", async () => {
    vi.mocked(client.media.list)
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([item({ id: "new", filename: "fresh.png" })]);
    vi.mocked(client.media.upload).mockResolvedValue(item({ id: "new", filename: "fresh.png" }));
    const user = userEvent.setup();
    renderPicker();

    await waitFor(() => expect(client.media.list).toHaveBeenCalledTimes(1));
    const file = new File(["x"], "fresh.png", { type: "image/png" });
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await user.upload(input, file);

    await waitFor(() => expect(client.media.upload).toHaveBeenCalledWith(file, "fresh.png"));
    await waitFor(() => expect(screen.getByText("fresh.png")).toBeInTheDocument());
  });

  it("shows an empty state when the library has no items", async () => {
    vi.mocked(client.media.list).mockResolvedValue([]);
    renderPicker();

    await waitFor(() => expect(screen.getByText(/no media yet/i)).toBeInTheDocument());
  });
});
