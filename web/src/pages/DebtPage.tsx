import { MediaFilter, useMediaFilter } from "@/components/MediaFilter";
import { Gi } from "@/components/ui/Gi";
import { EmptyState, Panel, Skeleton } from "@/components/ui/primitives";
import { useReadingDebt } from "@/hooks/useBooks";
import { useDebt } from "@/hooks/useLibrary";
import { formatHours, formatMonthYear, formatTimespan } from "@/lib/format";
import type { ClearanceScenario, DebtReport, ReadingDebt } from "@/lib/types";

/**
 * The time story of the backlog: how much of it is sitting there unfinished,
 * how fast it's actually being worked through, and when it all clears.
 *
 * One page, two arenas, same media filter the queue and lists carry — a game's
 * debt is hours from a crowd-sourced time to beat, a book's is pages from your
 * own measured reading pace, and "Both" stacks them rather than adding two
 * incompatible units together.
 */
export function DebtPage() {
  const [media, setMedia] = useMediaFilter();
  const showGames = media !== "book";
  const showBooks = media !== "game";

  const { data: debt, isLoading } = useDebt({ enabled: showGames });
  const { data: reading, isLoading: readingLoading } = useReadingDebt(showBooks);

  const loading = (showGames && isLoading) || (showBooks && readingLoading);
  const books = media === "book";

  return (
    <div className="mx-auto max-w-4xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <header className="mb-6 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-tight text-ink-100">
            {books ? "Reading Debt" : "Backlog Debt"}
          </h1>
          <p className="mt-1 text-sm text-ink-400">
            What you owe yourself, and when it gets paid off.
          </p>
        </div>
        <MediaFilter value={media} onChange={setMedia} />
      </header>

      {loading ? (
        <div className="space-y-4">
          <Skeleton className="h-24" />
          <Skeleton className="h-64" />
        </div>
      ) : (
        <div className="space-y-4">
          {showBooks && <ReadingDebtSection debt={reading} />}
          {showGames && <GameDebtSection debt={debt} />}
        </div>
      )}
    </div>
  );
}

function GameDebtSection({ debt }: { debt?: DebtReport }) {
  if (!debt || debt.total_hours <= 0) {
    return (
      <EmptyState
        icon={<Gi name="hourglass" className="size-7" />}
        title="Nothing owed"
        description="No unplayed hours in the backlog. Add games you mean to finish and the debt math shows up here."
      />
    );
  }
  return (
    <div className="space-y-4">
      <Headline debt={debt} />
      <BreakdownTable debt={debt} />
      <PacePanel debt={debt} />
    </div>
  );
}

function ReadingDebtSection({ debt }: { debt?: ReadingDebt }) {
  if (!debt || debt.hours_owed <= 0) {
    return (
      <EmptyState
        icon={<Gi name="bookshelf" className="size-7" />}
        title="Nothing owed"
        description="No unread pages on the shelf. Add books you mean to finish and the arithmetic shows up here."
      />
    );
  }
  return (
    <div className="space-y-4">
      <ReadingHeadlinePanel debt={debt} />
      <ReadingBreakdown debt={debt} />
      <ReadingPacePanel debt={debt} />
    </div>
  );
}

function ReadingHeadlinePanel({ debt }: { debt: ReadingDebt }) {
  const { current_pace } = debt.projection;
  return (
    <Panel className="animate-fade-rise p-6">
      <p className="text-sm text-ink-400">Still unread on your shelf</p>
      <p className="mt-1 text-4xl font-semibold tabular-nums tracking-tight text-ink-100">
        {Math.round(debt.pages_owed).toLocaleString()}
        <span className="ml-2 text-xl font-normal text-ink-400">
          pages, plus {formatHours(debt.audio_hours)} of audio
        </span>
      </p>
      <p className="mt-2 text-sm text-ink-300">
        That is <span className="font-medium text-ink-100">{formatHours(debt.hours_owed)}</span> at{" "}
        {debt.pace.pages_per_hour} pages an hour
        {debt.pace.measured ? " — your measured pace" : ", the assumed pace"}.
      </p>
      {current_pace && (
        <p className="mt-3 text-sm leading-relaxed text-ink-300">
          At the{" "}
          <span className="font-medium text-ink-100">
            {formatHours(current_pace.hours_per_week)} hrs/week
          </span>{" "}
          you actually read, the shelf clears in{" "}
          <span className="font-medium text-ink-100">{formatTimespan(current_pace.weeks)}</span> —
          around {formatMonthYear(current_pace.clear_by)}.
        </p>
      )}
    </Panel>
  );
}

function ReadingBreakdown({ debt }: { debt: ReadingDebt }) {
  const rows: { label: string; hint: string; value: string }[] = [
    {
      label: "Pages owed",
      hint: "Whole books on the shelf, plus the unread half of what you've started",
      value: Math.round(debt.pages_owed).toLocaleString(),
    },
    {
      label: "Estimated from pages",
      hint: `${debt.pace.pages_per_hour} pages an hour`,
      value: formatHours(debt.page_hours),
    },
    {
      label: "Audiobooks",
      hint: `${debt.audio_books} book${debt.audio_books === 1 ? "" : "s"} measured at their real running time`,
      value: formatHours(debt.audio_hours),
    },
    {
      label: "Short books",
      hint: "Under 250 pages — an evening each",
      value: formatHours(debt.short_books_hours),
    },
    {
      label: "Unsized",
      hint: "No page count and no audiobook, so they count for nothing",
      value: `${debt.unsized_books} book${debt.unsized_books === 1 ? "" : "s"}`,
    },
  ];

  return (
    <Panel className="overflow-hidden">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-white/[0.06] text-left text-xs font-medium text-ink-400">
            <th className="px-4 py-3 font-medium">Where it sits</th>
            <th className="hidden px-4 py-3 font-medium sm:table-cell">What counts</th>
            <th className="px-4 py-3 text-right font-medium">Amount</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.label} className="border-b border-white/[0.04] last:border-0">
              <td className="px-4 py-2.5 font-medium text-ink-200">{row.label}</td>
              <td className="hidden px-4 py-2.5 text-xs text-ink-500 sm:table-cell">{row.hint}</td>
              <td className="px-4 py-2.5 text-right tabular-nums text-ink-300">{row.value}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </Panel>
  );
}

function ReadingPacePanel({ debt }: { debt: ReadingDebt }) {
  const { current_pace, scenarios } = debt.projection;

  return (
    <Panel className="p-5">
      <div className="mb-4 flex items-baseline justify-between gap-3">
        <h2 className="text-sm font-semibold text-ink-200">When it clears</h2>
        <p className="text-xs text-ink-500">
          {debt.pace.hours_per_week_all != null
            ? `All-time pace: ${formatHours(debt.pace.hours_per_week_all)} hrs/week`
            : "Pace comes from logged reading sessions"}
        </p>
      </div>

      {current_pace ? (
        <ScenarioRow scenario={current_pace} label="Your pace" highlight />
      ) : (
        <p className="mb-3 rounded-xl bg-ink-850 px-3 py-2 text-xs leading-relaxed text-ink-400">
          Nothing read in the last 90 days, so there's no current pace to project from. Read a
          chapter in the app and this fills in.
        </p>
      )}

      <div className="mt-1 divide-y divide-white/[0.04]">
        {scenarios.map((scenario) => (
          <ScenarioRow key={scenario.hours_per_week} scenario={scenario} label="If you read" />
        ))}
      </div>
    </Panel>
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
