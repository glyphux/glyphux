import { ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/** A page-of-N controller for tables/lists that have grown past a single
 * screenful — the consumer owns `page` (1-indexed) and slices its own data;
 * this component is purely presentational + reports intent via
 * `onPageChange`, matching Tabs/Select's controlled-component convention
 * elsewhere in this library. Renders nothing for a single page, so
 * wiring it into a list unconditionally is safe. */
export function Pagination({
  page,
  totalPages,
  onPageChange,
  className,
}: {
  page: number;
  totalPages: number;
  onPageChange: (page: number) => void;
  className?: string;
}) {
  if (totalPages <= 1) return null;

  return (
    <nav aria-label="Pagination" className={cn("flex items-center justify-between gap-4", className)}>
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={() => onPageChange(page - 1)}
        disabled={page <= 1}
      >
        <ChevronLeft /> Previous
      </Button>
      <span className="text-muted-foreground text-small" aria-live="polite">
        Page {page} of {totalPages}
      </span>
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={() => onPageChange(page + 1)}
        disabled={page >= totalPages}
      >
        Next <ChevronRight />
      </Button>
    </nav>
  );
}
