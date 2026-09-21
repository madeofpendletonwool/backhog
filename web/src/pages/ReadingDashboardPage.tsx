import { useQuery } from "@tanstack/react-query";
import { Link, useOutletContext } from "react-router-dom";

import { BookCover } from "@/components/BookCover";
import { ProgressBar } from "@/components/ProgressBar";
import { ReadingSeasonCard } from "@/components/ReadingSeasonCard";
import { Gi } from "@/components/ui/Gi";
import { Button, EmptyState, Panel, Skeleton } from "@/components/ui/primitives";
import { useAudioPlayer } from "@/hooks/useAudioPlayer";
import { useReadingInsights, useReadingNow } from "@/hooks/useBooks";
import { continueLabel, useContinueReading } from "@/hooks/useContinueReading";
import { useQueue } from "@/hooks/useLibrary";
import { api, bookCoverUrl } from "@/lib/api";
import { chapterTitle, formatPage } from "@/lib/booktext";
import { accentStyle, byline, formatHours, formatTimecode, relativeTime } from "@/lib/format";
import {
  isBookEntry,
  type BookEntry,
  type BookSuperlative,
  type ReadingInsights,
  type ReadingNowBook,
  type ReadingPace,
} from "@/lib/types";

/**
 * The reading dashboard: the page the books arena opens on.
 *
 * It answers the question a reader actually arrives with — "where was I?" —
 * before anything else. The book you are in the middle of is the hero, one
 * click from the page you stopped on; the rest of what is in progress and
 * the top of the queue follow; and only then does "Your Reading Problem" —
 * the diagnosis, the pace and the superlatives — get its say. The stats are
 * still the fun part of the page; they are just not the part you came for.
 */
