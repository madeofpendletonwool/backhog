import { Link } from "react-router-dom";

import { GameCover } from "@/components/GameCover";
import { SeasonCard } from "@/components/SeasonCard";
import { Gi } from "@/components/ui/Gi";
import { EmptyState, Panel, Skeleton } from "@/components/ui/primitives";
import { useInsights } from "@/hooks/useLibrary";
import { accentStyle, formatHours } from "@/lib/format";
import type { Insights, Superlative } from "@/lib/types";

/**
 * "Your Gaming Problem" — the ridiculous-stats dashboard. One diagnosis up
 * top, then a wall of superlatives that makes the app fun to open.
 */
export function DashboardPage() {
  const { data: insights, isLoading } = useInsights();

  if (isLoading) {
    return (
      <div className="mx-auto max-w-5xl space-y-4 px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
        <Skeleton className="h-44" />
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 3 }).map((_, index) => (
            <Skeleton key={index} className="h-40" />
          ))}
        </div>
      </div>
    );
  }

  const hasLibrary = Boolean(insights && insights.headline.games_owned > 0);

  return (
    <div className="mx-auto max-w-5xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold tracking-tight text-ink-100">Your Gaming Problem</h1>
        <p className="mt-1 text-sm text-ink-400">
          A routine check-up on the pile. The prognosis is never good.
        </p>
      </header>

      {!hasLibrary ? (
        <EmptyState
          icon={<Gi name="gauge" className="size-7" />}
          title="Nothing to confess yet"
          description="Add games to your library and the embarrassing numbers will find you on their own."
        />
      ) : (
        <div className="space-y-4">
          <Diagnosis insights={insights!} />
          <SeasonCard />
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
            {insights!.superlatives.map((superlative) => (
              <SuperlativeCard key={superlative.kind} superlative={superlative} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

/** The medical-chart flourish that opens the page. */
function verdict(years: number | null, unplayed: number): { label: string; className: string } {
  if (unplayed === 0) {
    return { label: "Clean bill of health", className: verdictClass.emerald };
  }
  if (years == null) {
    return { label: "No pace on file", className: verdictClass.ink };
  }
  if (years > 5) return { label: "Terminal", className: verdictClass.red };
  if (years > 2) return { label: "Chronic", className: verdictClass.amber };
  if (years > 0.75) return { label: "Treatable", className: verdictClass.cyan };
  return { label: "Manageable", className: verdictClass.emerald };
}

const verdictClass = {
  red: "border-red-400/30 bg-red-400/10 text-red-300",
  amber: "border-amber-400/30 bg-amber-400/10 text-amber-300",
  cyan: "border-cyan-400/30 bg-cyan-400/10 text-cyan-300",
  emerald: "border-emerald-400/30 bg-emerald-400/10 text-emerald-300",
  ink: "border-edge-strong bg-fill-hover text-ink-300",
};

function Diagnosis({ insights }: { insights: Insights }) {
  const { headline } = insights;
  const verdictState = verdict(headline.years_at_current_rate, headline.unplayed_games);

  return (
    <Panel className="animate-fade-rise p-6">
      <div className="flex items-start justify-between gap-4">
        <p className="text-[11px] font-semibold uppercase tracking-wider text-ink-500">The diagnosis</p>
        <span
          className={`inline-flex items-center rounded-full border px-2.5 py-1 text-[11px] font-semibold uppercase tracking-wider ${verdictState.className}`}
        >
          {verdictState.label}
        </span>
      </div>

      <p className="mt-3 max-w-2xl text-lg leading-relaxed text-ink-300 sm:text-xl">
        You own{" "}
        <Emphasis>{headline.games_owned === 1 ? "one game" : `${headline.games_owned} games`}</Emphasis>.{" "}
        {headline.unplayed_games === 0 ? (
          <>Nothing is waiting on you. Suspicious, but healthy.</>
        ) : (
          <>
            <Emphasis>
              {headline.unplayed_games} {headline.unplayed_games === 1 ? "is" : "are"} still waiting
            </Emphasis>{" "}
            — <Emphasis>{formatHours(headline.hours_remaining)}</Emphasis> of playing,
            {headline.years_at_current_rate != null ? (
              <>
                {" "}
                or <Emphasis>{formatYears(headline.years_at_current_rate)}</Emphasis> at your current
                pace.
              </>
            ) : (
              <> and no recent pace to measure it against.</>
            )}
          </>
        )}
      </p>

      <div className="mt-5 flex flex-wrap gap-x-8 gap-y-2 border-t border-edge pt-4">
        <Figure label="Games owned" value={String(headline.games_owned)} />
        <Figure label="Unplayed" value={String(headline.unplayed_games)} />
        <Figure label="Hours owed" value={formatHours(headline.hours_remaining)} />
        <Figure
          label="Years at your pace"
          value={headline.years_at_current_rate != null ? formatYears(headline.years_at_current_rate) : "—"}
        />
      </div>
    </Panel>
  );
}

function Emphasis({ children }: { children: React.ReactNode }) {
  return (
    <span className="font-semibold tabular-nums tracking-tight text-ink-100">{children}</span>
  );
}

function Figure({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-[11px] font-medium uppercase tracking-wider text-ink-500">{label}</p>
      <p className="mt-0.5 text-lg font-semibold tabular-nums tracking-tight text-ink-100">{value}</p>
    </div>
  );
}

function formatYears(years: number): string {
  if (years < 1) return `${Math.round(years * 12)} mo`;
  return `${years.toFixed(1)} yr`;
}

const SUPERLATIVE_META: Record<
  Superlative["kind"],
  { eyebrow: string; quip: string; icon: React.ReactNode }
> = {
  oldest_untouched: {
    eyebrow: "Longest-serving resident",
    quip: "Bought, shelved, never once opened.",
    icon: <Gi name="history" className="size-5" />,
  },
  longest_unplayed: {
    eyebrow: "The biggest one",
    quip: "The game you keep meaning to start. Someday.",
    icon: <Gi name="mountain" className="size-5" />,
  },
  neglected_genre: {
    eyebrow: "Genre you keep buying",
    quip: "You keep buying them. They keep not getting played.",
    icon: <Gi name="tags" className="size-5" />,
  },
  worst_platform: {
    eyebrow: "Where the pile lives",
    quip: "Every platform has a backlog. This one has the backlog.",
    icon: <Gi name="monitor" className="size-5" />,
  },
  neglected_year: {
    eyebrow: "Best vintage, least opened",
    quip: "Collected religiously. Played rarely.",
    icon: <Gi name="calendar" className="size-5" />,
  },
};

function SuperlativeCard({ superlative }: { superlative: Superlative }) {
  const meta = SUPERLATIVE_META[superlative.kind];
  const { payload } = superlative;

  return (
    <Panel className="animate-fade-rise p-4">
      <div className="flex items-center justify-between gap-3">
        <p className="text-[11px] font-semibold uppercase tracking-wider text-ink-500">
          {meta.eyebrow}
        </p>
        <span className="text-ink-600">{meta.icon}</span>
      </div>

      {payload.game ? (
        <GameStat superlative={superlative} />
      ) : (
        <div className="mt-3">
          <p className="truncate text-xl font-semibold tracking-tight text-ink-100">
            {payload.year != null ? payload.year : payload.name}
          </p>
          <p className="mt-1 text-sm font-medium text-brand-300">{superlative.label}</p>
        </div>
      )}

      <p className="mt-2.5 text-xs leading-relaxed text-ink-500">{meta.quip}</p>
    </Panel>
  );
}

function GameStat({ superlative }: { superlative: Superlative }) {
  const { payload } = superlative;
  if (!payload.game) return null;

  return (
    <div className="mt-3 flex gap-4" style={accentStyle(payload.game)}>
      <Link
        to={`/game/${payload.entry_id}`}
        className="w-16 shrink-0 overflow-hidden rounded-lg ring-1 ring-art transition-transform duration-300 ease-[var(--ease-spring)] hover:-translate-y-0.5 hover:ring-art-hover focus-visible:focus-ring sm:w-20"
        aria-label={payload.game.name}
      >
        <GameCover game={payload.game} sizes="96px" />
      </Link>
      <div className="min-w-0 self-center">
        <Link
          to={`/game/${payload.entry_id}`}
          className="line-clamp-2 text-[15px] font-semibold leading-snug text-ink-100 hover:text-ink-max focus-visible:focus-ring"
        >
          {payload.game.name}
        </Link>
        <p className="mt-1 text-sm font-medium text-brand-300">{superlative.label}</p>
      </div>
    </div>
  );
}
