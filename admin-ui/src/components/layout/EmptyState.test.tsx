import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Image } from "lucide-react";
import { EmptyState } from "./EmptyState";
import { Button } from "@/components/ui/button";

// Seam: the "nothing here yet" state used across list/detail views (PRD
// §5.7's "real ... empty states ..., not blank screens") — renders the
// title always, the description/icon/action only when given, and the
// action (typically a primary CTA) is a real interactive element.
describe("EmptyState", () => {
  it("renders the title", () => {
    render(<EmptyState title="No media yet" />);
    expect(screen.getByText("No media yet")).toBeInTheDocument();
  });

  it("renders an optional description and icon", () => {
    render(<EmptyState title="No media yet" description="Upload your first asset." icon={<Image />} />);
    expect(screen.getByText("Upload your first asset.")).toBeInTheDocument();
  });

  it("renders an action and it responds to interaction", async () => {
    const onClick = vi.fn();
    const user = userEvent.setup();
    render(<EmptyState title="No media yet" action={<Button onClick={onClick}>Upload</Button>} />);

    await user.click(screen.getByRole("button", { name: "Upload" }));
    expect(onClick).toHaveBeenCalled();
  });
});
