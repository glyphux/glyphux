import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { ToastProvider } from "@/lib/toast-context";
import { DesignSystemPage } from "./DesignSystemPage";

// Seam: this is the component-preview surface itself (ticket DS item 4) —
// a smoke test that it renders every section without throwing, and that
// a handful of the components it's meant to showcase are actually present
// (not just section headings with empty bodies).
function renderPage() {
  return render(
    <ToastProvider>
      <DesignSystemPage />
    </ToastProvider>,
  );
}

describe("DesignSystemPage", () => {
  it("renders every catalog section", () => {
    renderPage();
    expect(screen.getByRole("heading", { name: "Design System" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Colors" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Typography" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Buttons" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Form controls" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Feedback" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Data display" })).toBeInTheDocument();
  });

  it("renders real interactive instances, not just labels", () => {
    renderPage();
    expect(screen.getByRole("button", { name: "Primary" })).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Textarea" })).toBeInTheDocument();
    expect(screen.getAllByRole("radio").length).toBeGreaterThan(0);
    expect(screen.getByRole("navigation", { name: /pagination/i })).toBeInTheDocument();
  });

  it("fires a real toast when the toast demo button is clicked", async () => {
    renderPage();
    const { default: userEvent } = await import("@testing-library/user-event");
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: /show toast/i }));
    expect(await screen.findByText(/this is a toast/i)).toBeInTheDocument();
  });
});
