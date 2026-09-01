import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";

/**
 * Backhog's theme system, in two tiers.
 *
 * A *family* decides how the chrome is built. There are two, and they are
 * genuinely different pieces of CSS, not two palettes:
 *
 *   flat   — the original dark theme. Rounded surfaces, hairline borders,
 *            a violet accent, system type. Neutral enough for a shelf of
 *            books, which is where Backhog is heading.
 *   pixel  — the arcade kit. Nine-slice sprite frames, Silkscreen type, a
 *            parallax backdrop behind everything.
 *
 * A *theme* is one palette inside a family. The flat family has one
 * (Midnight); the pixel family has one per backdrop, because its chrome is
 * recoloured from the backdrop art at build time (scripts/build-assets.py).
 * That is why the picker in Settings reads "pick a family, then a backdrop".
 *
 * Both tiers land on <html> as data-theme / data-family. Every component
 * keeps one set of class names — .panel, .f-field, .f-btn-gold — and the
 * family decides what those mean (see themes/flat.css and pixel/pixel.css).
 *
 * NOTE: web/index.html repeats this id -> family map in a pre-paint script
 * so the first frame is not the wrong theme. Keep the two in step.
 */

export type ThemeFamily = "flat" | "pixel";

export const THEMES = {
  midnight: {
    label: "Midnight",
    note: "The original dark",
    family: "flat",
  },
  arcade: { label: "Neon arcade", note: "The street outside the arcade", family: "pixel" },
  dusk: { label: "Dusk ridge", note: "Mountains before night", family: "pixel" },
  cavern: { label: "Cavern", note: "A lantern-lit dig", family: "pixel" },
  forest: { label: "Night forest", note: "Silhouettes between the trees", family: "pixel" },
  bare: { label: "Bare console", note: "No backdrop at all", family: "pixel" },
} as const satisfies Record<string, { label: string; note: string; family: ThemeFamily }>;

export type Theme = keyof typeof THEMES;

/** The picker's top row: one card per family. */
export const FAMILIES = {
  flat: {
    label: "Midnight",
    blurb: "Quiet dark chrome. Rounded surfaces, one violet accent, nothing between you and the covers.",
  },
  pixel: {
    label: "Arcade",
    blurb: "The pixel-art kit. Sprite frames, Silkscreen type, and a parallax backdrop you can pick.",
  },
} as const satisfies Record<ThemeFamily, { label: string; blurb: string }>;

export const DEFAULT_THEME: Theme = "midnight";

/** Every theme in a family, in picker order. */
export function themesInFamily(family: ThemeFamily): Theme[] {
  return (Object.keys(THEMES) as Theme[]).filter((id) => THEMES[id].family === family);
}

const STORAGE_KEY = "backhog:theme";
/** The backdrop to come back to when the arcade family is re-selected. */
const PIXEL_KEY = "backhog:theme:pixel";

interface ThemeValue {
  theme: Theme;
  family: ThemeFamily;
  setTheme: (t: Theme) => void;
  /** Switch families, landing on whichever theme in it was last used. */
  setFamily: (f: ThemeFamily) => void;
}

const ThemeContext = createContext<ThemeValue>({
  theme: DEFAULT_THEME,
  family: THEMES[DEFAULT_THEME].family,
  setTheme: () => {},
  setFamily: () => {},
});

function isTheme(v: string | null): v is Theme {
  return !!v && v in THEMES;
}

function read(): Theme {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    return isTheme(v) ? v : DEFAULT_THEME;
  } catch {
    return DEFAULT_THEME;
  }
}

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [theme, setTheme] = useState<Theme>(read);
  const family = THEMES[theme].family;

  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = theme;
    root.dataset.family = family;

    // The browser chrome should match the app's own backdrop rather than
    // the one hardcoded at build time.
    const meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
    if (meta) {
      const sky = getComputedStyle(root).getPropertyValue("--c-950").trim();
      if (sky) meta.content = sky;
    }

    try {
      localStorage.setItem(STORAGE_KEY, theme);
      if (family === "pixel") localStorage.setItem(PIXEL_KEY, theme);
    } catch {
      /* private browsing — the choice just will not survive the reload */
    }
  }, [theme, family]);

  const setFamily = useCallback((next: ThemeFamily) => {
    if (next === "flat") {
      setTheme("midnight");
      return;
    }
    let remembered: string | null = null;
    try {
      remembered = localStorage.getItem(PIXEL_KEY);
    } catch {
      /* no storage — fall through to the family's first theme */
    }
    setTheme(
      isTheme(remembered) && THEMES[remembered].family === next
        ? remembered
        : themesInFamily(next)[0],
    );
  }, []);

  const value = useMemo<ThemeValue>(
    () => ({ theme, family, setTheme, setFamily }),
    [theme, family, setFamily],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme() {
  return useContext(ThemeContext);
}
