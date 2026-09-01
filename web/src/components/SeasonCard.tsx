import { Link } from "react-router-dom";

import { Panel, Gi } from "@/components/ui/primitives";
import { useSeason } from "@/hooks/useAchievements";
import { formatHours } from "@/lib/format";

/**
 * The "YYYY Backlog Challenge" card: the year's campaign on the dashboard.
 * Zeros are motivating, not broken — a fresh year starts everyone at the line.
 */
export function SeasonCard() {
  const { data: season } = useSeason();
  if (!season) return null;

  return (
    <Panel className="animate-fade-rise p-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-wider text-ink-500">
            This year's campaign
          </p>
          <p className="mt-1 text-xl font-semibold tracking-tight text-ink-100">
            {season.year} Backlog Challenge
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
        <Stat icon={<Gi name="flag" className="size-3.5" />} label="Games completed" value={String(season.games_completed)} />
        <Stat icon={<Gi name="timer" className="size-3.5" />} label="Hours played" value={formatHours(season.hours_played)} />
        <Stat icon={<Gi name="layers" className="size-3.5" />} label="Franchises cleared" value={String(season.franchises_cleared)} />
        <Stat icon={<Gi name="lifebuoy" className="size-3.5" />} label="Backlog rescues" value={String(season.rescues)} />
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