export function ReadingDashboardPage() {
  const { openReadDialog } = useOutletContext<{ openReadDialog: () => void }>();
  const { data: insights, isLoading: insightsLoading } = useReadingInsights();
  const { data: now, isLoading: nowLoading } = useReadingNow();
  const { data: queue } = useQueue();
  const player = useAudioPlayer();

  if (nowLoading || insightsLoading) {
    return (
      <div className="mx-auto max-w-5xl space-y-4 px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
        <Skeleton className="h-64" />
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
  const inProgress = now?.books ?? [];

  // The book in the player is the book you are consuming, whatever the
  // timestamps say — a tape that is rolling has not written its checkpoint
  // yet. It leads whenever it is one of the books in progress.
  const playing = player.entry ? inProgress.find((b) => b.entry.id === player.entry?.id) : null;
  const hero = playing ?? inProgress[0] ?? null;
  const rest = inProgress.filter((b) => b !== hero);

  // The queue holds both arenas; the top of the reading queue is its books.
  const upNext = (queue?.entries ?? []).filter(isBookEntry).slice(0, 5);

  return (
    <div className="mx-auto max-w-5xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <header className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-ink-100">Reading</h1>
          <p className="mt-1 text-sm text-ink-400">
            {hero
              ? inProgress.length === 1
                ? "One book on the go."
                : `${inProgress.length} books on the go.`
              : "Nothing on the go. Pick something up."}
          </p>
        </div>
        <Button variant="secondary" onClick={openReadDialog}>
          <Gi name="dices" className="size-3.5" />
          What should I read?
        </Button>
      </header>

      {!hasShelf ? (
        <EmptyState
          icon={<Gi name="bookshelf" className="size-7" />}
          title="Nothing on the shelf yet"
          description="Add books to your shelf and this page becomes the one you open to keep reading."
        />
      ) : (
        <div className="space-y-4">
          {hero ? (
            <ContinueHero book={hero} />
          ) : (
            <NothingInProgress next={upNext[0] ?? null} onAsk={openReadDialog} />
          )}
          {rest.length > 0 && <InProgressRow books={rest} />}
          {upNext.length > 0 && <UpNext entries={upNext} />}
          <ReadingSeasonCard />
          <Diagnosis insights={insights!} />
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

/* ------------------------------------------------------------ in progress */

/**
 * The book you are in the middle of, and the one button that gets you back
 * into it. The position query is the same one the detail page and the
 * player hydrate from, so the chapter line here and the "where you are"
 * panel there can never disagree — and clicking through is a cache hit.
 */
function ContinueHero({ book }: { book: ReadingNowBook }) {
  const { entry, percent, remaining_hours, audio, last_read_at } = book;
  const continueReading = useContinueReading();
  const player = useAudioPlayer();
  const { data: position } = useQuery({
    queryKey: ["bookPosition", entry.id],
    queryFn: () => api.bookPosition(entry.id),
  });
  const { data: timeline } = useQuery({
    queryKey: ["bookAudio", entry.id],
    queryFn: () => api.bookAudio(entry.id),
    enabled: audio,
  });

  const paged = position?.position_mode === "page";
  const hasText = Boolean(position && (position.char_count > 0 || position.page_count > 0));
  const hasAudio = Boolean(timeline && timeline.tracks.length > 0);
  const inPlayer = player.entry?.id === entry.id;

  // What the position says, in whichever space is most concrete.
  const where: string[] = [];
  if (position) {
    if (paged) where.push(`Page ${(position.page_index ?? 0) + 1} of ${position.page_count}`);
    else {
      if (position.chapter) where.push(chapterTitle(position.chapter));
      const page = formatPage(position.page);
      if (page) where.push(page);
      if (position.audio) where.push(formatTimecode(position.audio.seconds));
    }
  }
  if (remaining_hours != null && remaining_hours > 0) {
    where.push(`${formatHours(remaining_hours)} left${audio ? " on tape" : ""}`);
  }

  return (
    <section
      style={accentStyle(entry.book)}
      className="panel animate-fade-rise relative isolate overflow-hidden p-5 sm:p-6"
      aria-label="Continue reading"
    >
      {entry.book.cover_url && (
        <div
          className="absolute inset-0 -z-10 scale-110 bg-cover bg-center opacity-20 blur-3xl"
          style={{ backgroundImage: `url(${bookCoverUrl(entry.book.id)})` }}
          aria-hidden="true"
        />
      )}
      <div className="absolute inset-0 -z-10 bg-gradient-to-r from-ink-950/70 via-ink-950/40 to-ink-950/70" />

      <div className="flex gap-4 sm:gap-5">
        <Link
          to={`/books/${entry.id}`}
          className="w-24 shrink-0 self-start overflow-hidden rounded-xl ring-1 ring-art shadow-2xl transition-transform duration-300 ease-[var(--ease-spring)] hover:-translate-y-0.5 hover:ring-art-hover focus-visible:focus-ring sm:w-36"
          aria-label={entry.book.title}
        >
          <BookCover book={entry.book} sizes="160px" />
        </Link>

        <div className="flex min-w-0 flex-1 flex-col">
          <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
            <p className="text-[11px] font-semibold uppercase tracking-wider text-ink-500">
              {inPlayer && player.playing ? "Now playing" : "Continue reading"}
            </p>
            {last_read_at && (
              <p className="text-xs text-ink-500">Last opened {relativeTime(last_read_at)}</p>
            )}
          </div>

          <Link
            to={`/books/${entry.id}`}
            className="mt-1.5 line-clamp-2 text-xl font-semibold leading-tight tracking-tight text-ink-max hover:text-ink-100 focus-visible:focus-ring sm:text-3xl"
          >
            {entry.book.title}
          </Link>
          {byline(entry.book) && <p className="mt-1 text-sm text-ink-300">{byline(entry.book)}</p>}

          <div className="mt-4">
            <div className="mb-1.5 flex items-baseline justify-between gap-3 text-xs">
              <p className="truncate text-ink-400">{where.join(" · ") || "Not started"}</p>
              <p className="shrink-0 font-display uppercase tracking-wider text-ink-200">
                {Math.round(percent)}%
              </p>
            </div>
            <ProgressBar percent={percent} complete={percent >= 100} />
          </div>

          <div className="mt-auto flex flex-wrap items-center gap-2.5 pt-5">
            {/* The primary is whichever end the book was last put down at;
                the other end, when it exists, is one click further. */}
            {entry.progress_source === "listen" && hasAudio ? (
              <>
                <ListenAction entry={entry} primary />
                {hasText && <ReadAction entry={entry} />}
              </>
            ) : (
              <>
                {(hasText || !hasAudio) && (
                  <Button variant="primary" onClick={() => continueReading(entry)}>
                    <Gi name="scroll-unfurled" className="size-3.5" />
                    {continueLabel(entry)}
                  </Button>
                )}
                {hasAudio && <ListenAction entry={entry} primary={!hasText} />}
              </>
            )}
            <Link to={`/books/${entry.id}`} className="text-sm text-ink-400 hover:text-ink-100">
              Details
            </Link>
          </div>
        </div>
      </div>
    </section>
  );
}

function ReadAction({ entry }: { entry: BookEntry }) {
  return (
    <Link to={`/books/${entry.id}/read`}>
      <Button variant="secondary">
        <Gi name="scroll-unfurled" className="size-3.5" />
        Read
      </Button>
    </Link>
  );
}

/** The player's resume control, the same rules as the detail page's ListenButton. */
function ListenAction({ entry, primary }: { entry: BookEntry; primary: boolean }) {
  const player = useAudioPlayer();
  const active = player.entry?.id === entry.id;
  return (
    <Button
      variant={primary && !active ? "primary" : "secondary"}
      onClick={() => (active ? player.toggle() : player.open(entry, { autoplay: true }))}
    >
      <Gi name={active && player.playing ? "pause" : "headphones"} className="size-3.5" />
      {active ? (player.playing ? "Pause" : "Resume") : "Listen"}
    </Button>
  );
}

/** The other books in progress: cover, bar, and a direct way back in. */
function InProgressRow({ books }: { books: ReadingNowBook[] }) {
  return (
    <section aria-label="Also in progress">
      <h2 className="mb-3 text-[11px] font-semibold uppercase tracking-wider text-ink-500">
        Also in progress
      </h2>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {books.map((book) => (
          <InProgressCard key={book.entry.id} book={book} />
        ))}
      </div>
    </section>
  );
}

function InProgressCard({ book }: { book: ReadingNowBook }) {
  const { entry, percent, remaining_hours, last_read_at } = book;
  const continueReading = useContinueReading();
  const hint = [
    last_read_at ? relativeTime(last_read_at) : null,
    remaining_hours != null && remaining_hours > 0 ? `${formatHours(remaining_hours)} left` : null,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <Panel className="flex gap-3.5 p-3.5" >
      <div style={accentStyle(entry.book)} className="contents">
        <Link
          to={`/books/${entry.id}`}
          className="w-14 shrink-0 overflow-hidden rounded-lg ring-1 ring-art focus-visible:focus-ring"
          aria-label={entry.book.title}
        >
          <BookCover book={entry.book} sizes="64px" />
        </Link>
        <div className="flex min-w-0 flex-1 flex-col">
          <Link
            to={`/books/${entry.id}`}
            className="line-clamp-2 text-sm font-semibold leading-snug text-ink-100 hover:text-ink-max focus-visible:focus-ring"
          >
            {entry.book.title}
          </Link>
          {hint && <p className="mt-0.5 truncate text-xs text-ink-500">{hint}</p>}
          <div className="mt-auto flex items-center gap-3 pt-2">
            <div className="min-w-0 flex-1">
              <ProgressBar percent={percent} complete={percent >= 100} />
            </div>
            <span className="shrink-0 text-xs tabular-nums text-ink-400">{Math.round(percent)}%</span>
            <Button size="sm" variant="secondary" onClick={() => continueReading(entry)}>
              {entry.progress_source === "listen" ? "Listen" : "Read"}
            </Button>
          </div>
        </div>
      </div>
    </Panel>
  );
}

/**
 * The hero's stand-in when nothing is in progress: the top of the queue and
 * the picks dialog, which is the whole point of the queue existing.
 */
function NothingInProgress({ next, onAsk }: { next: BookEntry | null; onAsk: () => void }) {
  return (
    <Panel className="animate-fade-rise p-6">
      <p className="text-[11px] font-semibold uppercase tracking-wider text-ink-500">Nothing on the go</p>
      {next ? (
        <div className="mt-3 flex gap-4" style={accentStyle(next.book)}>
          <Link
            to={`/books/${next.id}`}
            className="w-20 shrink-0 overflow-hidden rounded-lg ring-1 ring-art focus-visible:focus-ring"
            aria-label={next.book.title}
          >
            <BookCover book={next.book} sizes="96px" />
          </Link>
          <div className="min-w-0 self-center">
            <p className="text-xs text-ink-500">Next in your queue</p>
            <Link
              to={`/books/${next.id}`}
              className="mt-0.5 line-clamp-2 text-lg font-semibold leading-snug text-ink-100 hover:text-ink-max focus-visible:focus-ring"
            >
              {next.book.title}
            </Link>
            {byline(next.book) && <p className="mt-0.5 text-sm text-ink-400">{byline(next.book)}</p>}
            <div className="mt-3 flex flex-wrap gap-2">
              <Link to={`/books/${next.id}`}>
                <Button variant="primary">Open</Button>
              </Link>
              <Button variant="secondary" onClick={onAsk}>
                <Gi name="dices" className="size-3.5" />
                Something else
              </Button>
            </div>
          </div>
        </div>
      ) : (
        <div className="mt-3 flex flex-wrap items-center gap-4">
          <p className="text-sm text-ink-300">The queue is empty too. Let the shelf choose.</p>
          <Button variant="primary" onClick={onAsk}>
            <Gi name="dices" className="size-3.5" />
            What should I read?
          </Button>
        </div>
      )}
    </Panel>
  );
}

/** The top of the reading queue, so starting the next book is one click. */
function UpNext({ entries }: { entries: BookEntry[] }) {
  return (
    <section aria-label="Up next">
      <div className="mb-3 flex items-baseline justify-between">
        <h2 className="text-[11px] font-semibold uppercase tracking-wider text-ink-500">Up next</h2>
        <Link to="/queue?media=book" className="text-xs text-ink-400 hover:text-ink-100">
          Reading queue
        </Link>
      </div>
      <div className="flex gap-3 overflow-x-auto pb-1">
        {entries.map((entry) => (
          <Link
            key={entry.id}
            to={`/books/${entry.id}`}
            style={accentStyle(entry.book)}
            className="group w-24 shrink-0 focus-visible:focus-ring sm:w-28"
            title={entry.book.title}
          >
            <div className="overflow-hidden rounded-lg ring-1 ring-art transition-transform duration-300 ease-[var(--ease-spring)] group-hover:-translate-y-0.5 group-hover:ring-art-hover">
              <BookCover book={entry.book} sizes="112px" />
            </div>
            <p className="mt-1.5 line-clamp-2 text-xs font-medium leading-snug text-ink-200">
              {entry.book.title}
            </p>
          </Link>
        ))}
      </div>
    </section>
  );
}

/* --------------------------------------------------- your reading problem */

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
