import { createContext, useContext, useEffect, useState } from "react";

/**
 * The theme is the room the app sits in. Picking a backdrop retints the
 * whole chrome — the console-plastic ramp, the nine-slice frames, the sky
 * behind the parallax — because every one of those is derived from the
 * scene's own hue at build time (scripts/build-assets.py). What does not
 * change: the coin-gold actions, the danger red, and the per-game accent.
 */

export const THEMES = {
  arcade: { label: "Neon arcade", note: "The street outside the arcade" },
  dusk: { label: "Dusk ridge", note: "Mountains before night" },
  cavern: { label: "Cavern", note: "A lantern-lit dig" },
  forest: { label: "Night forest", note: "Silhouettes between the trees" },
  bare: { label: "Bare console", note: "No backdrop at all" },
} as const;

export type Theme = keyof typeof THEMES;
export const DEFAULT_THEME: Theme = "arcade";

const STORAGE_KEY = "backhog:theme";

const ThemeContext = createContext<{
  theme: Theme;
  setTheme: (t: Theme) => void;
}>({ theme: DEFAULT_THEME, setTheme: () => {} });

function read(): Theme {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    return v && v in THEMES ? (v as Theme) : DEFAULT_THEME;
  } catch {
    return DEFAULT_THEME;
  }
}

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [theme, setThemeState] = useState<Theme>(read);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    try {
      localStorage.setItem(STORAGE_KEY, theme);
    } catch {
      /* private browsing — the choice just will not survive the reload */
    }
  }, [theme]);

  return (
    <ThemeContext.Provider value={{ theme, setTheme: setThemeState }}>
      {children}
    </ThemeContext.Provider>
  );
}

export function useTheme() {
  return useContext(ThemeContext);
}
