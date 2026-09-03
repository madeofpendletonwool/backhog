import { cn } from "@/lib/cn";

import { statusLabel, type MediaType, type Status } from "@/lib/types";
import { Gi } from "./ui/Gi";
import type { GiName } from "@/lib/gameicons";

/* Status colour carries meaning — playing is cyan, played is green — and the
   hue stays constant across every theme, the same way grimoire's corpus
   accent never re-tints. Only the chrome around it follows the backdrop.

   The *lightness* cannot: this was a fixed dark-mode recipe — a 15% plate
   under 300-shade text — until the app grew a light theme, where the 300
   shade on a pale tint is about 1.5:1. So `tone-chip` gets the hue to wash
   the plate from and the ink to use on a dark ground, and a light theme
   pulls that ink toward its own near-black via --tone-darken. Dark themes
   leave it at 0 and render exactly what they always did. */
const STATUS_TONE: Record<Status, { tone: string; ink: string }> = {
  backlog: { tone: "var(--color-slate-500)", ink: "var(--color-slate-300)" },
  playing: { tone: "var(--color-cyan-500)", ink: "var(--color-cyan-300)" },
  played: { tone: "var(--color-emerald-500)", ink: "var(--color-emerald-300)" },
  dropped: { tone: "var(--color-red-500)", ink: "var(--color-red-300)" },
  ignored: { tone: "var(--color-zinc-500)", ink: "var(--color-zinc-300)" },
  wishlist: { tone: "var(--color-amber-500)", ink: "var(--color-amber-300)" },
};

/** The inline custom properties `tone-chip` and `tone-ink` read. */
export function toneStyle(status: Status) {
  const { tone, ink } = STATUS_TONE[status];
  return { "--tone": tone, "--tone-ink": ink } as React.CSSProperties;
}

export const STATUS_ICONS: Record<Status, GiName> = {
  backlog: "circle-dashed",
  playing: "play",
  played: "check-circle",
  dropped: "x-circle",
  ignored: "ban",
  wishlist: "gift",
};

export function StatusBadge({
  status,
  className,
  showLabel = true,
  media = "game",
}: {
  status: Status;
  className?: string;
  showLabel?: boolean;
  /** Which arena is asking — the colour is shared, the wording is not. */
  media?: MediaType;
}) {
  return (
    <span
      style={toneStyle(status)}
      className={cn(
        "tone-chip inline-flex items-center gap-1.5 px-2 py-0.5 text-xs font-medium",
        className,
      )}
    >
      <Gi name={STATUS_ICONS[status]} className="size-3" />
      {showLabel && statusLabel(status, media)}
    </span>
  );
}
