import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Textarea } from "./textarea";

// Seam: renders as a real <textarea> with the shared input styling
// contract (aria-invalid state, disabled state, ref forwarding), and
// responds to typed input like every other form control in this library.
describe("Textarea", () => {
  it("renders a textarea element and forwards value/onChange", async () => {
    const onChange = vi.fn();
    render(<Textarea aria-label="Body" value="" onChange={onChange} />);
    const el = screen.getByRole("textbox", { name: "Body" });
    expect(el.tagName).toBe("TEXTAREA");

    const user = userEvent.setup();
    await user.type(el, "hello");
    expect(onChange).toHaveBeenCalled();
  });

  it("marks itself invalid via aria-invalid when passed", () => {
    render(<Textarea aria-label="Body" aria-invalid="true" readOnly value="x" />);
    expect(screen.getByRole("textbox", { name: "Body" })).toHaveAttribute("aria-invalid", "true");
  });

  it("is disabled when disabled prop is set", () => {
    render(<Textarea aria-label="Body" disabled readOnly value="x" />);
    expect(screen.getByRole("textbox", { name: "Body" })).toBeDisabled();
  });

  it("forwards a ref to the underlying textarea", () => {
    let ref: HTMLTextAreaElement | null = null;
    render(
      <Textarea
        aria-label="Body"
        readOnly
        value="x"
        ref={(el) => {
          ref = el;
        }}
      />,
    );
    expect(ref).toBeInstanceOf(HTMLTextAreaElement);
  });
});
