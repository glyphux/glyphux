import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { ContentListPage } from "./ContentListPage";
import { AuthProvider } from "@/lib/auth-context";
import { ToastProvider } from "@/lib/toast-context";
import { client } from "@/lib/client";
import type { ContentItem, ContentType } from "@glyphux/sdk";

// Seam: this page's list view now paginates (this ticket's "pagination
// (for tables)" gap) — with more items than fit on one page, only a
// page's worth renders, and the ui/pagination.tsx controls step through
// the rest of them.
vi.mock("@/lib/client", () => ({
  client: {
    token: "tok_admin",
    auth: { login: vi.fn(), logout: vi.fn(), me: vi.fn() },
    contentTypes: { list: vi.fn(), define: vi.fn(), delete: vi.fn() },
    content: {
      list: vi.fn(),
      create: vi.fn(),
      get: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
      publish: vi.fn(),
      unpublish: vi.fn(),
    },
  },
  setToken: vi.fn(),
}));

vi.mock("@/lib/use-content-types", () => ({
  useContentTypes: () => ({
    types: {
      post: {
        fields: { title: { type: "string", required: true } },
      } satisfies ContentType,
    },
    loading: false,
  }),
}));

function makeItems(n: number): ContentItem[] {
  return Array.from({ length: n }, (_, i) => ({
    id: `item-${i + 1}`,
    type: "post",
    data: { title: `Post ${i + 1}` },
    status: "draft",
    version: 1,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  }));
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/content/post"]}>
      <ToastProvider>
        <AuthProvider>
          <Routes>
            <Route path="/content/:type" element={<ContentListPage />} />
          </Routes>
        </AuthProvider>
      </ToastProvider>
    </MemoryRouter>,
  );
}

describe("ContentListPage pagination", () => {
  beforeEach(() => {
    vi.mocked(client.auth.me).mockResolvedValue({ id: 1, email: "admin@example.com", role: "admin", mfaEnabled: false, active: true });
    vi.mocked(client.content.list).mockReset();
  });

  it("does not show pagination controls when everything fits on one page", async () => {
    vi.mocked(client.content.list).mockResolvedValue(makeItems(5));
    renderPage();

    await screen.findByText("Post 1");
    expect(screen.queryByRole("navigation", { name: /pagination/i })).not.toBeInTheDocument();
  });

  it("paginates a long list and steps to the next page", async () => {
    vi.mocked(client.content.list).mockResolvedValue(makeItems(25));
    const user = userEvent.setup();
    renderPage();

    await screen.findByText("Post 1");
    expect(screen.getByText("Post 10")).toBeInTheDocument();
    expect(screen.queryByText("Post 11")).not.toBeInTheDocument();
    expect(screen.getByText(/page 1 of 3/i)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /next/i }));

    expect(screen.getByText("Post 11")).toBeInTheDocument();
    expect(screen.queryByText("Post 1")).not.toBeInTheDocument();
  });
});
