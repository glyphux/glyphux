import { createContext, useContext, useEffect, useState, type ReactNode } from "react";

export type ThemeMode = "light" | "dark" | "system";
export type Density = "comfortable" | "compact";

interface ThemeState {
  mode: ThemeMode;
  setMode: (mode: ThemeMode) => void;
  /** The resolved theme actually applied (mode "system" resolves to the OS
   * preference at render time). */
  resolved: "light" | "dark";
  density: Density;
  setDensity: (density: Density) => void;
}

const ThemeContext = createContext<ThemeState | undefined>(undefined);

const MODE_KEY = "glyphux.admin.theme-mode";
const DENSITY_KEY = "glyphux.admin.density";

function systemPrefersDark(): boolean {
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ?? false;
}

/** Admin chrome theming (PRD §5.7: "admin theming via tokens: at minimum
 * light/dark... layout/density configurability... persist layout
 * preferences"). Applies `.dark` / `data-density` on <html> so every token
 * defined in index.css resolves consistently regardless of which component
 * tree renders first. */
export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(() => {
    const stored = window.localStorage.getItem(MODE_KEY);
    return stored === "light" || stored === "dark" || stored === "system" ? stored : "system";
  });
  const [density, setDensityState] = useState<Density>(() => {
    const stored = window.localStorage.getItem(DENSITY_KEY);
    return stored === "compact" ? "compact" : "comfortable";
  });

  const resolved: "light" | "dark" = mode === "system" ? (systemPrefersDark() ? "dark" : "light") : mode;

  useEffect(() => {
    const root = document.documentElement;
    root.classList.toggle("dark", resolved === "dark");
    root.style.colorScheme = resolved;
  }, [resolved]);

  useEffect(() => {
    document.documentElement.dataset.density = density;
  }, [density]);

  useEffect(() => {
    if (mode !== "system") return;
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => setModeState("system"); // force a re-render/re-resolve
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, [mode]);

  const setMode = (next: ThemeMode) => {
    setModeState(next);
    window.localStorage.setItem(MODE_KEY, next);
  };
  const setDensity = (next: Density) => {
    setDensityState(next);
    window.localStorage.setItem(DENSITY_KEY, next);
  };

  return (
    <ThemeContext.Provider value={{ mode, setMode, resolved, density, setDensity }}>
      {children}
    </ThemeContext.Provider>
  );
}

export function useTheme(): ThemeState {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error("useTheme must be used within a ThemeProvider");
  return ctx;
}
