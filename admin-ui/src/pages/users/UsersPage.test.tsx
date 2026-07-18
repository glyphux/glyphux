import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { UsersPage } from "./UsersPage";
import { AuthProvider } from "@/lib/auth-context";
import { ToastProvider } from "@/lib/toast-context";
import { client } from "@/lib/client";

// Seam: the users list page as an admin sees it — the account list, the
// active/deactivated status, and the deactivate confirmation flow —
// against a mocked SDK client (never a raw fetch). Role-change interaction
// (a Radix Select) is exercised at the SDK/API level in pkg/client,
// sdk-js, and internal/api's own tests; this file focuses on what the page
// renders and wires up.
vi.mock("@/lib/client", () => ({
  client: {
    token: "tok_admin",
    auth: { login: vi.fn(), logout: vi.fn(), me: vi.fn() },
    users: { list: vi.fn(), create: vi.fn(), updateRole: vi.fn(), deactivate: vi.fn(), reactivate: vi.fn() },
  },
  setToken: vi.fn(),
}));

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/users"]}>
      <ToastProvider>
        <AuthProvider>
          <UsersPage />
        </AuthProvider>
      </ToastProvider>
    </MemoryRouter>,
  );
}

describe("UsersPage", () => {
  beforeEach(() => {
    vi.mocked(client.auth.me).mockResolvedValue({
      id: 1,
      email: "admin@example.com",
      role: "admin",
      mfaEnabled: false,
      active: true,
    });
    vi.mocked(client.users.list).mockReset();
    vi.mocked(client.users.deactivate).mockReset();
    vi.mocked(client.users.reactivate).mockReset();
  });

  it("lists accounts with their active/deactivated status", async () => {
    vi.mocked(client.users.list).mockResolvedValue([
      { id: 1, email: "admin@example.com", role: "admin", mfaEnabled: false, active: true },
      { id: 2, email: "editor@example.com", role: "editor", mfaEnabled: false, active: false },
    ]);
    renderPage();

    expect(await screen.findByText("admin@example.com")).toBeInTheDocument();
    expect(screen.getByText("editor@example.com")).toBeInTheDocument();
    expect(screen.getByText("Active")).toBeInTheDocument();
    expect(screen.getByText("Deactivated")).toBeInTheDocument();
  });

  it("deactivates an account after confirming in the dialog", async () => {
    vi.mocked(client.users.list).mockResolvedValue([
      { id: 1, email: "admin@example.com", role: "admin", mfaEnabled: false, active: true },
      { id: 2, email: "editor@example.com", role: "editor", mfaEnabled: false, active: true },
    ]);
    vi.mocked(client.users.deactivate).mockResolvedValue(undefined);
    const user = userEvent.setup();
    renderPage();

    const editorRow = (await screen.findByText("editor@example.com")).closest("tr")!;
    await user.click(within(editorRow).getByRole("button", { name: /deactivate/i }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Deactivate" }));

    await waitFor(() => expect(client.users.deactivate).toHaveBeenCalledWith(2));
  });

  it("the current admin cannot deactivate their own account", async () => {
    vi.mocked(client.users.list).mockResolvedValue([
      { id: 1, email: "admin@example.com", role: "admin", mfaEnabled: false, active: true },
    ]);
    renderPage();

    await screen.findByText("admin@example.com");
    expect(screen.getByRole("button", { name: /deactivate/i })).toBeDisabled();
  });

  it("reactivates a deactivated account", async () => {
    vi.mocked(client.users.list).mockResolvedValue([
      { id: 1, email: "admin@example.com", role: "admin", mfaEnabled: false, active: true },
      { id: 2, email: "editor@example.com", role: "editor", mfaEnabled: false, active: false },
    ]);
    vi.mocked(client.users.reactivate).mockResolvedValue(undefined);
    const user = userEvent.setup();
    renderPage();

    await screen.findByText("editor@example.com");
    await user.click(screen.getByRole("button", { name: /reactivate/i }));

    await waitFor(() => expect(client.users.reactivate).toHaveBeenCalledWith(2));
  });
});
