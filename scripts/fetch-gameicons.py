#!/usr/bin/env python3
"""Regenerate web/src/lib/gameicons.ts from game-icons.net.

Backhog uses two icon systems. The arcade sheet's pixel sprites carry the
hero marks; these vectors carry everything that must be small or must take
a colour — every affordance in the chrome, and the small nouns (trophy,
dice, hourglass) that would mush at 16px as pixel art. They replace
lucide-react entirely: one system, one attribution file.

The upstream SVGs are a black background rect plus a white glyph path. We
keep only the glyph geometry — the `d` strings — so the shipped module is
pure path data with no markup to inject, and the rendered icon inherits
`currentColor`.

Glyphs do NOT all fill their 512 viewBox. game-icons' `badges/` set in
particular draws a small mark in the top-left quadrant, so rendering every
icon at a flat `0 0 512 512` made those come out at roughly a quarter of
the size of their neighbours — the plus on "Add game", the queue's
reorder arrows, the star on a rating. So each icon also ships a viewBox
measured from its own geometry and normalised to a common optical fill,
which is what makes `size-4` mean the same thing for all of them.

    python3 scripts/fetch-gameicons.py

Icons are CC BY 3.0; the per-icon author is recorded alongside the path so
ATTRIBUTIONS.md can be checked against what actually ships.
"""

import math
import os
import re
import sys
import urllib.request

BASE = "https://raw.githubusercontent.com/game-icons/icons/master"
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.join(ROOT, "web", "src", "lib", "gameicons.ts")
ATTRIB = os.path.join(ROOT, "ATTRIBUTIONS.md")

# The credits table in ATTRIBUTIONS.md lives between these markers and is
# rewritten from ICONS on every run. It used to be maintained by hand and
# had drifted 38 icons out of date — which for CC BY art is a licence
# problem, not a tidiness one.
ATTRIB_START = "<!-- BEGIN generated icon credits -->"
ATTRIB_END = "<!-- END generated icon credits -->"

