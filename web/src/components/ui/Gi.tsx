import { GAME_ICONS, type GiName } from "@/lib/gameicons";
import { cn } from "@/lib/cn";

/**
 * A game-icons.net vector — the small affordances and nouns. Monochrome,
 * inherits `currentColor`, crisp at any size. Sized with ordinary Tailwind
 * classes (`className="size-4"`); decorative unless given a label.
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
  return (
    <svg
      viewBox="0 0 512 512"
      className={cn("gi", className)}
      aria-hidden={label ? undefined : true}
      role={label ? "img" : undefined}
      aria-label={label}
    >
      {paths.map((d, i) => (
        <path key={i} d={d} />
      ))}
    </svg>
  );
}
