import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";

import { useArena } from "./useArena";
import { ARENAS, type Arena } from "@/lib/arena";
import {
  DEFAULT_THEME,
  LEGACY_KEY,
  LEGACY_PIXEL_KEY,
  LINKED_KEY,
  familyOf,
  isTheme,
  packSlot,
  rememberKey,
  slotKey,
  themesInFamily,
  unpackSlot,
  type Theme,
  type ThemeFamily,
} from "@/lib/themes";

/**
 * The active theme, which is a function of the arena you are standing in.
 *
 * Backhog is two arenas now, and one global theme forced a compromise neither
 * of them wanted: the arcade cabinet is right for a games backlog and wrong for
 * a shelf of books. So each arena keeps its own slot, and crossing between them
 * re-dresses the room.
 *
 * `linked` collapses that back to a single choice and is **on** by default, so
 * nobody gets a surprise second theme they did not ask for. When it is on both
 * slots are written together, which means every read below can just take the
 * current arena's slot and be right either way.
 *
 * The catalogue itself (THEMES, FAMILIES, the storage key helpers) lives in
 * lib/themes.ts, because vite.config.ts reads it at build time to generate the
 * pre-paint script in index.html.
 */

export type { Theme, ThemeFamily };
export { THEMES, FAMILIES, DEFAULT_THEME, TIER_LABEL, themesInFamily } from "@/lib/themes";

type Slots = Record<Arena, Theme>;

interface ThemeValue {
  theme: Theme;
  family: ThemeFamily;
  /** Every arena's current theme — the Settings picker needs both at once. */
  slots: Slots;
  /** Are the two arenas sharing one theme? */
  linked: boolean;
  setLinked: (linked: boolean) => void;
  /** Set the theme for `arena`, defaulting to the one you are in. */
  setTheme: (theme: Theme, arena?: Arena) => void;
  /** Switch families, landing on whichever theme in it was last used. */
  setFamily: (family: ThemeFamily, arena?: Arena) => void;
}

const ThemeContext = createContext<ThemeValue>({
  theme: DEFAULT_THEME,
  family: familyOf(DEFAULT_THEME),
  slots: { games: DEFAULT_THEME, books: DEFAULT_THEME },
  linked: true,
  setLinked: () => {},
  setTheme: () => {},
  setFamily: () => {},
});

/**
 * Fold the old single-theme keys into the per-arena ones, once.
 *
 * Anyone upgrading has exactly one theme and no opinion about arenas yet, so
 * both slots get it and `linked` stays on: the app looks identical until they
 * go and turn the link off.
 */
function migrate() {
  try {
    if (localStorage.getItem(slotKey("games"))) return;
    const legacy = localStorage.getItem(LEGACY_KEY);
    if (!isTheme(legacy)) return;
    for (const arena of ARENAS) localStorage.setItem(slotKey(arena), packSlot(legacy));
    localStorage.setItem(LINKED_KEY, "1");

    const pixel = localStorage.getItem(LEGACY_PIXEL_KEY);
    if (isTheme(pixel)) {
      for (const arena of ARENAS) localStorage.setItem(rememberKey(arena, "pixel"), pixel);
    }
  } catch {
    /* private browsing — nothing to migrate and nowhere to put it */
  }
}

function readSlots(): Slots {
  migrate();
  const read = (arena: Arena): Theme => {
    try {
      return unpackSlot(localStorage.getItem(slotKey(arena))) ?? DEFAULT_THEME;
    } catch {
      return DEFAULT_THEME;
    }
  };
  return { games: read("games"), books: read("books") };
}

function readLinked(): boolean {
  try {
    // Absent means "has never chosen", which is linked — the pre-per-arena
    // behaviour, and the one that surprises nobody.
    return localStorage.getItem(LINKED_KEY) !== "0";
  } catch {
    return true;
  }
}

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const { arena } = useArena();
  const [slots, setSlots] = useState<Slots>(readSlots);
  const [linked, setLinkedState] = useState<boolean>(readLinked);

  const theme = slots[arena];
  const family = familyOf(theme);

  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = theme;
    root.dataset.family = family;

    // The browser chrome should match the app's own backdrop rather than the
    // one hardcoded at build time.
    const meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
    if (meta) {
      const sky = getComputedStyle(root).getPropertyValue("--c-950").trim();
      if (sky) meta.content = sky;
    }
  }, [theme, family]);

  useEffect(() => {
    try {
      for (const key of ARENAS) {
        localStorage.setItem(slotKey(key), packSlot(slots[key]));
        localStorage.setItem(rememberKey(key, familyOf(slots[key])), slots[key]);
      }
      localStorage.setItem(LINKED_KEY, linked ? "1" : "0");
    } catch {
      /* private browsing — the choice just will not survive the reload */
    }
  }, [slots, linked]);

  const setTheme = useCallback(
    (next: Theme, target?: Arena) => {
      const which = target ?? arena;
      setSlots((prev) =>
        linked
          ? { games: next, books: next }
          : { ...prev, [which]: next },
      );
    },
    [arena, linked],
  );

  const setFamily = useCallback(
    (next: ThemeFamily, target?: Arena) => {
      const which = target ?? arena;
      let remembered: string | null = null;
      try {
        remembered = localStorage.getItem(rememberKey(which, next));
      } catch {
        /* no storage — fall through to the family's first theme */
      }
      const pick =
        isTheme(remembered) && familyOf(remembered) === next
          ? remembered
          : themesInFamily(next)[0];
      setTheme(pick, which);
    },
    [arena, setTheme],
  );

  const setLinked = useCallback(
    (next: boolean) => {
      setLinkedState(next);
      // Linking collapses two themes into one, and the arena you are standing
      // in is the one you can actually see — so that is the one that wins.
      if (next) setSlots((prev) => ({ games: prev[arena], books: prev[arena] }));
    },
    [arena],
  );

  const value = useMemo<ThemeValue>(
    () => ({ theme, family, slots, linked, setLinked, setTheme, setFamily }),
    [theme, family, slots, linked, setLinked, setTheme, setFamily],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme() {
  return useContext(ThemeContext);
}
