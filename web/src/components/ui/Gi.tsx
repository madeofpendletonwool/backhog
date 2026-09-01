import { GAME_ICONS, GAME_ICON_BOX, type GiName } from "@/lib/gameicons";
import { cn } from "@/lib/cn";

/**
 * A game-icons.net vector — the small affordances and nouns. Monochrome,
 * inherits `currentColor`, crisp at any size. Sized with ordinary Tailwind
 * classes (`className="size-4"`); decorative unless given a label.
 *
 * The viewBox comes from the icon's own measured geometry rather than a
 * flat "0 0 512 512" — upstream glyphs do not all fill the canvas, and the
 * ones that do not were rendering at a quarter size. See
 * scripts/fetch-gameicons.py.
 */
export function Gi({
  name,
  className,
  label,
}: {
  name: GiName;
  className?: string;
  label?: string;
}) {
  const paths = GAME_ICONS[name];
  const box: { v: string; r?: number } = GAME_ICON_BOX[name];
  // Rotation is about the viewBox centre, which is the glyph's own centre,
  // so a turned-over icon stays put instead of swinging off the canvas.
  const [bx, by, bw, bh] = box.v.split(" ").map(Number);
  const spin = box.r ? `rotate(${box.r} ${bx + bw / 2} ${by + bh / 2})` : undefined;
  return (
    <svg
      viewBox={box.v}
      className={cn("gi", className)}
      aria-hidden={label ? undefined : true}
      role={label ? "img" : undefined}
      aria-label={label}
    >
      <g transform={spin}>
        {paths.map((d, i) => (
          <path key={i} d={d} />
        ))}
      </g>
    </svg>
  );
}
