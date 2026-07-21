import { describe, expect, it, vi, afterEach } from "vitest";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ToastProvider, useToast } from "./toast-context";

function Trigger() {
  const { toast } = useToast();
  return (
    <button type="button" onClick={() => toast({ title: "Saved", description: "All good", variant: "success" })}>
      fire
    </button>
  );
}

describe("ToastProvider/useToast", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows a toast when triggered and announces it via a live region", async () => {
    const user = userEvent.setup();
    render(
      <ToastProvider>
        <Trigger />
      </ToastProvider>,
    );

    await user.click(screen.getByRole("button", { name: "fire" }));

    expect(await screen.findByText("Saved")).toBeInTheDocument();
    expect(screen.getByText("All good")).toBeInTheDocument();
    expect(screen.getByRole("status")).toBeInTheDocument();
  });

  it("auto-dismisses a toast after the timeout", async () => {
    vi.useFakeTimers();
    render(
      <ToastProvider>
        <Trigger />
      </ToastProvider>,
    );

    act(() => {
      screen.getByRole("button", { name: "fire" }).click();
    });
    expect(screen.getByText("Saved")).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(5000);
    });
    expect(screen.queryByText("Saved")).not.toBeInTheDocument();
  });

  it("dismisses a toast immediately when its close button is clicked", async () => {
    const user = userEvent.setup();
    render(
      <ToastProvider>
        <Trigger />
      </ToastProvider>,
    );

    await user.click(screen.getByRole("button", { name: "fire" }));
    await screen.findByText("Saved");

    await user.click(screen.getByRole("button", { name: /dismiss/i }));
    expect(screen.queryByText("Saved")).not.toBeInTheDocument();
  });
});
