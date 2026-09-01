import { Link } from "react-router-dom";

import { Gi } from "@/components/ui/Gi";
import { Button, EmptyState, Panel, Skeleton } from "@/components/ui/primitives";
import {
  useKickSeriesBackfill,
  useSeriesBackfillStatus,
  useSeriesIndex,
} from "@/hooks/useSeries";
import { useStats } from "@/hooks/useLibrary";
import { formatHours } from "@/lib/format";

/**
 * The series index: every franchise or collection with at least two owned
 * games, rolled up into a journey card.
 */
export function SeriesPage() {
  const { data, isLoading } = useSeriesIndex();
  const { data: stats } = useStats();
  const kick = useKickSeriesBackfill();

  // Poll while a backfill walk runs, so cards appear as IGDB data lands.
  const backfill = useSeriesBackfillStatus(
    kick.isPending || Boolean(data?.series.length === 0 && (stats?.total ?? 0) > 0),
  );

  const series = data?.series ?? [];
  const walking = backfill.data?.running ?? false;

  return (
    <div className="mx-auto max-w-5xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <header className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-ink-100">Series</h1>
          <p className="mt-1 text-sm text-ink-400">
            Franchises and collections, played as journeys instead of rows.
          </p>
        </div>
        {series.length > 0 && (
          <Button
            variant="ghost"
            size="sm"
            loading={kick.isPending || walking}
            disabled={walking && !kick.isPending}
            onClick={() => kick.mutate()}
            title="Re-link series from IGDB"
          >
            <Gi name="refresh" className="size-4" />
            Refresh
          </Button>
        )}
      </header>

      {walking && (
        <p className="mb-4 rounded-xl bg-brand-600/10 px-3 py-2 text-sm text-brand-300">
          Linking series from IGDB — this runs in the background and fills in as it goes.
        </p>
      )}

      {isLoading ? (
        <div className="grid gap-4 sm:grid-cols-2">
          {Array.from({ length: 4 }).map((_, index) => (
            <Skeleton key={index} className="h-36" />
          ))}
        </div>
      ) : series.length === 0 ? (
        <EmptyState
          icon={<Gi name="layers" className="size-7" />}
          title="No series yet"
          description={
            (stats?.total ?? 0) > 0
              ? "Series come from IGDB franchises and collections. A background link runs at startup; you can also start one now."
              : "Add a few games from the same franchise and they'll group up here automatically."
          }
          action={
            (stats?.total ?? 0) > 0 ? (
              <Button variant="primary" loading={kick.isPending || walking} onClick={() => kick.mutate()}>
                <Gi name="refresh" className="size-4" />
                Build series data
              </Button>
            ) : undefined
          }
        />
      ) : (
        <div className="grid gap-4 sm:grid-cols-2">
          {series.map((s) => (
            <Link key={s.id} to={`/series/${s.id}`} className="group focus-visible:focus-ring">
              <Panel className="h-full p-5 transition-colors group-hover:border-white/[0.12]">
                <div className="flex items-baseline justify-between gap-3">
                  <h2 className="truncate font-semibold text-ink-100">{s.name}</h2>
                  <span className="shrink-0 text-xs tabular-nums text-ink-500">
                    {s.played_count}/{s.owned_count} played
                  </span>
                </div>

                <div className="f-bar mt-3">
                  <div
                    className="h-full bg-brand-500 transition-[width]"
                    style={{ width: `${Math.min(s.completion, 100)}%` }}
                  />
                </div>

                <p className="mt-2.5 text-xs tabular-nums text-ink-500">
                  {Math.round(s.completion)}% complete
                  {s.remaining_hours > 0 && ` · ${formatHours(s.remaining_hours)} left`}
                </p>

                {s.next_game && (
                  <p className="mt-3 flex items-center gap-1.5 text-sm text-ink-300">
                    <span className="text-ink-500">Next up:</span>
                    <span className="truncate font-medium text-ink-100">{s.next_game.name}</span>
                  </p>
                )}
              </Panel>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
