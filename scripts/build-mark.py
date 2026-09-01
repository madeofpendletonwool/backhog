#!/usr/bin/env python3
"""Draws Backhog's brand mark — the hog — and the icons cut from it.

Everything else in web/public/assets is *derived* from a licensed upstream
pack (see build-assets.py and ATTRIBUTIONS.md). The mark is the one piece
that has to be ours, so it is authored here as a pixel grid rather than
recoloured from someone else's art.

Why by hand: the app's wordmark used to borrow a joystick cell out of the
arcade sprite sheet, which read as a grey blob at 32px and said nothing
about the product; and the favicon was a solid red square with four dark
pixels in it. A backlog manager called Backhog should have a hog.

Run: python3 scripts/build-mark.py
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import pngkit

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.join(ROOT, "web", "public", "assets")

# A DB16-adjacent set, so the mark sits in the same world as the arcade
# sheet without being tied to any one theme's hue.
# Bristly boar brown rather than pig pink: it has to sit next to coin gold
# in the arcade without arguing with it, and next to nothing at all in
# Midnight. Five steps of hide plus bone for the tusk — enough to model a
# curved skull, few enough to stay crisp when the tab strip rasterises it
# at 16px.
PALETTE = {
    ".": None,                # transparent
    "K": (0x1e, 0x14, 0x14),  # outline
    "D": (0x45, 0x2e, 0x28),  # shadow side
    "M": (0x66, 0x45, 0x36),  # midtone
    "L": (0x8a, 0x5f, 0x47),  # lit side
    "H": (0xad, 0x7c, 0x5c),  # highlight (the snout catches the light)
    "T": (0xf0, 0xe4, 0xd2),  # tusk
    "E": (0x14, 0x0d, 0x0e),  # eye
    "N": (0x33, 0x20, 0x1a),  # nostril
}

# 32x32. Snout to the right, ears back — a hog in profile, reading at
# 16px as a distinct silhouette rather than a blob.
ART = """
................................
.....KK...............KK........
....KDMK.............KMLK.......
....KDMMK...........KMLLK.......
....KDMMMK.........KMLLLK.......
....KDMMMMKKKKKKKKKMLLLLK.......
.....KDMMMMMMMMMMMMMLLLLK.......
.....KDMMMMLLLLLLLLLLLLLKK......
....KDMMLLLLLLLLLLLLLLLLLLK.....
...KDMLLLLLLLLLLLLLLLLLLLLLK....
..KDMLLLLLLLLLLLLLLLLLLLLLLLK...
..KDMLLLLLLLLLLLLLLLLLLLLLLLLK..
.KDMLLLLLLLLLLLLLLLLLLLLLLLLLLK.
.KDMLLLLKELLLLLLLLLLLLLLLLLLLLLK
.KDMLLLLKELLLLLLLLLLLLHHHHHHHHLK
.KDMLLLLLLLLLLLLLLLLLHHNHHHHNHHK
.KDMLLLLLLLLLLLLLLLLLHHNHHHHNHHK
.KDMLLLLLLLLLLLLLLLLLLHHHHHHHHHK
.KDMLLLLLLLLLLLLLLTKLLLHHHHHHHK.
.KDDMLLLLLLLLLLLLTTKLLLLHHHHHKK.
..KDDMLLLLLLLLLLTTKLLLLLLLLLLK..
..KKDDMLLLLLLLLLTKKLLLLLLLLLKK..
...KKDDMMLLLLLLLLKLLLLLLLLLKK...
.....KKDDMMMLLLLLLLLLLLLLKKK....
.......KKDDDMMMMMLLLLLKKKK......
.........KKKKDDDDDDKKKKK........
..............KKKKK.............
................................
................................
................................
................................
................................
"""


def grid(art):
    rows = [r for r in art.strip("\n").split("\n") if r]
    w = max(len(r) for r in rows)
    return [r.ljust(w, ".") for r in rows], w, len(rows)


def render(art, scale=1):
    rows, w, h = grid(art)
    img = pngkit.Image(w, h)
    for y, row in enumerate(rows):
        for x, ch in enumerate(row):
            rgb = PALETTE.get(ch)
            if rgb is None:
                continue
            o = (y * w + x) * 4
            img.px[o:o + 4] = bytes((*rgb, 255))
    return img.scale(scale) if scale > 1 else img


def main():
    mark = render(ART)

    targets = [
        # The wordmark sprite, used at its native size in the sidebar.
        (os.path.join(OUT, "sprites", "hog.png"), 1),
        # Browser tab. 32px is the size that actually gets rasterised on a
        # HiDPI tab strip; the old file was named -32 and was 16px.
        (os.path.join(OUT, "favicon-32.png"), 1),
        # Home-screen icon. Whole-number scale only — 6x of 32 is 192,
        # which is the size the manifest asks for. The old one was 96.
        (os.path.join(OUT, "icon-192.png"), 6),
    ]
    for path, scale in targets:
        os.makedirs(os.path.dirname(path), exist_ok=True)
        img = mark.scale(scale) if scale > 1 else mark
        pngkit.write(path, img)
        print(f"  {os.path.relpath(path, ROOT):44} {img.w}x{img.h}")


if __name__ == "__main__":
    main()
