import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

/** A real empty state — not a blank screen — for any list/detail view with
 * nothing in it yet (PRD §5.7 quality floor). */
export function EmptyState({
  icon,
  title,
  description,
  action,
  className,
}: {
  icon?: ReactNode;
  title: string;
  description?: string;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "border-border flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed px-6 py-16 text-center",
        className,
      )}
    >
      {icon && <div className="text-muted-foreground [&_svg]:size-8">{icon}</div>}
      <div className="flex flex-col gap-1">
        <p className="text-h2 font-semibold">{title}</p>
        {description && <p className="text-muted-foreground text-body max-w-sm">{description}</p>}
      </div>
      {action}
    </div>
  );
}
