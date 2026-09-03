# Backhog's design system

How the interface is built, and what you have to preserve when you add to
it.

Backhog ships **three theme families**, and a component belongs to none of
them. It names a surface — `.panel`, `.f-field`, `.f-btn-gold`,
`font-display` — and whichever family is active supplies the recipe:

- **Midnight** (`flat`) — the original dark theme. Rounded surfaces,
  hairline borders, one violet accent, system type. The neutral one, and
  the default.
- **Arcade** (`pixel`) — real pixel art. Nine-sliced console-plastic
  frames, an arcade sprite sheet, four parallax backdrops.
- **Library** (`library`) — the reading room. A serif display voice,
  print-flat surfaces with real cast shadows, and the live item marked in
  the margin rather than filled in. Two themes share it: **Paper**, the
  app's only light ground, and **Hearth**, warm umber lit by a fire.

**The theme follows the arena.** Games and books each keep their own, so an
arcade cabinet for the backlog and a reading room for the shelf is one
setting rather than a compromise. A "use the same theme in both arenas"
switch collapses that back to one choice, and is on by default. See
[Per-arena theming](#per-arena-theming).

All three are assembled with Tailwind v4's CSS-first config and a handful of
React components. There is no separate build step for the app itself; only
the arcade family's *assets* have one, and it only needs to run again when
the art changes.

**If you read nothing else, read [Invariants](#invariants).**

---

## The idea

> The room is themeable. What is in it is not.

Whichever family is on, the covers, the prose, the numbers and the per-game
accent look identical. Only the chrome around them changes. Status colour
keeps its *hue* for the same reason — see invariant 3, which had to grow a
caveat when the app gained a light ground.

Backhog is a library you scan quickly and read closely: a wall of cover art
punctuated by short, dense text (titles, hours, ratings). The chrome around
that — panels, buttons, the backdrop — is where the arcade cabinet feel
lives. The covers, the prose, and the numbers never wear a filter.

Two consequences worth internalising:

- **Text colour is solved, not chosen.** `--c-600` through `--c-100` are
  derived from a WCAG contrast target against each theme's own panel fill
  (`scripts/build-assets.py`), which is why `text-ink-400` is equally
  readable in every theme, on a cream ground as much as a black one. Picking a hex by eye for one of these is how
  the arcade theme ended up shipping body text at 1.7:1. Surfaces
  (`--c-950`..`--c-700`, `--c-line`) are the other half of the ladder and
  *are* fixed lightnesses, because the frame sprites are recoloured from
  them and the token has to name the colour the sprite baked in.
- **The accent never changes.** `--accent`, sampled server-side from each
  game's own cover art, tints hover glows and progress fills inside framed
  surfaces. The frame itself is always theme-coloured; the accent is always
  the game's. Status cyan/green/red and the achievement tiers are the same
  constant — meaning-bearing colour does not re-theme. The one ramp that
  *does* is `hl-*` (coin gold in the arcade, brand violet in Midnight),
  because it is pure chrome: focus rings, the live nav item, "unlocked!".
- **Give the art room rather than shrinking it.** The arcade sheet's sprite
  cells are 32×64 pixel art; a fractional scale would resample them into
  mush. When a sprite doesn't fit, the layout grows around it or the mark
  becomes a vector — never the other way around.

---

## Where things live

| File | Owns | Edit? |
|---|---|---|
| `web/src/index.css` | Shared tokens (`@theme inline`), body/base styles, `focus-ring` and `skeleton` utilities | yes |
| `web/src/themes/flat.css` | The Midnight palette **and** the whole flat family recipe | yes |
| `web/src/themes/library.css` | The Paper and Hearth palettes **and** the library family recipe | yes |
| `web/src/lib/themes.ts` | The theme catalogue and the storage-key helpers — data only, no React | yes |
| `web/src/lib/arena.ts` | `ARENA_ROUTES` and `arenaForLocation()` — data only, no React | yes |
| `web/src/hooks/useArena.tsx` | Which arena the app is in; must sit inside the router | yes |
| `web/src/pixel/pixel.css` | Arcade frame classes, sprite/vector-icon sizing, the scene | yes |
| `web/src/pixel/themes.css` | Per-theme chrome ramp + frame URLs | **generated** |
| `web/src/pixel/fonts.css` | `@font-face` for Silkscreen | **generated** |
| `web/src/lib/gameicons.ts` | Vector icon path data + `GiName` type | **generated** |
| `web/src/components/ui/Gi.tsx` | Renders a vector icon by name | yes |
| `web/src/components/ui/Sprite.tsx` | Renders an arcade sprite cell by name | yes |
| `web/src/components/Scene.tsx` | Mounts the backdrop layers, parallax, rescaling | yes |
| `web/src/hooks/useTheme.tsx` | Theme + family context, `data-theme`/`data-family` on `<html>`, localStorage | yes |
| `web/index.html` | Carries the `<!--@boot-theme-->` marker; the script itself is **generated** | yes |
| `web/vite.config.ts` | `bootTheme()`, which builds that script from `lib/arena.ts` + `lib/themes.ts` | yes |
| `web/public/assets/` | Shipped PNGs (frames, sprites, scenes, fonts) | **generated** |

`lib/themes.ts` and `lib/arena.ts` must stay **dependency-free**: the Vite
config imports both at build time to generate the pre-paint script, so a
React import in either of them breaks the build.

**index.css vs pixel.css.** index.css says what a token or a base style
*is* — colour ramps, typography scale, focus rings. pixel.css says what a
component is *framed in*. They're split for the same reason grimoire split
style.css from its own pixel.css: a framed surface must not carry its own
background or radius, and keeping that rule in one file makes it
enforceable by reading rather than by memory.

Generated files carry a `do not edit` header (or are pure build output with
no header at all, like the PNGs). Regenerate with the scripts in
[Regenerating assets](#regenerating-assets); editing them by hand means the
next build silently reverts you.

---

## Invariants

Break these and the design degrades in ways that are easy to miss in
review.

**1. Pixel art scales by whole numbers only.**
Frames pair `border-image-slice` with an integer multiple of
`border-width`. Sprites scale by `--s: 1 | 2 | 3`. Current frame pairs, all
clean:

| Frame | slice | border-width | scale |
|---|---|---|---|
| panel / panel-active | `4 fill` | `8px` | 2× |
| field / field-focus | `4 fill` | `8px` | 2× |
| button (gold / danger / soft) | `6 fill` | `12px` | 2× |
| chip / chip-active | `4 fill` | `4px` | 1× |
| bar | `6 fill` | `6px` | 1× |

**2. A framed surface carries no `background-color`, `border-radius`, or
`box-shadow` of its own.** The sprite supplies fill and corners; a stray
radius clips them off, a stray shadow floats the frame off the plastic
behind it. The shared reset at the top of `pixel.css` lists every class
that adopts a frame — add yours there too.

**3. Meaning-bearing colour keeps its hue in every theme. It cannot keep
its lightness.**
Status cyan/green/red and the achievement tiers say what something *is*, so
the hue is a constant. The lightness is not free to be: these were written
as `bg-cyan-500/15 text-cyan-300`, the standard dark-mode badge recipe, and
on Paper's cream ground the 300 shade lands around 1.5:1. So the hue is
handed to the `tone-chip` / `tone-ink` utilities as `--tone` and the ink is
mixed toward `--c-100`, the theme's own strongest ink — which lightens the
hue on a dark ground and darkens it on a light one, from one expression.
Add a new meaning-bearing colour the same way; never hard-code a shade.

**3b. Coin gold, danger red, and the per-game accent are theme-independent.**
`button-gold.png`, `button-danger.png` and their pressed states live at
`web/public/assets/ui/` (not per-theme) and are recoloured once, from fixed
constants in `scripts/build-assets.py` (`GOLD`, `RED`). `--accent` is set
per element from `game.accent_hex` (`lib/format.ts` → `accentStyle()`); it
survives inside every framed surface as a glow or a progress fill, but it
never appears on a frame itself. A backdrop restating the accent would be
exactly the mistake grimoire's corpus-accent invariant exists to prevent.

**4. Themes change hue, and may invert the ladder — never the contrast
between two steps.**
`CHROME_L` in `scripts/build-assets.py` is fixed (`--c-950` through
`--c-100`). A theme borrows its scene's hue and saturation and applies them
to those exact lightness steps, so the contrast between any two steps is
identical in every theme. Never hand-write a theme colour — if a new scene
needs a different feel, that's a saturation or lightness-cap change in
`build-assets.py`, not a value typed into `themes.css`.

A *light* family inverts the ladder's direction: on Paper `--c-950` is the
brightest surface and `--c-100` the darkest ink. The **roles are unchanged**
— 950 is still the page, 800 is still the plate text is solved against, 100
is still the strongest ink — which is why no component knows which way round
it is. `solve_lightness()` picks the direction from the background's own
luminance and bisects toward black instead of white.

**5. Nothing loads from a third party.**
Fonts, sprites, frames and scenes are all under `/assets/`, shipped in
the nginx image. No Google Fonts link, no icon CDN. The page scanner's OCR
runtime is the same rule applied to something that fights it: Tesseract.js
defaults to jsDelivr for both its WASM core and its English model, so
`web/scripts/vendor-ocr.mjs` copies them out of `node_modules` into
`/assets/ocr/` at build time and `lib/ocr.ts` spells out every path. It costs
about 15 MB of image and means a phone can scan a page on a LAN with no
internet, and no page load tells anyone else which book is being read.

**6. Every animation must survive `prefers-reduced-motion`.**
The global rule in `index.css` neutralises CSS animations/transitions;
`Scene.tsx` skips the `pointermove` listener entirely when the media query
matches, and `.sprite-loading`'s blink is turned off in the same query in
`pixel.css`.

**7. Sprites and decorative vectors carry no accessible name of their own.**
`<Sprite>` always renders `aria-hidden="true"`. `<Gi>` does too, unless
given a `label` prop — the control around the icon (a labelled button, a
heading) is what should carry the name. Never let an icon be the only
accessible name for an interactive element.

**8. Text contrast has a floor.**
`--c-100` on `--c-900` is a high-contrast pairing in every theme, because
the ladder in invariant 4 keeps it that way. The scene veil
(`.scene-veil` in pixel.css) exists to guarantee a floor over *any*
backdrop, including the brightest layer of the arcade theme's neon sky. If
you lighten the veil, re-check body text over every theme, not just the
default — and over both grounds, since Paper is light and everything else is
dark. `--c-600` is the one step deliberately under the AA floor (3.5:1); it
is for decorative text only and measures the same in every theme.

---

## Per-arena theming

Backhog is two arenas, and one global theme forced a compromise neither
wanted. So each arena keeps its own slot:

| Key | Value |
|---|---|
| `backhog:theme:games` | `"pixel:arcade"` — family and theme, one string |
| `backhog:theme:books` | `"library:hearth"` |
| `backhog:theme:linked` | `"1"` (default) or `"0"` |
| `backhog:theme:{arena}:{family}` | the theme to return to when that family is re-picked |

Packing the family into the slot is deliberate: the pre-paint script can
stamp both `data-` attributes by splitting on a colon, without carrying its
own copy of the id → family map. That map used to be hand-mirrored in
`index.html` with a "keep the two in step" comment; it is not any more.

**Which arena a URL belongs to is decided once**, by `arenaForLocation()`
over the `ARENA_ROUTES` table in `lib/arena.ts` — books by path, then
`?media=book`, then games by path, then `null` for a shared page, which
inherits the arena you came from. The nav set and the theme both follow that
one answer, so `/queue` is the arcade and `/queue?media=book` is the reading
room without either being special-cased.

Adding a route that belongs to an arena means adding a row to
`ARENA_ROUTES`. Nothing else — the boot script is regenerated from it.

`linked` is on by default and, while on, writes both slots together, so
every read can just take the current arena's slot and be right either way.
Anyone upgrading is migrated onto it with their existing theme in both
arenas, and sees no change until they turn it off.

**Crossing arenas re-skins the shell.** `body` eases its background and
colour over 200ms; the frames themselves are nine-slice sprites and cannot
be interpolated, so nothing tries. The audio player stays mounted across the
crossing and re-skins with everything else — it is chrome, and chrome
follows the room.

---

## Choosing an icon

Two systems, one rule, same as grimoire's:

> **Sprite when there is room and full colour matters. Vector when it must
> be small or must take a text colour.**

```tsx
import { Sprite } from "@/components/ui/Sprite";
import { Gi } from "@/components/ui/Gi";

<span className="mark-hog" />           // the brand mark, 32x32
<Sprite name="stick" />                 // 32x64 pixel art, hero marks only
<Gi name="search" className="size-4" /> // vector, sizes via className, inherits colour
<Gi name="trash" className="size-4" label="Delete" />
```

- **`.mark-hog`** — the brand mark, and the only art in the app that is
  ours (`scripts/build-mark.py`, which also cuts the favicons from it).
  The wordmark and the mobile header mark. 32×32, whole-number `--s` only.
- **`<Sprite>`** — the arcade sheet (`web/public/assets/sprites/arcade.png`).
  Seven cells: `stick`, `stick-alt`, `button`, `button-hi`, `button-lo`,
  `pad`, `ball`. Decorative hardware only — the mark is not one of these.
  Minimum cell size 32×64 (or 32×32 for `ball`) — never scale down.
- **`<Gi name="...">`** — game-icons.net vectors. Use for everything else:
  every nav item, every button icon, every status badge, every small noun.
  Monochrome, inherits `currentColor`, crisp at any size.

Adding a sprite: crop a new cell from the arcade sheet in
`scripts/build-assets.py`'s `SPRITES` list, add it to `SPRITES` in
`web/src/components/ui/Sprite.tsx`, and re-run the build. Adding a vector:
add it to the `ICONS` map in `scripts/fetch-gameicons.py`, re-run, and
record it in `ATTRIBUTIONS.md` — the icons are CC BY 3.0 and attribution is
per-icon.

---

## Adding a new surface

Worked example — a framed panel:

1. **Layout in the component.** No background, no border classes of your
   own:
   ```tsx
   <div className="panel p-5">…</div>
   ```
   `panel` already carries the frame (see `pixel.css`'s `.f-panel, .panel`
   rule) — most surfaces should reach for the existing `panel`, `f-field`,
   `f-btn-*`, or `chip` classes rather than inventing a new frame.
2. **If you do need a new frame family**, add it to `pixel.css`'s shared
   reset *and* give it a `border-image-source`/`slice` pair, following the
   table in [Invariants #1](#invariants). If it should vary by theme, add
   the recolour to `build_frames()`/`themed_frames()` in
   `scripts/build-assets.py` instead of hand-picking a colour.
3. **Check it in all three families**, not just the default (Midnight):
   Settings → Theme → Arcade (and Backdrop → "Bare console"), then Library →
   Hearth *and* Paper. Paper is the one that catches light-on-dark
   assumptions — a hairline written as `border-white/[0.06]` disappears
   there. Reach for `border-edge`, `bg-fill-hover`, `bg-fill-active`,
   `ring-art` and `text-ink-max` instead; they are the tokens those idioms
   became, and they flip with the ground.

### Typography

Two voices:

| Token | Font | For |
|---|---|---|
| `--font-display` | Silkscreen (arcade) / system UI (Midnight) / serif (library) | Wordmark, nav labels, section labels, button text, stat numbers |
| (default) | system UI stack | Everything else — game titles, descriptions, dense table text |

Silkscreen for **labels and numbers only**, never for long-form text —
game descriptions and notes stay in the system font so they read at real
resolution, the same reason grimoire kept its prose in a real serif.

The library family's serif is `--f-serif`, defined in `index.css` rather
than in the family, because the EPUB reader's own "Serif" setting is the
same stack: a book's body text and the chrome around it should not be two
different serifs. It is all system faces, so it fetches nothing
(invariant 5). The family also puts bare `h1`/`h2`/`h3` in it — page titles
are plain elements rather than a component, and leaving the biggest word on
the page in a sans while the nav and buttons are serif reads as a mistake
rather than a contrast.

---

## Themes

Two tiers. A **family** decides how the chrome is built; a **theme** is one
palette inside it. Both land on `<html>` as `data-family` / `data-theme`.

| Family | Themes | Chrome |
|---|---|---|
| `flat` | `midnight` (default) | Rounded surfaces, hairline borders, violet accent, system type |
| `pixel` | `arcade`, `forest`, `cavern`, `dusk`, `bare` | Nine-slice sprite frames, Silkscreen, parallax backdrop |

Only the pixel family has more than one theme, because its chrome is
*recoloured from its backdrop art* at build time — backdrop and palette are
one choice. That is why Settings reads "pick a family, then (for Arcade) a
backdrop".

| Pixel theme | Hue (measured) | Chrome |
|---|---|---|
| `arcade` | ~291° | Violet-magenta |
| `forest` | ~228° | Cool blue-grey |
| `cavern` | ~44° | Warm brown |
| `dusk` | ~332° | Mauve-pink |
| `bare` | — | Falls back to `:root`, no backdrop |

**How a family is implemented.** Both families style the *same* class
names. `pixel.css` writes them unprefixed (`.f-panel { … }`); `flat.css`
writes them as `html[data-family="flat"] .f-panel { … }`, which wins on
specificity alone, so import order does not matter. Both stay inside
`@layer components` so ordinary Tailwind utilities still override them at
the call site.

Three things a family owns that CSS alone cannot express, and which
therefore branch in TSX on `useTheme().family`:

- **Control metrics** (`primitives.tsx`) — the arcade's frames are 12px of
  sprite a side, so its buttons and fields have to be taller.
- **Label type** — Silkscreen is a 5px bitmap that only reads uppercase at
  ~11px; the system face wants normal case at 14px.
- **Hero marks** — 🐗 vs. a sprite cell, in `Layout.tsx` / `AuthShell.tsx`.

Everything else — colour, radius, border, shadow, the progress track's
height, the loading mark — belongs in CSS. If you find yourself adding a
fourth `family === "…"` branch, check first whether a token would do.

**Adding a backdrop to the pixel family:** drop a layered scene pack into
`icon-packs/`, add it to `SCENES` in `scripts/build-assets.py` (source pack
+ ordered layer list + whether it tiles) *and* to `THEMES` in
`web/src/hooks/useTheme.tsx` **and** the `FAMILY` map in `web/index.html`,
then re-run the build. Hue, chrome ramp, frames, and sky colour are all
derived — you write no colours by hand.

### Two things about the backdrop art

- **Layers are numbered front-to-back.** `0.png` is the foreground; the
  highest index is the sky. `Scene.tsx` appends them in reverse so index 0
  paints on top.
- **The scene is `position: fixed; z-index: 0`.** `#root` is `position:
  relative; z-index: 1` in `index.css`. Anything rendered outside `#root`
  (there shouldn't be anything) would sit behind the backdrop.

---

## Regenerating assets

Shipped assets are committed under `web/public/assets/`; a normal
`npm run build` needs none of this. All three scripts are pure-stdlib
Python 3, mirroring grimoire's pipeline (`scripts/pngkit.py` is a shared,
unmodified copy of the same minimal PNG codec).

```bash
python3 scripts/build-assets.py     # frames, sprites, scenes, themes.css
python3 scripts/build-mark.py       # the hog: sprites/hog.png + both favicons
python3 scripts/fetch-gameicons.py  # web/src/lib/gameicons.ts
python3 scripts/fetch-fonts.py      # web/public/assets/fonts/ + pixel/fonts.css
```

`build-mark.py` needs no inputs — the mark is a pixel grid in the script
itself, so it is the one asset step that always runs. `build-assets.py`
reads from `icon-packs/`, which is **gitignored on purpose** — see `ATTRIBUTIONS.md` for the four download links (all CC0,
free, no account needed).

Its recolour maps are **exhaustive** — an unmapped colour aborts the build
rather than shipping a half-recoloured sprite. If it fails after an art
update, that's the check working; add the new colour to the map.

---

## Verifying a change

```bash
cd web && npm run typecheck && npm run build
cd api && go test ./...
```

Then look at it. Things worth checking that automated tests won't catch:

- At least one theme *and* bare console (Settings → Theme), not just the
  default `arcade`.
- The per-game accent still shows through: open a game with a cover (glow
  on `GameCard`, progress fill on `SessionLog`/`ProjectDetailPage`) — it
  should never match the frame colour.
- Narrow width (≤560px): the sidebar collapses to the mobile top bar;
  nothing overflows.
- Zoom to 200%: no blurred sprites, no resampled frame corners.
- `prefers-reduced-motion` on: the scene stops drifting, the loading blink
  stops, layout is unchanged.
