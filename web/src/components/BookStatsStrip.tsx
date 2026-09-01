import { useBookStats } from "@/hooks/useBooks";
import { Gi } from "./ui/Gi";
import { Skeleton } from "./ui/primitives";

/**
 * The shelf strip, mirroring StatsStrip. It counts books rather than hours:
 * a backlog of games is measured in the time it owes you, and a backlog of
 * books is measured in books until a page map exists to turn them into time.
 */
export function BookStatsStrip() {
  const { data: stats, isLoading } = useBookStats();

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
        label="To read"
        value={String(stats.backlog)}
        hint={`${stats.total} book${stats.total === 1 ? "" : "s"} on the shelf`}
        icon={<Gi name="book-pile" className="size-4" />}
      />
      <Tile
        label="Reading now"
        value={String(stats.reading)}
        hint={stats.reading === 0 ? "Nothing in progress" : "Keep going"}
        icon={<Gi name="bookshelf" className="size-4" />}
        accent="text-cyan-300"
      />
      <Tile
        label="Read"
        value={String(stats.read)}
        hint={
          stats.dropped > 0
            ? `${stats.dropped} abandoned along the way`
            : "Finished, cover to cover"
        }
        icon={<Gi name="check-circle" className="size-4" />}
        accent="text-emerald-300"
      />
      <div className="panel p-4">
        <div className="flex items-center justify-between">
          <p className="text-xs font-medium text-ink-400">Completion</p>
          {(stats.wishlist > 0 || stats.ignored > 0) && (
            <span className="text-[11px] text-ink-500">
              {[
                stats.wishlist > 0 && `${stats.wishlist} wishlisted`,
                stats.ignored > 0 && `${stats.ignored} ignored`,
              ]
                .filter(Boolean)
                .join(" · ")}
            </span>
          )}
        </div>
        <p className="mt-1 text-2xl font-semibold tabular-nums tracking-tight text-ink-100">
          {stats.completion}%
        </p>
        <div
          className="f-bar mt-2.5"
          role="progressbar"
          aria-valuenow={stats.completion}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-label="Shelf completion"
        >
          <div
            className="h-full bg-gradient-to-r from-brand-500 to-emerald-400 transition-[width] duration-700 ease-[var(--ease-spring)]"
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
