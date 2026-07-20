import { Loader2 } from "lucide-react";
import { cn } from "@/lib/utils";

/** An inline loading spinner. `label` is announced to assistive tech via
 * aria-live so a loading list/detail view isn't silent for screen-reader
 * users (WCAG 2.1 AA, PRD §5.7). */
export function Spinner({ className, label = "Loading" }: { className?: string; label?: string }) {
  return (
    <span role="status" aria-live="polite" className="inline-flex items-center gap-2">
      <Loader2 className={cn("text-muted-foreground size-4 animate-spin", className)} aria-hidden="true" />
      <span className="sr-only">{label}</span>
    </span>
  );
}
