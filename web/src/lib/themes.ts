/**
 * The theme catalogue — data only, so the pre-paint script can share it.
 *
 * Backhog's theming is two-tiered. A *family* decides how the chrome is built
 * and is a genuinely different piece of CSS, not a palette:
 *
 *   flat    — the original dark theme. Rounded surfaces, hairline borders, a
 *             violet accent, system type. The neutral one.
 *   pixel   — the arcade kit. Nine-slice sprite frames, Silkscreen type, a
 *             parallax backdrop behind everything.
 *   library — the reading room. Serif display, print-flat surfaces, the live
 *             item marked in the margin rather than filled in.
 *
 * A *theme* is one palette inside a family. The pixel family has one per
 * backdrop, because its chrome is recoloured from the backdrop art at build
 * time (scripts/build-assets.py) — which is why the picker reads "pick a
 * family, then a backdrop".
 *
 * Both tiers land on <html> as data-theme / data-family. Every component keeps
 * one set of class names — .panel, .f-field, .f-btn-gold — and the family
 * decides what those mean (themes/flat.css, pixel/pixel.css).
 *
 * Keep this module dependency-free: vite.config.ts imports it at build time to
 * generate the boot script, so it must not pull in React.
 */

export type ThemeFamily = "flat" | "pixel" | "library";

export const THEMES = {
  midnight: { label: "Midnight", note: "The original dark", family: "flat" },
  arcade: { label: "Neon arcade", note: "The street outside the arcade", family: "pixel" },
  dusk: { label: "Dusk ridge", note: "Mountains before night", family: "pixel" },
  cavern: { label: "Cavern", note: "A lantern-lit dig", family: "pixel" },
  forest: { label: "Night forest", note: "Silhouettes between the trees", family: "pixel" },
  bare: { label: "Bare console", note: "No backdrop at all", family: "pixel" },
  paper: { label: "Paper", note: "Warm daylight", family: "library" },
  hearth: { label: "Hearth", note: "Read by the fire", family: "library" },
} as const satisfies Record<string, { label: string; note: string; family: ThemeFamily }>;

export type Theme = keyof typeof THEMES;

/** The picker's top row: one card per family. */
export const FAMILIES = {
  flat: {
    label: "Midnight",
    blurb:
      "Quiet dark chrome. Rounded surfaces, one violet accent, nothing between you and the covers.",
  },
  pixel: {
    label: "Arcade",
    blurb: "The pixel-art kit. Sprite frames, Silkscreen type, and a parallax backdrop you can pick.",
  },
  library: {
    label: "Library",
    blurb: "Ink on paper. Serif type, rules instead of boxes, and the page you are on marked in the margin.",
  },
} as const satisfies Record<ThemeFamily, { label: string; blurb: string }>;

export const DEFAULT_THEME: Theme = "midnight";

export function isTheme(v: string | null | undefined): v is Theme {
  return !!v && v in THEMES;
}

export function familyOf(theme: Theme): ThemeFamily {
  return THEMES[theme].family;
}

/** Every theme in a family, in picker order. */
export function themesInFamily(family: ThemeFamily): Theme[] {
  return (Object.keys(THEMES) as Theme[]).filter((id) => THEMES[id].family === family);
}

/**
 * What a family calls its second tier, or null when it only has one theme and
 * the picker should not show a row at all. The arcade's themes *are* its
 * backdrops — its chrome is recoloured from the art — while the library's are
 * two grounds to read on.
 */
export const TIER_LABEL: Record<ThemeFamily, { title: string; blurb: string } | null> = {
  flat: null,
  pixel: {
    title: "Backdrop",
    blurb: "Retints the whole cabinet to match the scene behind it.",
  },
  library: {
    title: "Light",
    blurb: "Read by daylight or by the fire. Same room, different hour.",
  },
};

/* ---------- Storage ----------
   One slot per arena, holding "family:theme" in a single string. Packing the
   family in is deliberate: the pre-paint script can stamp both data attributes
   by splitting on a colon, without carrying its own copy of the id -> family
   map (which is exactly the thing that used to drift). */

export const LINKED_KEY = "backhog:theme:linked";
/** Pre-per-arena key, read once and migrated. */
export const LEGACY_KEY = "backhog:theme";
export const LEGACY_PIXEL_KEY = "backhog:theme:pixel";

export function slotKey(arena: string): string {
  return `backhog:theme:${arena}`;
}

/** The theme to come back to when `family` is re-selected in `arena`. */
export function rememberKey(arena: string, family: ThemeFamily): string {
  return `backhog:theme:${arena}:${family}`;
}

export function packSlot(theme: Theme): string {
  return `${familyOf(theme)}:${theme}`;
}

/** The theme half of a slot value, or null if it is missing or unrecognised. */
export function unpackSlot(value: string | null): Theme | null {
  if (!value) return null;
  const theme = value.slice(value.indexOf(":") + 1);
  return isTheme(theme) ? theme : null;
}
