import { cn } from "@/lib/cn";
import { Gamepad2 } from "lucide-react";
import { useState } from "react";

import { coverUrl } from "@/lib/api";
import type { Game } from "@/lib/types";

/**
 * A game cover with a graceful fallback. Covers are 3:4; games missing artwork
 * get a tinted monogram tile rather than a broken image or an empty hole.
 */
export function GameCover({
  game,
  className,
  sizes,
}: {
  game: Game;
  className?: string;
  sizes?: string;
}) {
  const [failed, setFailed] = useState(false);
  const showFallback = failed || !game.cover_url;

  return (
    <div
      className={cn(
        "relative isolate aspect-[3/4] w-full overflow-hidden rounded-xl bg-ink-800",
        className,
      )}
    >
      {showFallback ? (
        <div
          className="flex size-full flex-col items-center justify-center gap-2 p-3 text-center"
          style={{
            background:
              "linear-gradient(160deg, color-mix(in oklab, var(--accent) 28%, var(--color-ink-800)), var(--color-ink-850))",
          }}
        >
          <Gamepad2 className="size-7 text-white/35" />
          <span className="line-clamp-3 text-[11px] font-medium leading-tight text-white/60">
            {game.name}
          </span>
        </div>
      ) : (
        <img
          src={coverUrl(game.id)}
          alt=""
          loading="lazy"
          decoding="async"
          sizes={sizes}
          onError={() => setFailed(true)}
          className="size-full object-cover"
        />
      )}
    </div>
  );
}
