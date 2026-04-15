"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";

/**
 * theme.tsx — client-side theme provider for PR #33.
 *
 * apogee was dark-only until this PR. The provider introduces a tri-state
 * preference (`system | light | dark`) that resolves to an effective
 * `Theme` and writes `data-theme` onto `<html>`. All rendering is still
 * CSS-variable driven — see `app/globals.css` for the palette definitions.
 *
 * Flash-of-wrong-theme is prevented by the inline script in `app/layout.tsx`
 * which applies the saved preference (or `prefers-color-scheme`) before
 * React hydrates. This provider then takes over and keeps the attribute in
 * sync with the user's explicit choice, persisting it to localStorage when
 * they pick `light` or `dark` and clearing it when they pick `system`.
 *
 * Shape-wise: `preference` is the user's choice; `systemTheme` tracks the
 * OS-level `prefers-color-scheme` so that `system` mode can re-render when
 * the OS flips. The effective `theme` is a pure derivation of those two,
 * computed via `useMemo` so the React 19
 * `react-hooks/set-state-in-effect` rule stays happy.
 */

export type Theme = "dark" | "light";
export type Preference = Theme | "system";

export const THEME_STORAGE_KEY = "apogee:theme";

interface ThemeContextValue {
  /** The currently applied theme. */
  theme: Theme;
  /** What the user picked, including `system`. */
  preference: Preference;
  /** Update the preference. Persists + applies `data-theme` immediately. */
  setPreference: (p: Preference) => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

function resolveSystemTheme(): Theme {
  if (typeof window === "undefined") return "dark";
  return window.matchMedia("(prefers-color-scheme: light)").matches
    ? "light"
    : "dark";
}

function readStoredPreference(): Preference {
  if (typeof window === "undefined") return "system";
  try {
    const saved = window.localStorage.getItem(THEME_STORAGE_KEY);
    if (saved === "light" || saved === "dark") return saved;
    return "system";
  } catch {
    return "system";
  }
}

function applyThemeAttribute(theme: Theme) {
  if (typeof document === "undefined") return;
  document.documentElement.setAttribute("data-theme", theme);
}

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  // Lazy initializers run once on mount. On the server they return the
  // defaults ("system" / "dark"); on the client they read the persisted
  // preference and the live media-query state the inline script already
  // consumed, so the first render matches what's painted.
  const [preference, setPreferenceState] = useState<Preference>(() =>
    readStoredPreference(),
  );
  const [systemTheme, setSystemTheme] = useState<Theme>(() =>
    resolveSystemTheme(),
  );

  // Effective theme is a pure derivation — no setState in effects needed.
  const theme: Theme = useMemo(
    () => (preference === "system" ? systemTheme : preference),
    [preference, systemTheme],
  );

  // Subscribe to OS-level prefers-color-scheme once. `setSystemTheme` is
  // only called from the media-query change handler (a callback, not the
  // effect body), so the set-state-in-effect rule does not fire.
  useEffect(() => {
    if (typeof window === "undefined") return undefined;
    const mq = window.matchMedia("(prefers-color-scheme: light)");
    const handler = (ev: MediaQueryListEvent) => {
      setSystemTheme(ev.matches ? "light" : "dark");
    };
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, []);

  // Mirror preference → localStorage. Writes happen in an effect so they
  // don't run on the server and can be wrapped in try/catch for private
  // mode. No setState here — pure side-effect on localStorage.
  useEffect(() => {
    if (typeof window === "undefined") return;
    try {
      if (preference === "system") {
        window.localStorage.removeItem(THEME_STORAGE_KEY);
      } else {
        window.localStorage.setItem(THEME_STORAGE_KEY, preference);
      }
    } catch {
      // ignore storage errors (private mode, quota, etc.)
    }
  }, [preference]);

  // Mirror effective theme → <html data-theme>. Pure DOM side-effect.
  useEffect(() => {
    applyThemeAttribute(theme);
  }, [theme]);

  const setPreference = useCallback((next: Preference) => {
    setPreferenceState(next);
  }, []);

  return (
    <ThemeContext.Provider value={{ theme, preference, setPreference }}>
      {children}
    </ThemeContext.Provider>
  );
}

/**
 * useTheme — returns the current theme + preference. When called outside a
 * provider (e.g. unit tests, server-rendered fallback), returns a stub that
 * reports `dark` / `system` and ignores writes, so consumers never crash.
 */
export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (ctx) return ctx;
  return {
    theme: "dark",
    preference: "system",
    setPreference: () => {
      /* no-op outside provider */
    },
  };
}