# backhog name -> upstream author/icon, or (author/icon, rotation) when the
# set has a glyph in only one orientation. Names follow the lucide icons
# they replaced (chevron-down, trash, x, ...) so call sites read naturally.
ICONS = {
    # chrome affordances
    "arrow-left":    "delapouite/previous-button",
    "check":         "delapouite/check-mark",
    "check-circle":  "delapouite/check-mark",
    "chevron-down":  "badges/arrow-down",
    "chevron-up":    "badges/arrow-up",
    # "to top"/"to bottom" have to be *visibly* different from the
    # single-step arrows next to them in the queue; both used to be
    # badges/arrow-*, so the two controls were indistinguishable. upgrade
    # is a stacked triple chevron, and there is no downgrade to pair with
    # it, so the down one is the same glyph turned over.
    "chevrons-down": ("delapouite/upgrade", 180),
    "chevrons-up":   "delapouite/upgrade",
    "download":      "delapouite/cloud-download",
    "external-link": "delapouite/window",
    "grab":          "lorc/grab",
    "layout-grid":   "delapouite/window-bars",
    "log-out":       "delapouite/exit-door",
    "pencil":        "delapouite/pencil",
    "plus":          "badges/plus",
    "refresh":       "delapouite/clockwise-rotation",
    "rows":          "delapouite/hamburger-menu",
    "search":        "lorc/magnifying-glass",
    "settings":      "lorc/gears",
    "sliders":       "delapouite/settings-knobs",
    "trash":         "delapouite/trash-can",
    "x":             "badges/multiply",
    "lock":          "lorc/padlock",
    "hand":          "lorc/hand",
    # nouns
    "brush":         "delapouite/paint-brush",
    "broom":         "delapouite/broom",
    "bubbles":       "lorc/bubbles",
    "building":      "badges/building",
    "calendar":      "delapouite/calendar",
    # NOT badges/blank: its ring is a stroked <circle>, and the only
    # <path> in that file is a literal "MZ". It passed the fetch, shipped
    # as an empty glyph, and the backlog status badge drew nothing at all.
    "circle-dashed": "delapouite/circle",
    "clock":         "lorc/stopwatch",
    "timer":         "lorc/stopwatch",
    "dices":         "delapouite/rolling-dices",
    "door-open":     "lorc/doorway",
    "droplet":       "lorc/drop",
    "film":          "delapouite/film-strip",
    "flag":          "delapouite/checkered-flag",
    "gamepad":       "delapouite/gamepad",
    "gauge":         "delapouite/speedometer",
    "gas-mask":      "lorc/gas-mask",
    "gift":          "delapouite/present",
    "hammer-drop":   "lorc/hammer-drop",
    "history":       "lorc/clockwork",
    "hourglass":     "lorc/hourglass",
    "joystick":      "delapouite/joystick",
    "amphora":       "delapouite/amphora",
    "bookshelf":     "delapouite/bookshelf",
    "fossil":        "lorc/fossil",
    "layers":        "lorc/cubes",
    "lifebuoy":      "delapouite/buoy",
    "lightning-storm": "lorc/lightning-storm",
    "list-checks":   "delapouite/checklist",
    "list-ordered":  "lorc/journey",
    "list-tree":     "lorc/checkbox-tree",
    "meteor-impact": "lorc/meteor-impact",
    "minus":         "badges/minus",
    "monitor":       "delapouite/game-console",
    "mountain":      "lorc/mountains",
    "mayan-pyramid": "delapouite/mayan-pyramid",
    "play":          "guard13007/play-button",
    "shovel":        "lorc/spade",
    "small-fire":    "lorc/small-fire",
    "sparkles":      "delapouite/sparkles",
    "star":          "badges/star",
    "stone-tablet":  "lorc/stone-tablet",
    "swords":        "lorc/crossed-swords",
    "tags":          "delapouite/price-tag",
    "target":        "lorc/targeting",
    "time-trap":     "lorc/time-trap",
    "tooth":         "lorc/tooth",
    "trophy":        "lorc/trophy",
    "tv":            "delapouite/tv",
    "vintage-robot": "lorc/vintage-robot",
    "wooden-door":   "lorc/wooden-door",
    "zap":           "lorc/focused-lightning",
    # series mastery
    "book-pile":       "delapouite/book-pile",
    "linked-rings":    "lorc/linked-rings",
    "scroll-unfurled": "lorc/scroll-unfurled",
    "imperial-crown":  "delapouite/imperial-crown",
    "sprint":          "lorc/sprint",
    "cycle":           "lorc/cycle",
    "full-folder":     "delapouite/full-folder",
    # diversity and platform mastery
    "pizza-slice":   "delapouite/pizza-slice",
    "compass":       "lorc/compass",
    "dust-cloud":    "lorc/dust-cloud",
    "family-tree":   "delapouite/family-tree",
    "cuckoo-clock":  "delapouite/cuckoo-clock",
    "mushroom-gills": "lorc/mushroom-gills",
    "knapsack":      "lorc/knapsack",
    "footprint":     "lorc/footprint",
    "clover":        "lorc/clover",
    # easter eggs
    "night-sleep":   "delapouite/night-sleep",
    "eyeball":       "lorc/eyeball",
    "keyboard":      "delapouite/keyboard",
    "whirlwind":     "lorc/whirlwind",
    # statuses
    "ban":           "lorc/interdiction",
    "x-circle":      "lorc/interdiction",
}

# The 512x512 background rect every upstream icon opens with.
BACKDROP = re.compile(r"^M0 0h512v512H0z$")
PATH_D = re.compile(r"<path[^>]*\sd=\"([^\"]+)\"")

# Fraction of the viewBox the glyph's longest edge should span. 0.92 is the
# median fill of the unnormalised set, so the icons that were already right
# keep their size and only the outliers move.
FILL = 0.92

_NUM = re.compile(r"[-+]?(?:\d*\.\d+|\d+)(?:[eE][-+]?\d+)?")
_CMD = re.compile(r"([MmLlHhVvCcSsQqTtAaZz])")
_ARGC = {"M": 2, "L": 2, "H": 1, "V": 1, "C": 6, "S": 4, "Q": 4, "T": 2, "A": 7, "Z": 0}


def _bezier(p, t):
    """De Casteljau, so curves are measured where they actually go."""
    while len(p) > 1:
        p = [(a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t) for a, b in zip(p, p[1:])]
    return p[0]


