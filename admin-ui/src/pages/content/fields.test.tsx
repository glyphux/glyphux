import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ContentTypeField, MediaItem } from "@glyphux/sdk";
import { FieldControl } from "./fields";
import { ToastProvider } from "@/lib/toast-context";
import { client } from "@/lib/client";

// Seam: FieldControl's "media" case is the one place both Layer-1
// content-type fields (this file) and the Layer-2 builder's PropsEditor
// share a FieldMedia control — fixing it here benefits both, per Ticket
// P4.9. Behavior under test: it shows the currently selected asset (not a
// bare id), opens the real media-library picker to change it, and clears
// through onChange(undefined) rather than the picker.
vi.mock("@/lib/client", () => ({
  client: {
    media: { list: vi.fn(), upload: vi.fn(), get: vi.fn() },
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

const mediaField: ContentTypeField = { type: "media" };

function renderField(value: unknown, onChange = vi.fn()) {
  render(
    <ToastProvider>
      <FieldControl id="cover" field={mediaField} value={value} onChange={onChange} />
    </ToastProvider>,
  );
  return { onChange };
}

describe("FieldControl media field", () => {
  beforeEach(() => {
    vi.mocked(client.media.list).mockReset();
    vi.mocked(client.media.get).mockReset();
  });

  it("shows no selection and a Choose button when value is empty", () => {
    renderField(undefined);
    expect(screen.getByText("No media selected")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /choose media/i })).toBeInTheDocument();
  });

  it("resolves and displays the selected item's filename when value is an id", async () => {
    vi.mocked(client.media.get).mockResolvedValue(item({ id: "m1", filename: "hero.png" }));
    renderField("m1");

    await waitFor(() => expect(client.media.get).toHaveBeenCalledWith("m1"));
    await waitFor(() => expect(screen.getByText("hero.png")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: /change/i })).toBeInTheDocument();
  });

  it("opens the media picker and selecting an asset calls onChange with its id", async () => {
    vi.mocked(client.media.list).mockResolvedValue([item({ id: "m2", filename: "banner.png" })]);
    const user = userEvent.setup();
    const { onChange } = renderField(undefined);

    await user.click(screen.getByRole("button", { name: /choose media/i }));
    await waitFor(() => expect(screen.getByText("banner.png")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /select banner.png/i }));

    expect(onChange).toHaveBeenCalledWith("m2");
  });

  it("clearing calls onChange(undefined)", async () => {
    vi.mocked(client.media.get).mockResolvedValue(item({ id: "m1", filename: "hero.png" }));
    const user = userEvent.setup();
    const { onChange } = renderField("m1");

    await waitFor(() => expect(screen.getByText("hero.png")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /clear/i }));

    expect(onChange).toHaveBeenCalledWith(undefined);
  });
});
