import type { HTMLAttributes } from "react";
import { cn } from "@/lib/utils";

/** A loading placeholder — used everywhere a list/detail view is fetching,
 * per PRD §5.7's "real ... loading states for every list/detail view, not
 * blank screens". Respects reduced-motion globally via index.css. */
export function Skeleton({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("bg-muted animate-pulse rounded-md", className)} {...props} />;
}
