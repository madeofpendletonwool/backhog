import { Clock, Gamepad2, Trophy } from "lucide-react";

import { formatHours } from "@/lib/format";
import { useStats } from "@/hooks/useLibrary";
import { Skeleton } from "./ui/primitives";

/**
 * The dashboard strip. Deliberately shows the backlog size in *hours*, not just
 * a count — the hours are the part that actually stings.
 */
export function StatsStrip() {
  const { data: stats, isLoading } = useStats();

  if (isLoading) {
    return (
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {Array.from({ length: 4 }).map((_, index) => (
          <Skeleton key={index} className="h-[86px]" />
        ))}
      </div>
    );
  }

  if (!stats) return null;

  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
      <Tile
        label="In the backlog"
        value={String(stats.backlog)}
        hint={`${formatHours(stats.backlog_hours)} of playing`}
        icon={<Gamepad2 className="size-4" />}
      />
      <Tile
        label="Playing now"
        value={String(stats.playing)}
        // Logged hours are what you actually recorded, so they beat the
        // crowd-sourced estimate whenever there are any.
        hint={
          stats.logged_hours > 0
            ? `${formatHours(stats.logged_hours)} logged by you`
            : stats.playing === 0
              ? "Nothing in progress"
              : "Keep going"
        }
        icon={<Clock className="size-4" />}
        accent="text-cyan-300"
      />
      <Tile
        label="Completed"
        value={String(stats.played)}
        hint={`${formatHours(stats.played_hours)} of games beaten`}
        icon={<Trophy className="size-4" />}
        accent="text-emerald-300"
      />
      <div className="panel p-4">
        <div className="flex items-center justify-between">
          <p className="text-xs font-medium text-ink-400">Completion</p>
          {stats.wishlist > 0 && (
            <span className="text-[11px] text-amber-300/70">{stats.wishlist} wishlisted</span>
          )}
        </div>
        <p className="mt-1 text-2xl font-semibold tabular-nums tracking-tight text-ink-100">
          {stats.completion}%
        </p>
        <div
          className="mt-2.5 h-1.5 overflow-hidden rounded-full bg-ink-800"
          role="progressbar"
          aria-valuenow={stats.completion}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-label="Library completion"
        >
          <div
            className="h-full rounded-full bg-gradient-to-r from-brand-500 to-emerald-400 transition-[width] duration-700 ease-[var(--ease-spring)]"
            style={{ width: `${Math.min(stats.completion, 100)}%` }}
          />
        </div>
      </div>
    </div>
  );
}

function Tile({
  label,
  value,
  hint,
  icon,
  accent = "text-ink-300",
}: {
  label: string;
  value: string;
  hint: string;
  icon: React.ReactNode;
  accent?: string;
}) {
  return (
    <div className="panel p-4">
      <div className="flex items-center justify-between">
        <p className="text-xs font-medium text-ink-400">{label}</p>
        <span className={accent}>{icon}</span>
      </div>
      <p className="mt-1 text-2xl font-semibold tabular-nums tracking-tight text-ink-100">
        {value}
      </p>
      <p className="mt-1 truncate text-[11px] text-ink-500">{hint}</p>
    </div>
  );
}
