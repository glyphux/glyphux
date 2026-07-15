import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { ContentTypesListPage } from "./ContentTypesListPage";
import { AuthProvider } from "@/lib/auth-context";
import { ToastProvider } from "@/lib/toast-context";
import { client } from "@/lib/client";

// Seam: the content-types list page as a user sees it — empty state,
// populated list, and the delete confirmation flow — against a mocked SDK
// client (never a raw fetch).
vi.mock("@/lib/client", () => ({
  client: {
    token: "tok_admin",
    auth: { login: vi.fn(), logout: vi.fn(), me: vi.fn() },
    contentTypes: { list: vi.fn(), define: vi.fn(), delete: vi.fn() },
  },
  setToken: vi.fn(),
}));

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/content-types"]}>
      <ToastProvider>
        <AuthProvider>
          <ContentTypesListPage />
        </AuthProvider>
      </ToastProvider>
    </MemoryRouter>,
  );
}

describe("ContentTypesListPage", () => {
  beforeEach(() => {
    vi.mocked(client.auth.me).mockResolvedValue({ id: 1, email: "admin@example.com", role: "admin" });
    vi.mocked(client.contentTypes.list).mockReset();
    vi.mocked(client.contentTypes.delete).mockReset();
  });

  it("shows an empty state when there are no content types", async () => {
    vi.mocked(client.contentTypes.list).mockResolvedValue({});
    renderPage();
    expect(await screen.findByText(/no content types yet/i)).toBeInTheDocument();
  });

  it("lists declared content types and their fields", async () => {
    vi.mocked(client.contentTypes.list).mockResolvedValue({
      post: { fields: { title: { type: "string", required: true } } },
    });
    renderPage();
    expect(await screen.findByText("post")).toBeInTheDocument();
    expect(screen.getByText(/title: string/)).toBeInTheDocument();
  });

  it("deletes a content type after confirming in the dialog", async () => {
    vi.mocked(client.contentTypes.list).mockResolvedValue({
      post: { fields: { title: { type: "string" } } },
    });
    vi.mocked(client.contentTypes.delete).mockResolvedValue(undefined);
    const user = userEvent.setup();
    renderPage();

    await screen.findByText("post");
    await user.click(screen.getByRole("button", { name: /delete post/i }));
    await user.click(await screen.findByRole("button", { name: "Delete" }));

    await waitFor(() => expect(client.contentTypes.delete).toHaveBeenCalledWith("post"));
  });
});
