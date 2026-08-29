import { cn } from "@/lib/cn";

/**
 * A pixel sprite from the arcade sheet — joysticks and buttons, DB16
 * pixel art at 32x64 per cell. Whole-number scales only (`scale` 1-3);
 * a fractional scale would resample the pixels. Decorative by default:
 * the control around a sprite carries the accessible name.
 */
export const SPRITES = {
  stick: 0,
  "stick-alt": 1,
  button: 2,
  "button-hi": 3,
  "button-lo": 4,
  pad: 5,
  ball: 6,
} as const;

export type SpriteName = keyof typeof SPRITES;

export function Sprite({
  name,
  scale = 1,
  className,
  loading = false,
}: {
  name: SpriteName;
  scale?: 1 | 2 | 3;
  className?: string;
  loading?: boolean;
}) {
  return (
    <span
      aria-hidden="true"
      className={cn("sprite", loading && "sprite-loading", className)}
      style={{ "--i": SPRITES[name], "--s": scale } as React.CSSProperties}
    />
  );
}
