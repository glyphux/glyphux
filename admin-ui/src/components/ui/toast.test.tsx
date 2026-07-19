import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Toast } from "./toast";

// Seam: the presentational half of the toast/notification system — title,
// optional description, variant styling, and a manual dismiss control
// (WCAG 2.2.1 Timing Adjustable: an auto-dismissing notification must
// still let the user act on it before it disappears). The auto-dismiss
// timer itself lives in lib/toast-context.tsx and is exercised there.
describe("Toast", () => {
  it("renders the title and description", () => {
    render(<Toast title="Saved" description="Your changes were saved." variant="success" onDismiss={vi.fn()} />);
    expect(screen.getByText("Saved")).toBeInTheDocument();
    expect(screen.getByText("Your changes were saved.")).toBeInTheDocument();
  });

  it("calls onDismiss when the close button is activated", async () => {
    const onDismiss = vi.fn();
    const user = userEvent.setup();
    render(<Toast title="Deleted" variant="destructive" onDismiss={onDismiss} />);

    await user.click(screen.getByRole("button", { name: /dismiss/i }));
    expect(onDismiss).toHaveBeenCalled();
  });

  it("renders without a description when none is given", () => {
    render(<Toast title="Just a title" variant="default" onDismiss={vi.fn()} />);
    expect(screen.getByText("Just a title")).toBeInTheDocument();
  });
});
