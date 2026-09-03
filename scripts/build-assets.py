#!/usr/bin/env python3
"""Derive Backhog's shipped pixel assets from the upstream art packs.

The packs live in `icon-packs/` and are *not* committed — see
ATTRIBUTIONS.md for where to get them. What ships is what this script
produces: nine-slice frames recoloured into each theme's console-plastic
palette, the arcade sprite sheet, the parallax scene layers, and the
generated themes.css that ties a backdrop to a whole retinted interface.

    python3 scripts/build-assets.py

Everything is nearest-neighbour and integer-scaled; pixel art is never
resampled. Recolour maps are exhaustive — an unmapped colour is a build
error, so an upstream tweak surfaces instead of shipping half-recoloured
frames.

The recipe follows the grimoire project's asset pipeline:

- A theme is a backdrop. Each scene's hue is measured from its own art
  and applied to the chrome ladder, so a retinted chrome is exactly as
  legible as the default: only the hue moves, never the contrast.
- The ladder has two halves, derived two different ways, because they are
  doing two different jobs. See CHROME below.
- What never changes is the game: coin-gold action buttons, the danger
  red, and the per-game accent all stay constant, because the chrome is
  the room and the game is the point.
"""

import collections
import colorsys
import math
import os
import shutil
import sys
import tempfile
import zipfile

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import pngkit  # noqa: E402

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
PACKS = os.path.join(ROOT, "icon-packs")
OUT = os.path.join(ROOT, "web", "public", "assets")
THEMES_CSS = os.path.join(ROOT, "web", "src", "pixel", "themes.css")

# ---------------------------------------------------------------- palettes

# The ink ladder, in two halves.
#
# 950-700 are SURFACES: panel fills, inset fields, the console plastic the
# frame sprites are recoloured from. They are HLS lightnesses, because a
# sprite is recoloured by lightness and the token has to name the same
# colour the sprite baked in.
#
# 600-100 are TEXT, and they are NOT lightnesses. HLS lightness is not
# perceptual: at L=0.27 a near-neutral grey and a saturated purple differ
# by 2x in actual luminance, so a "fixed lightness ladder" silently made
# the saturated themes' text 2-4x darker than the neutral one's. Arcade's
# body text landed at 1.7:1 against its own panel — unreadable, and the
# whole reason this file now solves for contrast instead.
#
# Each text step names the contrast ratio it must hit against that theme's
# own panel fill (--c-800). Solving per theme is what actually makes the
# promise true: every theme's ink-400 is the same *readability*, whatever
# hue it wears.
SURFACE_L = {
    "950": 0.035, "900": 0.059, "850": 0.072,
    "800": 0.084, "750": 0.098, "700": 0.112,
}

# The frame sprites' own edge step. It is a surface — it has to match the
# bevel the sprite baked in — so it keeps the old 600 lightness even
# though the 600 *token* is now text.
LINE_L = 0.145

# WCAG contrast against --c-800. 4.5 is the AA floor for body text; 600 is
# below it on purpose, being decorative only ("(optional)", em-dashes).
TEXT_CONTRAST = {
    "600": 3.5, "500": 4.8, "400": 6.5,
    "300": 8.5, "200": 11.0, "100": 14.5,
}


def _lin(c):
    c /= 255
    return c / 12.92 if c <= 0.03928 else ((c + 0.055) / 1.055) ** 2.4


def luminance(rgb):
    """WCAG relative luminance of an (r, g, b) 0-255 triple."""
    r, g, b = rgb
    return 0.2126 * _lin(r) + 0.7152 * _lin(g) + 0.0722 * _lin(b)


def contrast(a, b):
    ya, yb = luminance(a), luminance(b)
    hi, lo = max(ya, yb), min(ya, yb)
    return (hi + 0.05) / (lo + 0.05)


def _rgb(hue, lightness, sat):
    r, g, b = colorsys.hls_to_rgb(hue, lightness, sat)
    return round(r * 255), round(g * 255), round(b * 255)


