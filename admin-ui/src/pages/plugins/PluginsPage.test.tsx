import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { PluginsPage } from "./PluginsPage";
import { AuthProvider } from "@/lib/auth-context";
import { ToastProvider } from "@/lib/toast-context";
import { client } from "@/lib/client";

// Seam: the plugin consent screen as an admin sees it — pending requests
// with their full requested surface, the Approve / Deny / Apply-selection
// actions, and the status table — against a mocked SDK client (never a raw
// fetch). The decision semantics themselves (grant-subset validation,
// fingerprint staleness, persistence across restarts) are exercised at the
// sdk-js / internal/api / daemon levels; this file focuses on what the
// page renders and wires up.
vi.mock("@/lib/client", () => ({
  client: {
    token: "tok_admin",
    auth: { login: vi.fn(), logout: vi.fn(), me: vi.fn() },
    plugins: { list: vi.fn(), consentRequests: vi.fn(), decide: vi.fn() },
  },
  setToken: vi.fn(),
}));

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/plugins"]}>
      <ToastProvider>
        <AuthProvider>
          <PluginsPage />
        </AuthProvider>
      </ToastProvider>
    </MemoryRouter>,
  );
}

const commerceRequest = {
  name: "commerce",
  version: "1.0.0",
  fingerprint: "ab".repeat(32),
  api: [
    { capability: "payments", scopes: ["charge", "refund"] },
    { capability: "content", scopes: ["read", "write"] },
  ],
  permissions: [],
};

describe("PluginsPage", () => {
  beforeEach(() => {
    vi.mocked(client.auth.me).mockResolvedValue({
      id: 1,
      email: "admin@example.com",
      role: "admin",
      mfaEnabled: false,
      active: true,
    });
    vi.mocked(client.plugins.list).mockReset();
    vi.mocked(client.plugins.consentRequests).mockReset();
    vi.mocked(client.plugins.decide).mockReset();
  });

  it("shows pending requests with their full requested surface and checkboxes pre-checked", async () => {
    vi.mocked(client.plugins.consentRequests).mockResolvedValue([commerceRequest]);
    vi.mocked(client.plugins.list).mockResolvedValue([
      { ...commerceRequest, status: "undecided", requested: { api: commerceRequest.api, permissions: [] } },
    ]);
    renderPage();

    expect(await screen.findByRole("heading", { name: "commerce" })).toBeInTheDocument();
    expect(screen.getByText("payments")).toBeInTheDocument();
    // Pre-checked scope labels: charge, refund, read, write.
    const checkboxes = within(screen.getByLabelText("Consent for commerce")).getAllByRole("checkbox");
    expect(checkboxes).toHaveLength(4);
    for (const box of checkboxes) {
      expect(box).toBeChecked();
    }
  });

  it("Apply selection sends a partial grant of exactly the checked subset", async () => {
    vi.mocked(client.plugins.consentRequests).mockResolvedValue([commerceRequest]);
    vi.mocked(client.plugins.list).mockResolvedValue([
      { ...commerceRequest, status: "undecided", requested: { api: commerceRequest.api, permissions: [] } },
    ]);
    vi.mocked(client.plugins.decide).mockResolvedValue({
      id: 1,
      plugin_name: "commerce",
      plugin_version: "1.0.0",
      fingerprint: commerceRequest.fingerprint,
      status: "partial",
      granted_api: [{ capability: "content", scopes: ["read"] }],
      granted_permissions: [],
      decided_by: 1,
      decided_at: "2026-08-01T00:00:00Z",
    });
    const user = userEvent.setup();
    renderPage();

    const section = await screen.findByLabelText("Consent for commerce");
    // Uncheck everything except content:read, then apply.
    const boxes = within(section).getAllByRole("checkbox");
    await user.click(boxes[0]); // payments:charge
    await user.click(boxes[1]); // payments:refund
    await user.click(boxes[3]); // content:write

    await user.click(within(section).getByRole("button", { name: "Apply selection" }));

    await waitFor(() => expect(client.plugins.decide).toHaveBeenCalledTimes(1));
    expect(client.plugins.decide).toHaveBeenCalledWith("commerce", {
      decision: "partial",
      granted_api: [{ capability: "content", scopes: ["read"] }],
      granted_permissions: [],
    });
  });

  it("Approve all sends an approved decision with the full request", async () => {
    vi.mocked(client.plugins.consentRequests).mockResolvedValue([commerceRequest]);
    vi.mocked(client.plugins.list).mockResolvedValue([
      { ...commerceRequest, status: "undecided", requested: { api: commerceRequest.api, permissions: [] } },
    ]);
    const user = userEvent.setup();
    renderPage();

    const section = await screen.findByLabelText("Consent for commerce");
    await user.click(within(section).getByRole("button", { name: "Approve all" }));

    await waitFor(() => expect(client.plugins.decide).toHaveBeenCalledTimes(1));
    expect(client.plugins.decide).toHaveBeenCalledWith("commerce", {
      decision: "approved",
      granted_api: commerceRequest.api,
      granted_permissions: [],
    });
  });

  it("Deny sends a denied decision granting nothing", async () => {
    vi.mocked(client.plugins.consentRequests).mockResolvedValue([commerceRequest]);
    vi.mocked(client.plugins.list).mockResolvedValue([
      { ...commerceRequest, status: "undecided", requested: { api: commerceRequest.api, permissions: [] } },
    ]);
    const user = userEvent.setup();
    renderPage();

    const section = await screen.findByLabelText("Consent for commerce");
    await user.click(within(section).getByRole("button", { name: "Deny" }));

    await waitFor(() => expect(client.plugins.decide).toHaveBeenCalledTimes(1));
    expect(client.plugins.decide).toHaveBeenCalledWith("commerce", { decision: "denied" });
  });

  it("after a decision the page reloads and the plugin leaves the pending section", async () => {
    // First call (initial mount) sees an undecided commerce; every later
    // call (the reload after the decision) sees it decided-but-denied.
    vi.mocked(client.plugins.consentRequests)
      .mockResolvedValueOnce([commerceRequest])
      .mockResolvedValue([]);
    vi.mocked(client.plugins.list)
      .mockResolvedValueOnce([
        { ...commerceRequest, status: "undecided", requested: { api: commerceRequest.api, permissions: [] } },
      ])
      .mockResolvedValue([
        { ...commerceRequest, status: "denied", requested: { api: commerceRequest.api, permissions: [] } },
      ]);
    const user = userEvent.setup();
    renderPage();

    const section = await screen.findByLabelText("Consent for commerce");
    await user.click(within(section).getByRole("button", { name: "Deny" }));
    await waitFor(() => expect(client.plugins.decide).toHaveBeenCalledTimes(1));

    expect(await screen.findByText("No pending consent requests")).toBeInTheDocument();
    // The status table still shows the plugin, now as Denied.
    expect(within(await screen.findByRole("table")).getByText("Denied")).toBeInTheDocument();
  });

  it("hides the page entirely for a non-admin", async () => {
    vi.mocked(client.auth.me).mockResolvedValue({
      id: 2,
      email: "editor@example.com",
      role: "editor",
      mfaEnabled: false,
      active: true,
    });
    renderPage();

    expect(
      await screen.findByText("You don't have permission to manage plugins. Ask an admin for access."),
    ).toBeInTheDocument();
    expect(client.plugins.list).not.toHaveBeenCalled();
  });
});