def _arc_points(x0, y0, rx, ry, phi, large, sweep, x1, y1, steps=24):
    """Endpoint-parameterised arc -> sampled points (F.6.5 in the SVG spec).

    Sampled rather than bounded by its endpoints: an arc bulges away from
    the chord between them, and a bbox that ignored the bulge would clip
    the glyph once it is scaled up to fill the box.
    """
    if rx == 0 or ry == 0:
        return [(x1, y1)]
    rad = math.radians(phi)
    cos_p, sin_p = math.cos(rad), math.sin(rad)
    dx2, dy2 = (x0 - x1) / 2, (y0 - y1) / 2
    x1p, y1p = cos_p * dx2 + sin_p * dy2, -sin_p * dx2 + cos_p * dy2
    rx, ry = abs(rx), abs(ry)
    lam = (x1p / rx) ** 2 + (y1p / ry) ** 2
    if lam > 1:
        rx, ry = rx * math.sqrt(lam), ry * math.sqrt(lam)
    num = rx * rx * ry * ry - rx * rx * y1p * y1p - ry * ry * x1p * x1p
    den = rx * rx * y1p * y1p + ry * ry * x1p * x1p
    co = math.sqrt(max(0.0, num / den)) if den else 0.0
    if large == sweep:
        co = -co
    cxp, cyp = co * rx * y1p / ry, -co * ry * x1p / rx
    cx = cos_p * cxp - sin_p * cyp + (x0 + x1) / 2
    cy = sin_p * cxp + cos_p * cyp + (y0 + y1) / 2

    def ang(ux, uy, vx, vy):
        d = math.hypot(ux, uy) * math.hypot(vx, vy)
        if d == 0:
            return 0.0
        a = math.acos(max(-1.0, min(1.0, (ux * vx + uy * vy) / d)))
        return -a if ux * vy - uy * vx < 0 else a

    t1 = ang(1, 0, (x1p - cxp) / rx, (y1p - cyp) / ry)
    dt = ang((x1p - cxp) / rx, (y1p - cyp) / ry, (-x1p - cxp) / rx, (-y1p - cyp) / ry)
    if not sweep and dt > 0:
        dt -= 2 * math.pi
    elif sweep and dt < 0:
        dt += 2 * math.pi
    pts = []
    for i in range(1, steps + 1):
        th = t1 + dt * i / steps
        pts.append((cos_p * rx * math.cos(th) - sin_p * ry * math.sin(th) + cx,
                    sin_p * rx * math.cos(th) + cos_p * ry * math.sin(th) + cy))
    return pts


def bbox(paths):
    """Tight (minx, miny, maxx, maxy) over a glyph's path data."""
    xs, ys = [], []
    for d in paths:
        cx = cy = sx = sy = 0.0
        prev_ctrl = None
        prev_cmd = ""
        tokens = [t for t in _CMD.split(d) if t.strip()]
        i = 0
        while i < len(tokens):
            cmd = tokens[i]
            i += 1
            if not _CMD.fullmatch(cmd):
                continue
            up, rel = cmd.upper(), cmd.islower()
            args = []
            if i < len(tokens) and not _CMD.fullmatch(tokens[i]):
                args = [float(n) for n in _NUM.findall(tokens[i])]
                i += 1
            n = _ARGC[up]
            if n == 0:
                cx, cy = sx, sy
                prev_cmd = up
                continue
            for j in range(0, len(args) - n + 1, n):
                a = args[j:j + n]
                x0, y0 = cx, cy
                if up == "H":
                    cx = cx + a[0] if rel else a[0]
                elif up == "V":
                    cy = cy + a[0] if rel else a[0]
                elif up == "A":
                    nx, ny = (cx + a[5], cy + a[6]) if rel else (a[5], a[6])
                    for px, py in _arc_points(x0, y0, a[0], a[1], a[2], int(a[3]), int(a[4]), nx, ny):
                        xs.append(px)
                        ys.append(py)
                    cx, cy = nx, ny
                else:
                    pts = [(a[k], a[k + 1]) for k in range(0, n, 2)]
                    if rel:
                        pts = [(cx + px, cy + py) for px, py in pts]
                    if up in ("S", "T"):
                        # Reflected control point from the previous curve.
                        refl = (2 * cx - prev_ctrl[0], 2 * cy - prev_ctrl[1]) \
                            if prev_ctrl and prev_cmd in ("C", "S", "Q", "T") else (cx, cy)
                        pts = [refl] + pts
                    if up in ("C", "S", "Q", "T"):
                        curve = [(x0, y0)] + pts
                        for k in range(1, 17):
                            px, py = _bezier(curve, k / 16)
                            xs.append(px)
                            ys.append(py)
                        prev_ctrl = pts[-2] if len(pts) >= 2 else pts[-1]
                    else:
                        for px, py in pts:
                            xs.append(px)
                            ys.append(py)
                        prev_ctrl = None
                    cx, cy = pts[-1]
                    if up == "M" and j == 0:
                        sx, sy = cx, cy
                xs.append(cx)
                ys.append(cy)
                prev_cmd = up
    if not xs:
        sys.exit("empty glyph")
    return min(xs), min(ys), max(xs), max(ys)


def view_box(paths):
    """A square viewBox centred on the glyph, sized so it fills FILL of it."""
    x0, y0, x1, y1 = bbox(paths)
    side = max(x1 - x0, y1 - y0) / FILL
    cx, cy = (x0 + x1) / 2, (y0 + y1) / 2
    return f"{cx - side / 2:.1f} {cy - side / 2:.1f} {side:.1f} {side:.1f}"


