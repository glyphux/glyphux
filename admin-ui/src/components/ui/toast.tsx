import { X } from "lucide-react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const toastVariants = cva("animate-slide-in pointer-events-auto w-full max-w-sm rounded-lg border p-4 shadow-lg sm:w-96", {
  variants: {
    variant: {
      default: "border-border bg-card text-card-foreground",
      success: "border-success/30 bg-success/10 text-foreground",
      destructive: "border-destructive/30 bg-destructive/10 text-foreground",
    },
  },
  defaultVariants: { variant: "default" },
});

export type ToastVariant = NonNullable<VariantProps<typeof toastVariants>["variant"]>;

/** One notification — the presentational half of the toast system.
 * `lib/toast-context.tsx` owns the queue/timer and renders these into an
 * `aria-live` region so screen-reader users hear each toast without
 * needing focus to move; the close button here covers WCAG 2.2.1 (a timed
 * notification must still be dismissible/actionable on demand, not only
 * on its own timer). */
export function Toast({
  title,
  description,
  variant = "default",
  onDismiss,
  className,
}: {
  title: string;
  description?: string;
  variant?: ToastVariant;
  onDismiss: () => void;
  className?: string;
}) {
  return (
    <div className={cn(toastVariants({ variant }), "relative", className)}>
      <button
        type="button"
        onClick={onDismiss}
        aria-label="Dismiss notification"
        className="absolute top-3 right-3 rounded-sm opacity-70 transition-opacity hover:opacity-100 focus-visible:opacity-100"
      >
        <X className="size-3.5" />
      </button>
      <p className="text-body pr-5 font-medium">{title}</p>
      {description && <p className="text-muted-foreground text-small mt-1">{description}</p>}
    </div>
  );
}