def solve_lightness(hue, sat, bg, target):
    """The ink closest to `bg` that still hits `target` contrast against it.

    Contrast changes monotonically with lightness on either side of the
    background, so a bisection is exact — but which way it runs depends on
    which side the ink is on. Against a dark ground the ink is lighter than
    the background and contrast rises with lightness; against a light one it
    is darker and contrast rises as lightness falls. `lit` is the extreme the
    search pushes toward (white or black) and `near` is the end that fails,
    so the same loop serves both.

    If the hue cannot reach the target even at the extreme (a deeply
    saturated hue has a luminance ceiling), the saturation is walked down
    until it can — a slightly paler tint beats an illegible one.
    """
    # Mid-grey in luminance terms, not in lightness: a light ground is one
    # that white cannot make enough contrast against.
    on_light = luminance(bg) > 0.18

    while sat >= 0:
        near, lit = (1.0, 0.0) if on_light else (0.0, 1.0)
        if contrast(_rgb(hue, lit, sat), bg) < target:
            sat = round(sat - 0.02, 2)
            continue
        lo, hi = near, lit
        for _ in range(40):
            mid = (lo + hi) / 2
            if contrast(_rgb(hue, mid, sat), bg) < target:
                lo = mid
            else:
                hi = mid
        return _rgb(hue, hi, sat)
    # Unreachable at any saturation: fall back to the extreme we were pushing
    # toward, which is the most contrast this ground can carry.
    return (0, 0, 0) if on_light else (255, 255, 255)

# Bare console plastic: a cool, almost-neutral grey for when no backdrop
# is picked. The themes are the same ladder wearing a scene's colours.
BASE_HUE, BASE_SAT = 228 / 360, 0.10

# Chrome takes a scene's colour, not its intensity.
MAX_THEME_SAT = 0.24

# Coin gold — the one accent that never re-tints. The primary action
# reads the same in every theme, the way an arcade's coin slot always
# looks like a coin slot.
GOLD = {"pale": "#ffe9a8", "bright": "#ffd071", "mid": "#c9a227", "dim": "#8a7024"}

# Danger red, from the arcade sheet's own DB16 red.
RED = {"pale": "#ea7a7c", "bright": "#d04648", "mid": "#992e30", "dim": "#6b1f21"}


def ramp(hue, sat):
    """The full ink ladder at a given hue. Returns {step: "#rrggbb"}.

    Surfaces first, because the text half is solved *against* one of them
    (--c-800, the panel fill, which is where most text in the app sits).
    """
    out = {}
    for step, lightness in SURFACE_L.items():
        out[step] = _rgb(hue, lightness, sat)
    out["line"] = _rgb(hue, LINE_L, sat)

    bg = out["800"]
    for step, target in TEXT_CONTRAST.items():
        out[step] = solve_lightness(hue, sat, bg, target)

    return {k: "#%02x%02x%02x" % v for k, v in out.items()}


# ---------------------------------------------------------------- unpacking

# Each theme is a backdrop pack. Layers are listed FRONT first — 0 is the
# foreground, the last is the sky — and the layer files are normalised to
# that order at copy time. `tile` says whether the art repeats sideways
# (cave is one painted chamber, everything else tiles).
SCENES = {
    "arcade": {
        "label": "Neon arcade",
        "note": "The street outside the arcade",
        "pack": ("city.zip", "CityBackground"),
        "layers": [
            "sCityClose.png", "sCityCarsClose.png", "sCityMid.png",
            "sCityCarsFar.png", "sCityFar.png", "sCitySky.png",
        ],
        "tile": True,
        "height": 144,
    },
    "forest": {
        "label": "Night forest",
        "note": "Silhouettes between the trees",
        "pack": ("forest/", ""),
        "layers": ["forest-front_2.png", "forest-middle_0.png", "forest-Back_1.png"],
        "tile": True,
        "height": 384,
    },
    "cavern": {
        "label": "Cavern",
        "note": "A lantern-lit dig",
        "pack": ("cave.zip", "Seamless Parallax Cave Background"),
        "layers": [
            "Cave Front.png", "Cave Mid.png", "Cave Far.png", "Cave Back.png",
        ],
        "tile": True,
        "height": 800,
    },
    "dusk": {
        "label": "Dusk ridge",
        "note": "Mountains before night",
        "pack": ("mountain.zip", "parallax_mountain_pack/layers"),
        "layers": [
            "parallax-mountain-foreground-trees.png", "parallax-mountain-trees.png",
            "parallax-mountain-mountains.png", "parallax-mountain-montain-far.png",
            "parallax-mountain-bg.png",
        ],
        "tile": True,
        "height": 160,
    },
}


