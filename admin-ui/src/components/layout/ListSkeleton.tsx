import { Skeleton } from "@/components/ui/skeleton";

/** A real loading state for list views — row-shaped placeholders rather
 * than a bare spinner or blank screen (PRD §5.7 quality floor). */
export function ListSkeleton({ rows = 5 }: { rows?: number }) {
  return (
    <div className="flex flex-col gap-2" aria-hidden="true">
      {Array.from({ length: rows }).map((_, i) => (
        <Skeleton key={i} className="h-11 w-full" />
      ))}
    </div>
  );
}
