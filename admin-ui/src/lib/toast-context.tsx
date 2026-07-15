import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from "react";
import { cn } from "./utils";

export type ToastVariant = "default" | "success" | "destructive";

interface Toast {
  id: number;
  title: string;
  description?: string;
  variant: ToastVariant;
}

interface ToastState {
  toast: (t: Omit<Toast, "id">) => void;
}

const ToastContext = createContext<ToastState | undefined>(undefined);

const VARIANT_CLASSES: Record<ToastVariant, string> = {
  default: "border-border bg-card text-card-foreground",
  success: "border-success/30 bg-success/10 text-foreground",
  destructive: "border-destructive/30 bg-destructive/10 text-foreground",
};

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const nextId = useRef(0);

  const toast = useCallback((t: Omit<Toast, "id">) => {
    const id = nextId.current++;
    setToasts((prev) => [...prev, { ...t, id }]);
    window.setTimeout(() => {
      setToasts((prev) => prev.filter((x) => x.id !== id));
    }, 5000);
  }, []);

  return (
    <ToastContext.Provider value={{ toast }}>
      {children}
      {/* aria-live region: every toast is announced to assistive tech
       * without needing focus to move (WCAG 2.1 AA, PRD §5.7). */}
      <div
        role="status"
        aria-live="polite"
        className="pointer-events-none fixed inset-x-0 bottom-0 z-50 flex flex-col items-end gap-2 p-4 sm:bottom-4 sm:right-4 sm:left-auto"
      >
        {toasts.map((t) => (
          <div
            key={t.id}
            className={cn(
              "animate-slide-in pointer-events-auto w-full max-w-sm rounded-lg border p-4 shadow-lg sm:w-96",
              VARIANT_CLASSES[t.variant],
            )}
          >
            <p className="text-body font-medium">{t.title}</p>
            {t.description && <p className="text-muted-foreground text-small mt-1">{t.description}</p>}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast(): ToastState {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error("useToast must be used within a ToastProvider");
  return ctx;
}