def unpack(work):
    """Extract every pack into `work`, returning roots we read from."""
    def need(name):
        p = os.path.join(PACKS, name)
        if not os.path.exists(p):
            sys.exit(f"missing pack: {p}\n"
                     f"Place the source art packs in icon-packs/ and re-run.")
        return p

    ui = os.path.join(work, "ui")
    os.makedirs(ui, exist_ok=True)
    with zipfile.ZipFile(need("PixelUIpack.zip")) as z:
        z.extractall(ui)

    scenes = {}
    for key, scene in SCENES.items():
        name, sub = scene["pack"]
        if name.endswith(".zip"):
            d = os.path.join(work, "scene-" + key)
            with zipfile.ZipFile(need(name)) as z:
                z.extractall(d)
            scenes[key] = os.path.join(d, sub)
        else:
            scenes[key] = need(name)

    return {
        "frames": os.path.join(ui, "9-Slice"),
        "scenes": scenes,
        "arcade": need(os.path.join("arcade", "arcade64_sheet.png")),
    }


# ------------------------------------------------------------------ frames
#
# Kenney's pixel UI gives us flat bevel frames in four families; Backhog
# recolours three of them. Every map is exhaustive — the sprites carry
# three or four colours and all of them are listed.
#
#   space.png          panel   2px edge, 1px inner highlight
#   space_inlay.png    field   inset panel, darker than its parent
#   Colored/blue.png   button  light top-left bevel, dark bottom-right
#   Colored/grey.png   soft button / active panel
#   Outline/blue.png   chip    fill with a coloured ring
#
# The recoloured output is what the CSS nine-slices; the panel's fill
# pixel IS the --c-800 token, so a framed surface and a flat one sit side
# by side without a seam.

def themed_frames(sprite, c, outdir):
    """The frames that carry a theme's colour, emitted into `outdir`."""
    os.makedirs(outdir, exist_ok=True)

    def emit(name, img):
        pngkit.write(os.path.join(outdir, name + ".png"), img)

    # Panel — the chrome. Slot space, wearing the theme's plastic.
    emit("panel", sprite("space").recolour({
        "#6486af": c["800"],   # fill
        "#7cadc5": c["600"],   # edge
        "#a5cae0": c["500"],   # bevel highlight
    }))
    # Active panel — nav hover, selected list rows. The soft grey shape,
    # one step brighter than the panel.
    emit("panel-active", sprite("Colored/grey").recolour({
        "#eeeeee": c["700"],
        "#ffffff": c["400"],
        "#d5d5d5": c["800"],
        "#aaaaaa": c["950"],
    }))
    # Field — text sunk into the panel.
    emit("field", sprite("space_inlay").recolour({
        "#c9d0d9": c["900"],   # fill
        "#b6bec9": c["700"],   # inset edge
    }))
    # Focus keeps coin gold, which never changes — but its fill is themed.
    emit("field-focus", sprite("space_inlay").recolour({
        "#c9d0d9": c["900"],
        "#b6bec9": GOLD["mid"],
    }))
    # Chip — the small pills. Sits on panels, so it is one step lighter.
    emit("chip", sprite("Outline/blue").recolour({
        "#eeeeee": c["700"],   # fill
        "#1ea7e1": c["600"],   # ring
        "#d5d5d5": c["500"],
        "#aaaaaa": c["500"],
    }))
    emit("chip-active", sprite("Outline/blue_pressed").recolour({
        "#eeeeee": c["600"],
        "#1ea7e1": GOLD["mid"],
        "#d5d5d5": c["400"],
    }))
    # Progress bar track.
    emit("bar", sprite("Colored/grey").recolour({
        "#eeeeee": c["950"],
        "#ffffff": c["600"],
        "#d5d5d5": c["600"],
        "#aaaaaa": c["850"],
    }))


