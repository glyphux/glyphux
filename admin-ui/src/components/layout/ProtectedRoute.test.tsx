import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { ProtectedRoute } from "./ProtectedRoute";
import { AuthProvider } from "@/lib/auth-context";
import { client } from "@/lib/client";

// Seam: ProtectedRoute's redirect behavior as driven by the session state
// AuthProvider derives from the (mocked) SDK client — the slice brief's
// "redirect unauthenticated users to login."
vi.mock("@/lib/client", () => ({
  client: {
    token: undefined as string | undefined,
    auth: { login: vi.fn(), logout: vi.fn(), me: vi.fn() },
  },
  setToken: vi.fn(),
}));

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<div>LOGIN PAGE</div>} />
          <Route element={<ProtectedRoute />}>
            <Route path="/" element={<div>PROTECTED HOME</div>} />
          </Route>
        </Routes>
      </AuthProvider>
    </MemoryRouter>,
  );
}

describe("ProtectedRoute", () => {
  beforeEach(() => {
    client.token = undefined;
    vi.mocked(client.auth.me).mockReset();
  });

  it("redirects to /login when there is no session", async () => {
    renderAt("/");
    await waitFor(() => expect(screen.getByText("LOGIN PAGE")).toBeInTheDocument());
  });

  it("renders the protected content when a valid session exists", async () => {
    client.token = "tok_123";
    vi.mocked(client.auth.me).mockResolvedValue({ id: 1, email: "admin@example.com", role: "admin" });
    renderAt("/");
    await waitFor(() => expect(screen.getByText("PROTECTED HOME")).toBeInTheDocument());
  });
});
