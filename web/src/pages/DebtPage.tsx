import { Gi } from "@/components/ui/Gi";
import { EmptyState, Panel, Skeleton } from "@/components/ui/primitives";
import { useDebt } from "@/hooks/useLibrary";
import { formatHours, formatMonthYear, formatTimespan } from "@/lib/format";
import type { ClearanceScenario, DebtReport } from "@/lib/types";

/**
 * The time story of the backlog: how many unplayed hours are sitting there,
 * how fast they're actually being worked through, and when it all clears.
 */
export function DebtPage() {
  const { data: debt, isLoading } = useDebt();

  if (isLoading) {
    return (
      <div className="mx-auto max-w-4xl space-y-4 px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
        <Skeleton className="h-24" />
        <Skeleton className="h-64" />
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-4xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold tracking-tight text-ink-100">Backlog Debt</h1>
        <p className="mt-1 text-sm text-ink-400">What you owe yourself, and when it gets paid off.</p>
      </header>

      {!debt || debt.total_hours <= 0 ? (
        <EmptyState
          icon={<Gi name="hourglass" className="size-7" />}
          title="Nothing owed"
          description="No unplayed hours in the backlog. Add games you mean to finish and the debt math shows up here."
        />
      ) : (
        <div className="space-y-4">
          <Headline debt={debt} />
          <BreakdownTable debt={debt} />
          <PacePanel debt={debt} />
        </div>
      )}
    </div>
  );
}

function Headline({ debt }: { debt: DebtReport }) {
  return (
    <Panel className="animate-fade-rise p-6">
      <p className="text-sm text-ink-400">You currently have</p>
      <p className="mt-1 text-4xl font-semibold tabular-nums tracking-tight text-ink-100">
        {formatHours(debt.total_hours)}
        <span className="ml-2 text-xl font-normal text-ink-400">of unplayed games</span>
      </p>
      {debt.projection.current_pace && (
        <p className="mt-3 text-sm leading-relaxed text-ink-300">
          At your current pace of{" "}
          <span className="font-medium text-ink-100">
            {formatHours(debt.projection.current_pace.hours_per_week)} hrs/week
          </span>
          , your backlog would take{" "}
          <span className="font-medium text-ink-100">
            {formatTimespan(debt.projection.current_pace.weeks)}
          </span>{" "}
          to clear — around {formatMonthYear(debt.projection.current_pace.clear_by)}.
        </p>
      )}
    </Panel>
  );
}

function BreakdownTable({ debt }: { debt: DebtReport }) {
  const rows: { label: string; hint: string; hours: number | null }[] = [
    { label: "Main backlog", hint: "Untouched games", hours: debt.main_backlog_hours },
    { label: "Started", hint: "Minus what you've already logged", hours: debt.started_hours },
    { label: "Short games", hint: "Under 8 hours each", hours: debt.short_games_hours },
    // Deliberately not summed: these are shown as "—" until they mean something.
    { label: "Wishlist", hint: "A shopping list, not a debt", hours: debt.wishlist_hours },
    { label: "DLC", hint: "Add-ons of games still owed", hours: debt.dlc_hours },
  ];

  return (
    <Panel className="overflow-hidden">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-white/[0.06] text-left text-xs font-medium text-ink-400">
            <th className="px-4 py-3 font-medium">Where it sits</th>
            <th className="hidden px-4 py-3 font-medium sm:table-cell">What counts</th>
            <th className="px-4 py-3 text-right font-medium">Hours</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.label} className="border-b border-white/[0.04] last:border-0">
              <td className="px-4 py-2.5 font-medium text-ink-200">{row.label}</td>
              <td className="hidden px-4 py-2.5 text-xs text-ink-500 sm:table-cell">{row.hint}</td>
              <td className="px-4 py-2.5 text-right tabular-nums text-ink-300">
                {row.hours == null ? "—" : formatHours(row.hours)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </Panel>
  );
}

function PacePanel({ debt }: { debt: DebtReport }) {
  const { current_pace, scenarios } = debt.projection;

  return (
    <Panel className="p-5">
      <div className="mb-4 flex items-baseline justify-between gap-3">
        <h2 className="text-sm font-semibold text-ink-200">When it clears</h2>
        <p className="text-xs text-ink-500">
          {debt.pace.hours_per_week_all != null
            ? `All-time pace: ${formatHours(debt.pace.hours_per_week_all)} hrs/week`
            : "Pace comes from logged play sessions"}
        </p>
      </div>

      {current_pace ? (
        <ScenarioRow scenario={current_pace} label="Your pace" highlight />
      ) : (
        <p className="mb-3 rounded-xl bg-ink-850 px-3 py-2 text-xs leading-relaxed text-ink-400">
          No sessions logged in the last 90 days, so there's no current pace to project from. Log
          playtime on a game and this fills in.
        </p>
      )}

      <div className="mt-1 divide-y divide-white/[0.04]">
        {scenarios.map((scenario) => (
          <ScenarioRow key={scenario.hours_per_week} scenario={scenario} label="If you play" />
        ))}
      </div>
    </Panel>
  );
}

function ScenarioRow({
  scenario,
  label,
  highlight = false,
}: {
  scenario: ClearanceScenario;
  label: string;
  highlight?: boolean;
}) {
  return (
    <div className="flex items-center justify-between gap-3 py-2.5 first:pt-0">
      <p className="text-sm">
        {label}{" "}
        <span className={highlight ? "font-medium text-ink-100" : "font-medium text-ink-200"}>
          {formatHours(scenario.hours_per_week)} hrs/week
        </span>
      </p>
      <p className="text-right text-sm">
        <span className="tabular-nums text-ink-100">{formatMonthYear(scenario.clear_by)}</span>
        <span className="ml-2 text-xs text-ink-500">
          {scenario.clear_by ? formatTimespan(scenario.weeks) : "never"}
        </span>
      </p>
    </div>
  );
}