def build_frames(frames, themes):
    def sprite(name):
        return pngkit.read(os.path.join(frames, f"{name}.png"))

    os.makedirs(os.path.join(OUT, "ui"), exist_ok=True)

    def emit(name, img):
        pngkit.write(os.path.join(OUT, "ui", name + ".png"), img)

    # Coin-gold button and its pressed state — shared by every theme.
    emit("button-gold", sprite("Colored/blue").recolour({
        "#1ea7e1": GOLD["bright"],   # fill
        "#35baf3": GOLD["pale"],     # top-left bevel
        "#1989b8": GOLD["mid"],      # bottom-right bevel
        "#166e93": GOLD["dim"],      # drop shadow
    }))
    emit("button-gold-pressed", sprite("Colored/blue_pressed").recolour({
        "#1ea7e1": GOLD["mid"],
        "#35baf3": GOLD["bright"],
        "#1989b8": GOLD["dim"],
        "#166e93": GOLD["dim"],
    }))
    # Danger red, from the arcade sheet's own red.
    emit("button-danger", sprite("Colored/blue").recolour({
        "#1ea7e1": RED["bright"],
        "#35baf3": RED["pale"],
        "#1989b8": RED["mid"],
        "#166e93": RED["dim"],
    }))
    emit("button-danger-pressed", sprite("Colored/blue_pressed").recolour({
        "#1ea7e1": RED["mid"],
        "#35baf3": RED["bright"],
        "#1989b8": RED["dim"],
        "#166e93": RED["dim"],
    }))

    # The default (bare plastic) chrome, then one set per theme.
    base = ramp(BASE_HUE, BASE_SAT)
    themed_frames(sprite, base, os.path.join(OUT, "ui"))
    for key, theme in themes.items():
        themed_frames(sprite, theme["chrome"], os.path.join(OUT, "ui", key))


# ------------------------------------------------------------------ arcade
#
# The arcade sheet is proper DB16 pixel art: joysticks and buttons in the
# palette the arcade city scene uses. We ship it un-recoloured — the reds
# and creams are the point — as a grid of 32x64 cells (the square button
# pads to 32x32), which the CSS positions by cell index like grimoire's
# 32px sheet.

SPRITES = [  # (name, x, y, w, h) crops from arcade64_sheet.png
    ("stick",      2,  66, 28, 58),
    ("stick-alt", 38,  66, 28, 58),
    ("button",   191, 126, 29, 62),
    ("button-hi",226, 126, 29, 62),
    ("button-lo",260, 126, 29, 62),
    ("pad",      287, 132, 33, 28),
    ("ball",     193, 127, 25, 25),
]
CELL_W, CELL_H = 32, 64


