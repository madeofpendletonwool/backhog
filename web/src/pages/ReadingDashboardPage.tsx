import { Link } from "react-router-dom";

import { BookCover } from "@/components/BookCover";
import { ReadingSeasonCard } from "@/components/ReadingSeasonCard";
import { Gi } from "@/components/ui/Gi";
import { EmptyState, Panel, Skeleton } from "@/components/ui/primitives";
import { useReadingInsights } from "@/hooks/useBooks";
import { accentStyle, byline, formatHours } from "@/lib/format";
import type { BookSuperlative, ReadingInsights, ReadingPace } from "@/lib/types";

/**
 * "Your Reading Problem" — the books mirror of the gaming dashboard. Same
 * diagnosis-then-superlatives shape, same bedside manner; what changes is the
 * unit. A backlog of games is hours owed, a backlog of books is pages owed,
 * and the conversion between them is your own measured reading pace, which
 * this page shows rather than hides.
 */
export function ReadingDashboardPage() {
  const { data: insights, isLoading } = useReadingInsights();

  if (isLoading) {
    return (
      <div className="mx-auto max-w-5xl space-y-4 px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
        <Skeleton className="h-44" />
        <Skeleton className="h-24" />
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 3 }).map((_, index) => (
            <Skeleton key={index} className="h-40" />
          ))}
        </div>
      </div>
    );
  }

  const hasShelf = Boolean(insights && insights.headline.books_owned > 0);

  return (
    <div className="mx-auto max-w-5xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold tracking-tight text-ink-100">Your Reading Problem</h1>
        <p className="mt-1 text-sm text-ink-400">
          The shelf, honestly measured. You are not going to like it.
        </p>
      </header>

      {!hasShelf ? (
        <EmptyState
          icon={<Gi name="bookshelf" className="size-7" />}
          title="No confession on file"
          description="Add books to your shelf and the arithmetic will introduce itself."
        />
      ) : (
        <div className="space-y-4">
          <Diagnosis insights={insights!} />
          <ReadingSeasonCard />
          <PaceCard pace={insights!.pace} />
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
            {insights!.superlatives.map((superlative) => (
              <BookSuperlativeCard key={superlative.kind} superlative={superlative} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

/** The same medical-chart flourish the games dashboard opens with. */
function verdict(years: number | null, unread: number): { label: string; className: string } {
  if (unread === 0) return { label: "Shelf clear", className: verdictClass.emerald };
  if (years == null) return { label: "No pace on file", className: verdictClass.ink };
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

function Diagnosis({ insights }: { insights: ReadingInsights }) {
  const { headline } = insights;
  const verdictState = verdict(headline.years_at_current_rate, headline.unread_books);

  return (
    <Panel className="animate-fade-rise p-6">
      <div className="flex items-start justify-between gap-4">
        <p className="text-[11px] font-semibold uppercase tracking-wider text-ink-500">
          The diagnosis
        </p>
        <span
          className={`inline-flex items-center rounded-full border px-2.5 py-1 text-[11px] font-semibold uppercase tracking-wider ${verdictState.className}`}
        >
          {verdictState.label}
        </span>
      </div>

      <p className="mt-3 max-w-2xl text-lg leading-relaxed text-ink-300 sm:text-xl">
        You own{" "}
        <Emphasis>
          {headline.books_owned === 1 ? "one book" : `${headline.books_owned} books`}
        </Emphasis>
        .{" "}
        {headline.unread_books === 0 ? (
          <>Every one of them has been read. Nobody believes you.</>
        ) : (
          <>
            <Emphasis>
              {headline.unread_books} {headline.unread_books === 1 ? "is" : "are"} still unread
            </Emphasis>{" "}
            — <Emphasis>{formatPageCount(headline.pages_owed)}</Emphasis> and{" "}
            <Emphasis>{formatHours(headline.hours_owed)}</Emphasis> of your life,
            {headline.years_at_current_rate != null ? (
              <>
                {" "}
                or <Emphasis>{formatYears(headline.years_at_current_rate)}</Emphasis> at the rate
                you actually read.
              </>
            ) : (
              <> and no logged reading to measure it against.</>
            )}
          </>
        )}
      </p>

      <div className="mt-5 flex flex-wrap gap-x-8 gap-y-2 border-t border-edge pt-4">
        <Figure label="Books owned" value={String(headline.books_owned)} />
        <Figure label="Unread" value={String(headline.unread_books)} />
        <Figure label="Pages owed" value={Math.round(headline.pages_owed).toLocaleString()} />
        <Figure label="Hours owed" value={formatHours(headline.hours_owed)} />
        <Figure
          label="Years at your pace"
          value={
            headline.years_at_current_rate != null
              ? formatYears(headline.years_at_current_rate)
              : "—"
          }
        />
      </div>
    </Panel>
  );
}

/**
 * The pace, spelled out. "Years at your pace" is only meaningful if you can
 * see the two numbers it divides, so both are on the page: how fast you read
 * and how much you read.
 */
function PaceCard({ pace }: { pace: ReadingPace }) {
  return (
    <Panel className="p-5">
      <div className="mb-3 flex items-baseline justify-between gap-3">
        <h2 className="text-sm font-semibold text-ink-200">Your reading pace</h2>
        <p className="text-xs text-ink-500">
          {pace.measured
            ? `Measured from ${formatHours(pace.session_hours)} of logged reading`
            : "Assumed until you log some reading"}
        </p>
      </div>

      <div className="flex flex-wrap gap-x-8 gap-y-2">
        <Figure label="Pages per hour" value={String(pace.pages_per_hour)} />
        <Figure
          label="Hours per week"
          value={
            pace.hours_per_week_90d != null ? formatHours(pace.hours_per_week_90d) : "—"
          }
        />
        <Figure
          label="All-time weekly"
          value={
            pace.hours_per_week_all != null ? formatHours(pace.hours_per_week_all) : "—"
          }
        />
      </div>

      <p className="mt-3 text-xs leading-relaxed text-ink-500">
        {pace.measured ? (
          <>
            {pace.chars_per_hour.toLocaleString()} characters an hour, at{" "}
            {pace.chars_per_page.toLocaleString()} characters a page. Books you own the audiobook
            of are counted at their real running time instead.
          </>
        ) : (
          <>
            Reading in the app measures this for itself. Until then every page estimate uses{" "}
            {pace.pages_per_hour} pages an hour.
          </>
        )}
      </p>
    </Panel>
  );
}

function Emphasis({ children }: { children: React.ReactNode }) {
  return <span className="font-semibold tabular-nums tracking-tight text-ink-100">{children}</span>;
}

function Figure({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-[11px] font-medium uppercase tracking-wider text-ink-500">{label}</p>
      <p className="mt-0.5 text-lg font-semibold tabular-nums tracking-tight text-ink-100">
        {value}
      </p>
    </div>
  );
}

function formatYears(years: number): string {
  if (years < 1) return `${Math.round(years * 12)} mo`;
  return `${years.toFixed(1)} yr`;
}

function formatPageCount(pages: number): string {
  const rounded = Math.round(pages);
  return `${rounded.toLocaleString()} page${rounded === 1 ? "" : "s"}`;
}

const SUPERLATIVE_META: Record<
  BookSuperlative["kind"],
  { eyebrow: string; quip: string; icon: React.ReactNode }
> = {
  oldest_unopened: {
    eyebrow: "Longest-serving resident",
    quip: "Bought with real intent. Opened never.",
    icon: <Gi name="history" className="size-5" />,
  },
  longest_unread: {
    eyebrow: "The doorstop",
    quip: "You will start it during a quiet week that never arrives.",
    icon: <Gi name="mountain" className="size-5" />,
  },
  unread_author: {
    eyebrow: "Author you keep buying",
    quip: "A one-sided relationship, and you're the one not calling.",
    icon: <Gi name="pencil" className="size-5" />,
  },
  neglected_subject: {
    eyebrow: "Subject with the worst backlog",
    quip: "Deeply interested in theory. Untested in practice.",
    icon: <Gi name="tags" className="size-5" />,
  },
  restarted: {
    eyebrow: "Started, and started, and started",
    quip: "Chapter one knows you well by now.",
    icon: <Gi name="cycle" className="size-5" />,
  },
};

function BookSuperlativeCard({ superlative }: { superlative: BookSuperlative }) {
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

      {payload.book ? (
        <BookStat superlative={superlative} />
      ) : (
        <div className="mt-3">
          <p className="truncate text-xl font-semibold tracking-tight text-ink-100">
            {payload.name}
          </p>
          <p className="mt-1 text-sm font-medium text-brand-300">{superlative.label}</p>
        </div>
      )}

      <p className="mt-2.5 text-xs leading-relaxed text-ink-500">{meta.quip}</p>
    </Panel>
  );
}

function BookStat({ superlative }: { superlative: BookSuperlative }) {
  const { payload } = superlative;
  if (!payload.book) return null;
  const href = `/books/${payload.entry_id}`;

  return (
    <div className="mt-3 flex gap-4" style={accentStyle(payload.book)}>
      <Link
        to={href}
        className="w-16 shrink-0 overflow-hidden rounded-lg ring-1 ring-art transition-transform duration-300 ease-[var(--ease-spring)] hover:-translate-y-0.5 hover:ring-art-hover focus-visible:focus-ring sm:w-20"
        aria-label={payload.book.title}
      >
        <BookCover book={payload.book} sizes="96px" />
      </Link>
      <div className="min-w-0 self-center">
        <Link
          to={href}
          className="line-clamp-2 text-[15px] font-semibold leading-snug text-ink-100 hover:text-ink-max focus-visible:focus-ring"
        >
          {payload.book.title}
        </Link>
        {byline(payload.book) && (
          <p className="mt-0.5 truncate text-xs text-ink-500">{byline(payload.book)}</p>
        )}
        <p className="mt-1 text-sm font-medium text-brand-300">{superlative.label}</p>
      </div>
    </div>
  );
}
