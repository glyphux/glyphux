import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { GlyphuxApiError } from "@glyphux/sdk";
import { LoginPage } from "./LoginPage";
import { AuthProvider } from "@/lib/auth-context";
import { client } from "@/lib/client";

// Seam: LoginPage as rendered/interacted with by a user, through the
// AuthProvider it actually runs under in production. The SDK client is
// mocked at `@/lib/client` — the one seam every admin page is meant to
// call through — never at `fetch`, matching the "admin talks to the core
// only through sdk-js" rule.
vi.mock("@/lib/client", () => ({
  client: {
    token: undefined,
    auth: { login: vi.fn(), verifyMfa: vi.fn(), logout: vi.fn(), me: vi.fn() },
  },
  setToken: vi.fn(),
}));

function renderLoginPage() {
  return render(
    <MemoryRouter initialEntries={["/login"]}>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/" element={<div>HOME</div>} />
        </Routes>
      </AuthProvider>
    </MemoryRouter>,
  );
}

describe("LoginPage", () => {
  beforeEach(() => {
    vi.mocked(client.auth.login).mockReset();
    client.token = undefined;
  });

  it("renders email and password fields with a sign-in button", () => {
    renderLoginPage();
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /sign in/i })).toBeInTheDocument();
  });

  it("signs in and navigates to the app on valid credentials", async () => {
    vi.mocked(client.auth.login).mockResolvedValue({
      id: 1,
      email: "admin@example.com",
      role: "admin",
      mfaEnabled: false,
      active: true,
      token: "tok_123",
    });
    const user = userEvent.setup();
    renderLoginPage();

    await user.type(screen.getByLabelText(/email/i), "admin@example.com");
    await user.type(screen.getByLabelText(/password/i), "hunter2!!");
    await user.click(screen.getByRole("button", { name: /sign in/i }));

    await waitFor(() => expect(screen.getByText("HOME")).toBeInTheDocument());
    expect(client.auth.login).toHaveBeenCalledWith("admin@example.com", "hunter2!!");
  });

  it("shows the server's error message on failed login and stays on the login page", async () => {
    vi.mocked(client.auth.login).mockRejectedValue(new GlyphuxApiError("invalid email or password", 401));
    const user = userEvent.setup();
    renderLoginPage();

    await user.type(screen.getByLabelText(/email/i), "admin@example.com");
    await user.type(screen.getByLabelText(/password/i), "wrong");
    await user.click(screen.getByRole("button", { name: /sign in/i }));

    expect(await screen.findByText(/invalid email or password/i)).toBeInTheDocument();
    expect(screen.queryByText("HOME")).not.toBeInTheDocument();
  });

  it("shows an MFA code step when login() reports mfaRequired, then verifies it", async () => {
    vi.mocked(client.auth.login).mockResolvedValue({ mfaRequired: true, mfaToken: "challenge-token" });
    vi.mocked(client.auth.verifyMfa).mockResolvedValue({
      id: 1,
      email: "admin@example.com",
      role: "admin",
      mfaEnabled: true,
      active: true,
      token: "tok_123",
    });
    const user = userEvent.setup();
    renderLoginPage();

    await user.type(screen.getByLabelText(/email/i), "admin@example.com");
    await user.type(screen.getByLabelText(/password/i), "hunter2!!");
    await user.click(screen.getByRole("button", { name: /sign in/i }));

    expect(await screen.findByText(/two-factor verification/i)).toBeInTheDocument();
    expect(screen.queryByText("HOME")).not.toBeInTheDocument();

    await user.type(screen.getByLabelText(/authentication code/i), "123456");
    await user.click(screen.getByRole("button", { name: /verify/i }));

    await waitFor(() => expect(screen.getByText("HOME")).toBeInTheDocument());
    expect(client.auth.verifyMfa).toHaveBeenCalledWith("challenge-token", "123456");
  });
});
