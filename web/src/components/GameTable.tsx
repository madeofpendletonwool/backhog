import { Star } from "lucide-react";
import { Link } from "react-router-dom";

import { GameCover } from "./GameCover";
import { StatusBadge } from "./StatusBadge";
import { accentStyle, formatDuration, relativeTime, releaseYear } from "@/lib/format";
import type { Entry } from "@/lib/types";

/** The dense alternative to the cover grid, for scanning many games at once. */
export function GameTable({ entries }: { entries: Entry[] }) {
  return (
    <div className="panel overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[52rem] text-sm">
          <thead>
            <tr className="border-b border-white/[0.06] text-left text-xs font-medium text-ink-400">
              <th className="px-4 py-3 font-medium">Game</th>
              <th className="px-4 py-3 font-medium">Status</th>
              <th className="px-4 py-3 font-medium">Genres</th>
              <th className="px-4 py-3 text-right font-medium">To beat</th>
              <th className="px-4 py-3 text-right font-medium">Rating</th>
              <th className="px-4 py-3 text-right font-medium">Added</th>
            </tr>
          </thead>
          <tbody>
            {entries.map((entry) => (
              <tr
                key={entry.id}
                className="border-b border-white/[0.04] transition-colors last:border-0 hover:bg-white/[0.03]"
              >
                <td className="px-4 py-2.5">
                  <Link
                    to={`/game/${entry.id}`}
                    style={accentStyle(entry.game)}
                    className="flex items-center gap-3 rounded-lg focus-visible:focus-ring"
                  >
                    <GameCover game={entry.game} className="w-9 shrink-0 rounded-md" />
                    <div className="min-w-0">
                      <p className="truncate font-medium text-ink-100">{entry.game.name}</p>
                      <p className="text-xs text-ink-500">{releaseYear(entry.game) || "—"}</p>
                    </div>
                  </Link>
                </td>
                <td className="px-4 py-2.5">
                  <StatusBadge status={entry.status} />
                </td>
                <td className="max-w-[14rem] truncate px-4 py-2.5 text-xs text-ink-400">
                  {entry.game.genres.map((g) => g.name).join(", ") || "—"}
                </td>
                <td className="px-4 py-2.5 text-right tabular-nums text-ink-300">
                  {formatDuration(entry.game.time_to_beat_main)}
                </td>
                <td className="px-4 py-2.5 text-right">
                  {entry.user_rating != null ? (
                    <span className="inline-flex items-center gap-1 tabular-nums text-amber-300">
                      <Star className="size-3 fill-amber-300" />
                      {entry.user_rating}
                    </span>
                  ) : entry.game.igdb_rating != null ? (
                    <span className="tabular-nums text-ink-500">
                      {Math.round(entry.game.igdb_rating)}
                    </span>
                  ) : (
                    <span className="text-ink-600">—</span>
                  )}
                </td>
                <td className="px-4 py-2.5 text-right text-xs text-ink-500">
                  {relativeTime(entry.created_at)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
