import { Link } from "react-router-dom";

import { Panel, Gi } from "@/components/ui/primitives";
import { useReadingSeason } from "@/hooks/useAchievements";
import { formatHours } from "@/lib/format";

/**
 * The "YYYY Reading Challenge" card: the books arena's year campaign, the
 * mirror of the games dashboard' Backlog Challenge. Zeros are motivating,
 * not broken — a fresh year starts everyone at the line.
 */
export function ReadingSeasonCard() {
  const { data: season } = useReadingSeason();
  if (!season) return null;

  return (
    <Panel className="animate-fade-rise p-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-wider text-ink-500">
            This year's campaign
          </p>
          <p className="mt-1 text-xl font-semibold tracking-tight text-ink-100">
            {season.year} Reading Challenge
          </p>
        </div>
        <Link
          to="/achievements"
          className="f-btn-soft flex size-11 items-center justify-center text-hl-bright focus-visible:focus-ring"
          aria-label="View achievements"
        >
          <Gi name="trophy" className="size-5" />
        </Link>
      </div>

      <div className="mt-5 flex flex-wrap gap-x-8 gap-y-3 border-t-2 border-line pt-4">
        <Stat icon={<Gi name="book-pile" className="size-3.5" />} label="Books finished" value={String(season.books_finished)} />
        <Stat icon={<Gi name="bookshelf" className="size-3.5" />} label="Pages read" value={season.pages_read.toLocaleString()} />
        <Stat icon={<Gi name="headphones" className="size-3.5" />} label="Hours listened" value={formatHours(season.hours_listened)} />
        <Stat icon={<Gi name="pencil" className="size-3.5" />} label="Authors cleared" value={String(season.authors_cleared)} />
        <Stat icon={<Gi name="lifebuoy" className="size-3.5" />} label="Shelf rescues" value={String(season.rescues)} />
      </div>
    </Panel>
  );
}

function Stat({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <div>
      <p className="flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wider text-ink-500">
        {icon}
        {label}
      </p>
      <p className="mt-0.5 text-lg font-semibold tabular-nums tracking-tight text-ink-100">{value}</p>
    </div>
  );
}
