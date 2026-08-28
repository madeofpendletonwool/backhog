# Backhog's design system

How the interface is built, and what you have to preserve when you add to
it.

Backhog's UI is real pixel art — nine-sliced console-plastic frames, an
arcade sprite sheet, four parallax backdrops — assembled with Tailwind v4's
CSS-first config and a handful of React components. There is no separate
build step for the app itself; only the *assets* have one, and it only
needs to run again when the art changes.

**If you read nothing else, read [Invariants](#invariants).**

---

## The idea

> Coin-gold for the actions, console plastic for the room. Pixel art
> carries the chrome and the hero marks. The words, the covers, and the
> per-game accent stay at full resolution.

Backhog is a library you scan quickly and read closely: a wall of cover art
punctuated by short, dense text (titles, hours, ratings). The chrome around
that — panels, buttons, the backdrop — is where the arcade cabinet feel
lives. The covers, the prose, and the numbers never wear a filter.

Two consequences worth internalising:

- **The accent never changes.** `--accent`, sampled server-side from each
  game's own cover art, tints hover glows and progress fills inside framed
  surfaces. The frame itself is always theme-coloured; the accent is always
  the game's. Coin gold (the primary action) and danger red are a third,
  separate constant — they don't re-tint either, the same way grimoire's
  gold buttons never changed between its four themes.
- **Give the art room rather than shrinking it.** The arcade sheet's sprite
  cells are 32×64 pixel art; a fractional scale would resample them into
  mush. When a sprite doesn't fit, the layout grows around it or the mark
  becomes a vector — never the other way around.

---

## Where things live

| File | Owns | Edit? |
|---|---|---|
| `web/src/index.css` | Tokens (`@theme inline`), body/base styles, `focus-ring` and `skeleton` utilities | yes |
| `web/src/pixel/pixel.css` | Frame classes, sprite/vector-icon sizing, the scene | yes |
| `web/src/pixel/themes.css` | Per-theme chrome ramp + frame URLs | **generated** |
| `web/src/pixel/fonts.css` | `@font-face` for Silkscreen | **generated** |
| `web/src/lib/gameicons.ts` | Vector icon path data + `GiName` type | **generated** |
| `web/src/components/ui/Gi.tsx` | Renders a vector icon by name | yes |
| `web/src/components/ui/Sprite.tsx` | Renders an arcade sprite cell by name | yes |
| `web/src/components/Scene.tsx` | Mounts the backdrop layers, parallax, rescaling | yes |
| `web/src/hooks/useTheme.tsx` | Theme context, `data-theme` on `<html>`, localStorage | yes |
| `web/public/assets/` | Shipped PNGs (frames, sprites, scenes, fonts) | **generated** |

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

**3. Coin gold, danger red, and the per-game accent are theme-independent.**
`button-gold.png`, `button-danger.png` and their pressed states live at
`web/public/assets/ui/` (not per-theme) and are recoloured once, from fixed
constants in `scripts/build-assets.py` (`GOLD`, `RED`). `--accent` is set
per element from `game.accent_hex` (`lib/format.ts` → `accentStyle()`); it
survives inside every framed surface as a glow or a progress fill, but it
never appears on a frame itself. A backdrop restating the accent would be
exactly the mistake grimoire's corpus-accent invariant exists to prevent.

**4. Themes change hue, never the lightness ladder.**
`CHROME_L` in `scripts/build-assets.py` is fixed (`--c-950` through
`--c-100`). A theme borrows its scene's hue and saturation and applies them
to those exact lightness steps, so the contrast between any two steps is
identical in every theme. Never hand-write a theme colour — if a new scene
needs a different feel, that's a saturation or lightness-cap change in
`build-assets.py`, not a value typed into `themes.css`.

**5. Nothing loads from a third party.**
Fonts, sprites, frames and scenes are all under `/assets/`, shipped in the
nginx image. No Google Fonts link, no icon CDN.

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
default.

---

## Choosing an icon

Two systems, one rule, same as grimoire's:

> **Sprite when there is room and full colour matters. Vector when it must
> be small or must take a text colour.**

```tsx
import { Sprite } from "@/components/ui/Sprite";
import { Gi } from "@/components/ui/Gi";

<Sprite name="stick" />                 // 32x64 pixel art, hero marks only
<Sprite name="ball" scale={2} />        // 64x128, for a bigger mark
<Gi name="search" className="size-4" /> // vector, sizes via className, inherits colour
<Gi name="trash" className="size-4" label="Delete" />
```

- **`<Sprite>`** — the arcade sheet (`web/public/assets/sprites/arcade.png`).
  Six cells: `stick`, `stick-alt`, `button`, `button-hi`, `button-lo`, `pad`,
  `ball`. Use for the handful of marks the app is *about*: the wordmark,
  the mobile header mark, the favicon. Minimum cell size 32×64 (or 32×32
  for `ball`) — never scale down.
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
3. **Check it against every theme and against bare plastic** (Settings →
   Theme → "Bare console"), not just the default (`arcade`).

### Typography

Two voices:

| Token | Font | For |
|---|---|---|
| `--font-pixel` | Silkscreen | Wordmark, nav labels, section labels, button text, stat numbers |
| (default) | system UI stack | Everything else — game titles, descriptions, dense table text |

Silkscreen for **labels and numbers only**, never for long-form text —
game descriptions and notes stay in the system font so they read at real
resolution, the same reason grimoire kept its prose in a real serif.

---

## Themes

Four backdrops, plus a no-backdrop "bare console" fallback. Picking one in
Settings sets `data-theme` on `<html>`; `themes.css` swaps the chrome ramp
and the frame sprites to match.

| Theme | Hue (measured) | Chrome |
|---|---|---|
| `arcade` | ~291° | Violet-magenta (default) |
| `forest` | ~228° | Cool blue-grey |
| `cavern` | ~44° | Warm brown |
| `dusk` | ~332° | Mauve-pink |
| `bare` | — | Falls back to `:root`, no backdrop |

**Adding a theme:** drop a layered scene pack into `icon-packs/`, add it to
`SCENES` in `scripts/build-assets.py` (source pack + ordered layer list +
whether it tiles) *and* to `THEMES` in `web/src/hooks/useTheme.tsx`, then
re-run the build. Hue, chrome ramp, frames, and sky colour are all
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
python3 scripts/build-assets.py     # frames, sprites, scenes, themes.css, favicons
python3 scripts/fetch-gameicons.py  # web/src/lib/gameicons.ts
python3 scripts/fetch-fonts.py      # web/public/assets/fonts/ + pixel/fonts.css
```

`build-assets.py` reads from `icon-packs/`, which is **gitignored on
purpose** — see `ATTRIBUTIONS.md` for the four download links (all CC0,
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
