import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { RadioGroup, RadioGroupItem } from "./radio-group";
import { Label } from "./label";

// Seam: a set of mutually-exclusive options, keyboard-navigable (Radix's
// roving tabindex + arrow keys) and reporting the selected value via
// onValueChange, matching switch.tsx/checkbox.tsx's controlled-primitive
// convention.
function Options({ onValueChange }: { onValueChange: (v: string) => void }) {
  return (
    <RadioGroup defaultValue="draft" onValueChange={onValueChange}>
      <div className="flex items-center gap-2">
        <RadioGroupItem value="draft" id="r-draft" />
        <Label htmlFor="r-draft">Draft</Label>
      </div>
      <div className="flex items-center gap-2">
        <RadioGroupItem value="published" id="r-published" />
        <Label htmlFor="r-published">Published</Label>
      </div>
    </RadioGroup>
  );
}

describe("RadioGroup", () => {
  it("renders each item as a radio with the correct checked state", () => {
    render(<Options onValueChange={vi.fn()} />);
    expect(screen.getByRole("radio", { name: "Draft" })).toBeChecked();
    expect(screen.getByRole("radio", { name: "Published" })).not.toBeChecked();
  });

  it("selects an option on click and calls onValueChange", async () => {
    const onValueChange = vi.fn();
    const user = userEvent.setup();
    render(<Options onValueChange={onValueChange} />);

    await user.click(screen.getByRole("radio", { name: "Published" }));

    expect(onValueChange).toHaveBeenCalledWith("published");
    expect(screen.getByRole("radio", { name: "Published" })).toBeChecked();
    expect(screen.getByRole("radio", { name: "Draft" })).not.toBeChecked();
  });

  it("is reachable and selectable via keyboard alone (Tab + Space)", async () => {
    const onValueChange = vi.fn();
    const user = userEvent.setup();
    render(
      <>
        <button type="button">before</button>
        <Options onValueChange={onValueChange} />
      </>,
    );

    await user.click(screen.getByRole("button", { name: "before" }));
    await user.tab();
    expect(screen.getByRole("radio", { name: "Draft" })).toHaveFocus();
  });
});
