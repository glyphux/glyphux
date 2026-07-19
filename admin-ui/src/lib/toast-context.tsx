import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from "react";
import { Toast, type ToastVariant } from "@/components/ui/toast";

export type { ToastVariant };

interface ToastRecord {
  id: number;
  title: string;
  description?: string;
  variant: ToastVariant;
}

interface ToastState {
  toast: (t: Omit<ToastRecord, "id">) => void;
}

const ToastContext = createContext<ToastState | undefined>(undefined);

const AUTO_DISMISS_MS = 5000;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastRecord[]>([]);
  const nextId = useRef(0);

  const dismiss = useCallback((id: number) => {
    setToasts((prev) => prev.filter((x) => x.id !== id));
  }, []);

  const toast = useCallback(
    (t: Omit<ToastRecord, "id">) => {
      const id = nextId.current++;
      setToasts((prev) => [...prev, { ...t, id }]);
      window.setTimeout(() => dismiss(id), AUTO_DISMISS_MS);
    },
    [dismiss],
  );

  return (
    <ToastContext.Provider value={{ toast }}>
      {children}
      {/* aria-live region: every toast is announced to assistive tech
       * without needing focus to move (WCAG 2.1 AA, PRD §5.7). Each Toast
       * also carries its own dismiss button (WCAG 2.2.1) so the 5s timer
       * isn't the only way to get rid of one. */}
      <div
        role="status"
        aria-live="polite"
        className="pointer-events-none fixed inset-x-0 bottom-0 z-50 flex flex-col items-end gap-2 p-4 sm:bottom-4 sm:right-4 sm:left-auto"
      >
        {toasts.map((t) => (
          <Toast
            key={t.id}
            title={t.title}
            description={t.description}
            variant={t.variant}
            onDismiss={() => dismiss(t.id)}
          />
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
