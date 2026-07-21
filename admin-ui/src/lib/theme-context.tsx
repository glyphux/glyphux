import { createContext, useContext, useEffect, useState, type ReactNode } from "react";

export type ThemeMode = "light" | "dark" | "system";

interface ThemeState {
  mode: ThemeMode;
  setMode: (mode: ThemeMode) => void;
  /** The resolved theme actually applied (mode "system" resolves to the OS
   * preference at render time). */
  resolved: "light" | "dark";
}

const ThemeContext = createContext<ThemeState | undefined>(undefined);

const MODE_KEY = "glyphux.admin.theme-mode";

function systemPrefersDark(): boolean {
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ?? false;
}

/** localStorage can throw (private browsing, storage disabled by policy) —
 * guarded the same way client.ts's loadToken/setToken are, since an
 * unguarded read here runs during ThemeProvider's very first render and
 * would white-screen the whole admin shell before login even renders. */
function readStored(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

function writeStored(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // Unavailable — the in-memory state still works for this tab's lifetime.
  }
}

/** Admin chrome theming (PRD §5.7: "admin theming via tokens: at minimum
 * light/dark"). Applies `.dark` on <html> so every token defined in
 * index.css resolves consistently regardless of which component tree
 * renders first.
 *
 * Density (comfortable/compact) is not implemented — it's real, separately
 * tracked follow-up work (a spacing-token scale plus the components that
 * consume it), not a per-role toggle stub with no visible effect. */
export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(() => {
    const stored = readStored(MODE_KEY);
    return stored === "light" || stored === "dark" || stored === "system" ? stored : "system";
  });

  const resolved: "light" | "dark" = mode === "system" ? (systemPrefersDark() ? "dark" : "light") : mode;

  useEffect(() => {
    const root = document.documentElement;
    root.classList.toggle("dark", resolved === "dark");
    root.style.colorScheme = resolved;
  }, [resolved]);

  useEffect(() => {
    if (mode !== "system") return;
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => setModeState("system"); // force a re-render/re-resolve
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, [mode]);

  const setMode = (next: ThemeMode) => {
    setModeState(next);
    writeStored(MODE_KEY, next);
  };

  return <ThemeContext.Provider value={{ mode, setMode, resolved }}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeState {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error("useTheme must be used within a ThemeProvider");
  return ctx;
}
