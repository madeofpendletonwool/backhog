import { useEffect, useRef } from "react";

import { useTheme } from "@/hooks/useTheme";

/**
 * The room the app sits in — whichever room the theme picked.
 *
 * Under the arcade family that is a layered pixel backdrop that drifts
 * with the pointer, plus the veil that guarantees text contrast over any
 * scene. Layer counts and art heights come from the build (themes.css
 * sets --scene-layers and --scene-height); layers are numbered front to
 * back and appended in reverse so 0 paints on top — stacking them the
 * other way buries the scene under its own sky.
 *
 * Midnight has no backdrop art. It gets a single soft aurora instead, so
 * flat black never reads as dead space, and none of the parallax
 * machinery below ever runs.
 */

/** How far the nearest layer travels, corner to corner, in px. */
const DRIFT = 52;

/** Depth is not linear: distant layers stay almost still while the
 *  foreground sweeps, which is what actually reads as distance. */
const DEPTH_CURVE = 2.2;

export function Scene() {
  const { theme, family } = useTheme();
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const root = rootRef.current;
    if (!root) return;

    if (family === "flat") {
      const aurora = document.createElement("div");
      aurora.className = "scene-aurora";
      root.replaceChildren(aurora);
      root.style.removeProperty("--scene-scale");
      return;
    }

    const css = getComputedStyle(document.documentElement);
    const layers = Number(css.getPropertyValue("--scene-layers")) || 0;
    const height = Number(css.getPropertyValue("--scene-height")) || 216;

    root.replaceChildren();

    const layerEls: HTMLElement[] = [];
    if (theme !== "bare" && layers > 0) {
      for (let i = layers - 1; i >= 0; i--) {
        const layer = document.createElement("div");
        layer.className = "scene-layer";
        layer.style.backgroundImage = `url("/assets/scenes/${theme}/${i}.png")`;
        const depth = 1 - i / Math.max(1, layers - 1);
        layer.dataset.depth = String(Math.pow(depth, DEPTH_CURVE));
        root.append(layer);
        layerEls.push(layer);
      }
    }
    const veil = document.createElement("div");
    veil.className = "scene-veil";
    const glow = document.createElement("div");
    glow.className = "scene-glow";
    root.append(veil, glow);

    // Whole numbers only: a fractional scale resamples the pixels. The
    // zoom is whatever integer covers the window's height; repeat-x
    // covers the width for every tiling layer.
    const rescale = () => {
      const cover = Math.ceil(window.innerHeight / height);
      root.style.setProperty("--scene-scale", String(Math.min(6, Math.max(1, cover))));
    };
    rescale();

    let target = { x: 0, y: 0 };
    let frame = 0;
    const apply = () => {
      frame = 0;
      for (const layer of layerEls) {
        const depth = Number(layer.dataset.depth);
        const x = target.x * DRIFT * depth;
        const y = target.y * DRIFT * depth * 0.4;
        layer.style.transform = `translate3d(${x.toFixed(2)}px, ${y.toFixed(2)}px, 0)`;
      }
    };
    const onPointer = (e: PointerEvent) => {
      // -1..1 from centre, inverted so the scene falls away from the cursor.
      target = {
        x: -((e.clientX / window.innerWidth) * 2 - 1),
        y: -((e.clientY / window.innerHeight) * 2 - 1),
      };
      if (!frame) frame = requestAnimationFrame(apply);
    };

    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    window.addEventListener("resize", rescale);
    if (!reduced && window.matchMedia("(hover: hover)").matches) {
      window.addEventListener("pointermove", onPointer, { passive: true });
    }
    return () => {
      window.removeEventListener("resize", rescale);
      window.removeEventListener("pointermove", onPointer);
      if (frame) cancelAnimationFrame(frame);
    };
  }, [theme, family]);

  return <div ref={rootRef} className="scene" aria-hidden="true" />;
}