def build_arcade(src):
    sheet = pngkit.read(src)
    out = pngkit.Image(CELL_W * len(SPRITES), CELL_H)
    index = {}
    for i, (name, x, y, w, h) in enumerate(SPRITES):
        cell = pngkit.Image(CELL_W, CELL_H)
        # tall art centres in the cell; the small ball mark sits at the
        # top so a half-height box still shows all of it
        py = (CELL_H - h) // 2 if h > 32 else 0
        cell.paste(sheet.crop(x, y, w, h), (CELL_W - w) // 2, py)
        out.paste(cell, i * CELL_W, 0)
        index[name] = i
    os.makedirs(os.path.join(OUT, "sprites"), exist_ok=True)
    pngkit.write(os.path.join(OUT, "sprites", "arcade.png"), out)

    # The favicon is NOT cut from this sheet. It used to be a 16x16 crop of
    # the arcade button's red crown, which shipped as a plain red square
    # with four dark pixels in it and said nothing about the product (and
    # was 16px despite the -32 in its name). The mark is Backhog's own
    # now — see scripts/build-mark.py, which owns favicon-32.png,
    # icon-192.png and sprites/hog.png. Do not write them from here.
    return index


# ------------------------------------------------------------------ scenes

def build_scenes(sources):
    """Copy each scene's layers front-to-back and read its colour back out."""
    themes = {}
    for key, scene in SCENES.items():
        src = sources[key]
        dst = os.path.join(OUT, "scenes", key)
        os.makedirs(dst, exist_ok=True)
        layers = []
        for i, name in enumerate(scene["layers"]):
            shutil.copyfile(os.path.join(src, name), os.path.join(dst, f"{i}.png"))
            layers.append(f"{i}.png")

        hue, sat = scene_hue(dst, layers)
        themes[key] = {
            **scene,
            "count": len(layers),
            "hue": hue,
            "sat": sat,
            "chrome": ramp(hue, sat),
            "sky": top_colour(os.path.join(dst, layers[-1])),
        }
        print(f"  scene {key:8s} {len(layers)} layers  "
              f"hue {hue * 360:5.1f}°  sat {sat:.2f}  sky {themes[key]['sky']}")
    return themes


def scene_hue(d, layers):
    """A scene's representative hue and saturation.

    Hue is circular, so it is averaged as a unit vector rather than as a
    number. Each pixel votes in proportion to its own saturation, so flat
    greys do not drown out the colour that gives a scene its character.
    """
    x = y = weight = 0.0
    sats = []
    for name in layers:
        img = pngkit.read(os.path.join(d, name))
        counts = collections.Counter()
        for i in range(0, len(img.px), 4):
            if img.px[i + 3] < 128:
                continue
            counts[(img.px[i], img.px[i + 1], img.px[i + 2])] += 1
        for (r, g, b), n in counts.items():
            h, l, s = colorsys.rgb_to_hls(r / 255, g / 255, b / 255)
            if s < 0.06 or l < 0.04 or l > 0.95:
                continue
            w = n * s
            x += math.cos(h * 2 * math.pi) * w
            y += math.sin(h * 2 * math.pi) * w
            weight += w
            sats.append((s, n))
    if not weight:
        return BASE_HUE, BASE_SAT
    total = sum(n for _, n in sats)
    hue = (math.atan2(y, x) / (2 * math.pi)) % 1.0
    sat = min(sum(s * n for s, n in sats) / total, MAX_THEME_SAT)
    return hue, sat


def top_colour(path):
    """The dominant colour along the top edge of the backmost layer."""
    img = pngkit.read(path)
    counts = collections.Counter()
    for yy in range(min(8, img.h)):
        for xx in range(img.w):
            r, g, b, a = img.at(xx, yy)
            if a >= 128:
                counts[(r, g, b)] += 1
    if not counts:
        return "#000000"
    r, g, b = counts.most_common(1)[0][0]
    return "#%02x%02x%02x" % (r, g, b)


# ------------------------------------------------------------------ themes

THEMES_HEAD = """/* Per-backdrop themes. Generated by scripts/build-assets.py — do not edit.
//
// Each scene's hue is measured from its own art and applied to the ink
// ladder. Surfaces (950-700, --c-line) are lightnesses, because the frame
// sprites are recoloured from them and the token has to name the colour
// the sprite baked in. Text (600-100) is solved for a WCAG contrast ratio
// against that theme's own panel fill, so ink-400 is the same
// *readability* in every theme rather than the same HLS number — which is
// not the same thing, and used to leave the saturated themes' body text
// at 1.7:1.
//
// What changes is the room — the console plastic, the inset fields, the
// backdrop's own sky. What does not change is the game: coin-gold
// buttons, danger red and the per-game accent stay put, because the
// accent means something and a backdrop should not restate it. */
"""


def write_themes(themes):
    out = [THEMES_HEAD]
    for key, theme in themes.items():
        c = theme["chrome"]
        steps = "\n".join(
            f"  --c-{k}: {v};" for k, v in c.items()
            if k not in ("line",)
        )
        steps += f"\n  --c-line: {c['line']};"
        out.append(f"""
html[data-theme="{key}"] {{
{steps}
  --scene-sky: {theme['sky']};
  --scene-layers: {theme['count']};
  --scene-height: {theme['height']};
  --frame-panel: url("/assets/ui/{key}/panel.png");
  --frame-panel-active: url("/assets/ui/{key}/panel-active.png");
  --frame-field: url("/assets/ui/{key}/field.png");
  --frame-field-focus: url("/assets/ui/{key}/field-focus.png");
  --frame-chip: url("/assets/ui/{key}/chip.png");
  --frame-chip-active: url("/assets/ui/{key}/chip-active.png");
  --frame-bar: url("/assets/ui/{key}/bar.png");
}}""")
    open(THEMES_CSS, "w").write("\n".join(out) + "\n")
    print(f"  themes.css: {len(themes)} themes -> {os.path.relpath(THEMES_CSS, ROOT)}")


# --------------------------------------------------------------------- main

def main():
    with tempfile.TemporaryDirectory() as work:
        src = unpack(work)
        print("scenes:")
        themes = build_scenes(src["scenes"])
        print("frames:")
        build_frames(src["frames"], themes)
        print("arcade:")
        index = build_arcade(src["arcade"])
        print("  sprites:", ", ".join(f"{k}@{v}" for k, v in index.items()))
        print("themes:")
        write_themes(themes)
    print(f"\nwrote {OUT}")


if __name__ == "__main__":
    main()
