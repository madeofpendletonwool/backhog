import { cn } from "@/lib/cn";
import { Clock, Star } from "lucide-react";
import { Link } from "react-router-dom";

import { GameCover } from "./GameCover";
import { StatusMenu } from "./StatusMenu";
import { StatusBadge } from "./StatusBadge";
import { accentStyle, formatDuration, releaseYear } from "@/lib/format";
import type { Entry } from "@/lib/types";

/**
 * A cover-led card. Metadata sits over the artwork on hover so the grid reads
 * as a wall of covers at rest, and only reveals detail on intent.
 */
export function GameCard({ entry }: { entry: Entry }) {
  const { game } = entry;
  const year = releaseYear(game);

  return (
    <div className="group relative" style={accentStyle(game)}>
      {/* Accent glow, sampled from the cover art. */}
      <div
        className="pointer-events-none absolute -inset-2 -z-10 rounded-2xl opacity-0 blur-xl transition-opacity duration-300 group-hover:opacity-45"
        style={{ background: "var(--accent)" }}
        aria-hidden="true"
      />

      <Link
        to={`/game/${entry.id}`}
        className="block rounded-xl focus-visible:focus-ring"
        aria-label={game.name}
      >
        <div
          className={cn(
            "relative overflow-hidden rounded-xl ring-1 ring-white/[0.08]",
            "transition-transform duration-300 ease-[var(--ease-spring)]",
            "group-hover:-translate-y-1 group-hover:ring-white/20",
          )}
        >
          <GameCover game={game} sizes="(max-width: 640px) 45vw, 200px" />

          {/* Bottom scrim carrying the title, always legible over any art. */}
          <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-ink-950 via-ink-950/85 to-transparent px-3 pb-2.5 pt-8">
            <p className="line-clamp-2 text-[13px] font-semibold leading-snug text-white">
              {game.name}
            </p>
            <div className="mt-1 flex items-center gap-2 text-[11px] text-ink-400">
              {year && <span>{year}</span>}
              {game.time_to_beat_main && (
                <span className="inline-flex items-center gap-1">
                  <Clock className="size-3" />
                  {formatDuration(game.time_to_beat_main)}
                </span>
              )}
            </div>
          </div>

          {entry.user_rating != null && (
            <div className="absolute right-2 top-2 inline-flex items-center gap-1 rounded-lg bg-ink-950/80 px-1.5 py-1 text-[11px] font-semibold text-amber-300 backdrop-blur-sm">
              <Star className="size-3 fill-amber-300" />
              {entry.user_rating}
            </div>
          )}

          <div className="absolute left-2 top-2">
            <StatusBadge status={entry.status} showLabel={false} className="px-1.5 backdrop-blur-sm" />
          </div>
        </div>
      </Link>

      {/* Quick status switch, revealed on hover or keyboard focus. */}
      <div className="pointer-events-none absolute inset-x-2 bottom-2 opacity-0 transition-opacity duration-200 focus-within:pointer-events-auto focus-within:opacity-100 group-hover:pointer-events-auto group-hover:opacity-100">
        <StatusMenu entry={entry} />
      </div>
    </div>
  );
}

export function GameCardSkeleton() {
  return (
    <div className="space-y-2">
      <div className="skeleton aspect-[3/4] w-full rounded-xl" />
    </div>
  );
}