def fetch(slug):
    with urllib.request.urlopen(f"{BASE}/{slug}.svg", timeout=20) as r:
        return r.read().decode("utf-8")


def glyph(svg, slug):
    """The glyph paths, or a hard failure.

    Only <path> geometry ships, so an upstream icon that draws itself with
    <circle>/<rect>/<line> would come through empty or partial. That is not
    hypothetical: badges/blank is two <circle> elements plus <path d="MZ">,
    and it shipped for months as a status badge that drew nothing. Fail
    here instead — a missing icon should break the build, not the UI.
    """
    paths = [d for d in PATH_D.findall(svg) if not BACKDROP.match(d.strip())]
    paths = [d for d in paths if len(d.strip()) > 8]
    if not paths:
        sys.exit(f"{slug}: no usable glyph path (drawn with non-path shapes?)")
    x0, y0, x1, y1 = bbox(paths)
    if x1 - x0 < 1 or y1 - y0 < 1:
        sys.exit(f"{slug}: glyph has no area ({x0},{y0})-({x1},{y1})")
    return paths


def main():
    icons, credits, boxes = {}, {}, {}
    for name, entry in sorted(ICONS.items()):
        slug, rotation = entry if isinstance(entry, tuple) else (entry, 0)
        paths = glyph(fetch(slug), slug)
        icons[name] = paths
        credits[name] = slug
        box = {"v": view_box(paths)}
        if rotation:
            box["r"] = rotation
        boxes[name] = box
        print(f"  {name:14s} <- {slug:38s} viewBox {box['v']}"
              + (f"  rot {rotation}" if rotation else ""))

    def js_obj(d, fmt):
        rows = ",\n".join(f'  "{k}": {fmt(v)}' for k, v in d.items())
        return "{\n" + rows + ",\n}"

    body = f"""// Generated by scripts/fetch-gameicons.py — do not edit by hand.
//
// Glyph geometry from game-icons.net (CC BY 3.0), by the authors named in
// CREDITS below. Only the path data ships: the <Gi> component builds the
// <svg> around it, so every icon inherits currentColor and stays crisp at
// any size.

/** name -> array of SVG path `d` strings, in the upstream 512 coordinate
 *  space. Render them through the viewBox in GAME_ICON_BOX, not 0 0 512 512. */
export const GAME_ICONS = Object.freeze({js_obj(icons, lambda v: "[" + ", ".join(f'"{p}"' for p in v) + "]")});

/** name -> {{ v: viewBox measured from the glyph, r?: rotation in degrees }}.
 *  The viewBox is what makes every icon the same optical size; see FILL in
 *  scripts/fetch-gameicons.py. */
export const GAME_ICON_BOX = Object.freeze({js_obj(boxes, lambda v: "{ " + ", ".join(f'{k}: ' + (f'"{x}"' if isinstance(x, str) else str(x)) for k, x in v.items()) + " }")});

/** name -> "author/icon" upstream, for attribution. */
export const CREDITS = Object.freeze({js_obj(credits, lambda v: f'"{v}"')});

/** Every valid icon name. */
export type GiName = keyof typeof GAME_ICONS;
"""
    open(OUT, "w").write(body)
    size = os.path.getsize(OUT)
    print(f"\nwrote {OUT} ({size // 1024}KB, {len(icons)} icons)")
    write_credits(credits)


def write_credits(credits):
    """Rewrite the ATTRIBUTIONS.md table so it always matches what ships."""
    names = sorted(credits)
    half = (len(names) + 1) // 2
    left, right = names[:half], names[half:] + [None] * (half - len(names[half:]))
    rows = ["| Backhog name | Upstream | | Backhog name | Upstream |",
            "|---|---|---|---|---|"]
    for a, b in zip(left, right):
        rhs = f"`{b}` | {credits[b]}" if b else " | "
        rows.append(f"| `{a}` | {credits[a]} | | {rhs} |")
    table = "\n".join(rows)

    md = open(ATTRIB).read()
    i, j = md.find(ATTRIB_START), md.find(ATTRIB_END)
    if i == -1 or j == -1:
        sys.exit(f"{ATTRIB}: missing {ATTRIB_START} / {ATTRIB_END} markers")
    md = md[:i] + ATTRIB_START + "\n" + table + "\n" + md[j:]
    open(ATTRIB, "w").write(md)
    print(f"wrote {ATTRIB} ({len(names)} icons credited)")


if __name__ == "__main__":
    main()
