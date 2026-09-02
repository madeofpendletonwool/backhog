import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import { BookCover } from "./BookCover";
import { Button } from "./ui/primitives";
import { Dialog } from "./ui/Dialog";
import { Gi } from "./ui/Gi";
import { useUpdateEntry } from "@/hooks/useLibrary";
import { ApiError, api } from "@/lib/api";
import { accentStyle, byline, formatDuration } from "@/lib/format";
import { cn } from "@/lib/cn";
import type { GiName } from "@/lib/gameicons";
import type { BookEntry, ReadingPick } from "@/lib/types";

const PRESETS = [15, 30, 60, 90, 120];

const CATEGORIES = [
  { key: "continue", label: "Carry on", icon: "bookshelf", blurb: "Already in progress" },
  { key: "short_win", label: "Short win", icon: "zap", blurb: "Fits in your evening" },
  { key: "wildcard", label: "Wildcard", icon: "dices", blurb: "Never even opened" },
  { key: "rescue", label: "Shelf rescue", icon: "history", blurb: "Owned long, read little" },
] as const satisfies { key: string; label: string; icon: GiName; blurb: string }[];

type CategoryKey = (typeof CATEGORIES)[number]["key"];
type Excludes = Record<CategoryKey, string[]>;

const NO_EXCLUDES: Excludes = { continue: [], short_win: [], wildcard: [], rescue: [] };

/**
 * The books mirror of PickDialog: say how long you have and get four books
 * with human reasons instead of standing in front of the shelf. Every pick is
 * scored and explained server-side against your measured reading pace; this
 * just renders it.
 */
export function ReadDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const navigate = useNavigate();
  const update = useUpdateEntry();

  const [minutes, setMinutes] = useState(60);
  const [excludes, setExcludes] = useState<Excludes>(NO_EXCLUDES);

  // A fresh budget or a fresh opening starts from a clean slate of candidates.
  useEffect(() => {
    if (open) setExcludes(NO_EXCLUDES);
  }, [open, minutes]);

  const flatExcludes = Object.values(excludes).flat();
  const picks = useQuery({
    queryKey: ["readingPicks", minutes, ...[...flatExcludes].sort()],
    queryFn: () => api.readingPicks(minutes, flatExcludes),
    enabled: open,
  });

  const reroll = (key: CategoryKey) => {
    const current = picks.data?.[key];
    if (!current) return;
    setExcludes((prev) => ({ ...prev, [key]: [...prev[key], current.entry.id] }));
  };

  // Opening a book is the same move logging playtime is for a game: it comes
  // off the shelf and out of the queue, which the reader itself would do on
  // the first saved position anyway.
  const read = (entry: BookEntry) => {
    if (entry.status === "playing") {
      onClose();
      navigate(`/books/${entry.id}/read`);
      return;
    }
    update.mutate(
      { id: entry.id, patch: { status: "playing" } },
      {
        onSuccess: () => {
          onClose();
          navigate(`/books/${entry.id}/read`);
        },
      },
    );
  };

  const budgetLabel = formatDuration(minutes * 60);

  return (
    <Dialog open={open} onClose={onClose} label="What should I read?" className="max-w-2xl">
      <h2 className="flex items-center gap-2 text-lg font-semibold text-ink-100">
        <Gi name="book-pile" className="size-5 text-hl-bright" />
        What should I read?
      </h2>
      <p className="mt-1 text-sm text-ink-400">Four books for {budgetLabel}.</p>

      <div className="mt-4 flex flex-wrap items-center gap-2">
        {PRESETS.map((preset) => (
          <button
            key={preset}
            onClick={() => setMinutes(preset)}
            className={cn(
              "h-8 rounded-lg px-3 text-xs font-medium transition-colors focus-visible:focus-ring",
              minutes === preset
                ? "bg-brand-600 text-white"
                : "bg-ink-850 text-ink-300 hover:bg-ink-800 hover:text-ink-100",
            )}
          >
            {formatDuration(preset * 60)}
          </button>
        ))}
        <label className="flex h-8 items-center gap-1.5 rounded-lg bg-ink-850 px-3 text-xs text-ink-400">
          <Gi name="clock" className="size-3.5" />
          <input
            type="number"
            min={10}
            max={1440}
            step={15}
            value={PRESETS.includes(minutes) ? "" : minutes}
            onChange={(event) => {
              const value = Number(event.target.value);
              if (Number.isFinite(value) && value > 0) setMinutes(value);
            }}
            placeholder="custom"
            aria-label="Custom minutes"
            className="w-14 bg-transparent tabular-nums text-ink-100 outline-none placeholder:text-ink-500"
          />
          min
        </label>
      </div>

      <div className="mt-4 grid gap-3 sm:grid-cols-2">
        {picks.isLoading || !picks.data
          ? CATEGORIES.map((category) => <ReadCardSkeleton key={category.key} />)
          : CATEGORIES.map(({ key, label, blurb, icon }) => (
              <ReadCard
                key={key}
                label={label}
                blurb={blurb}
                icon={<Gi name={icon} className="size-3.5" />}
                pick={picks.data[key]}
                onReroll={() => reroll(key)}
                onRead={read}
                opening={update.isPending}
              />
            ))}
      </div>

      {picks.isError && (
        <p className="mt-3 text-xs text-red-400">
          {picks.error instanceof ApiError ? picks.error.message : "Couldn't load any picks."}
        </p>
      )}

      <div className="mt-5 flex justify-end">
        <Button variant="ghost" onClick={onClose}>
          Close
        </Button>
      </div>
    </Dialog>
  );
}

function ReadCard({
  label,
  blurb,
  icon,
  pick,
  onReroll,
  onRead,
  opening,
}: {
  label: string;
  blurb: string;
  icon: React.ReactNode;
  pick?: ReadingPick | null;
  onReroll?: () => void;
  onRead: (entry: BookEntry) => void;
  opening: boolean;
}) {
  return (
    <div className="flex flex-col rounded-xl border border-white/[0.06] bg-ink-850/50 p-3">
      <div className="flex items-baseline gap-1.5">
        <p className="flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-ink-300">
          <span className="text-brand-400">{icon}</span>
          {label}
        </p>
        <p className="truncate text-[11px] text-ink-500">{blurb}</p>
      </div>

      {pick ? (
        <div className="group relative mt-2.5 flex-1" style={accentStyle(pick.entry.book)}>
          {/* Accent glow sampled from the jacket; visible at rest — these are the heroes. */}
          <div
            className="pointer-events-none absolute -inset-1.5 -z-10 rounded-2xl opacity-20 blur-lg transition-opacity duration-300 group-hover:opacity-40"
            style={{ background: "var(--accent)" }}
            aria-hidden="true"
          />
          <div className="animate-fade-rise flex gap-3 rounded-xl bg-ink-900/80 p-2.5 ring-1 ring-white/[0.06]">
            <BookCover book={pick.entry.book} className="w-14 shrink-0" sizes="80px" />
            <div className="flex min-w-0 flex-1 flex-col">
              <p className="truncate text-sm font-semibold leading-snug text-ink-100">
                {pick.entry.book.title}
              </p>
              {byline(pick.entry.book) && (
                <p className="truncate text-[11px] text-ink-500">{byline(pick.entry.book)}</p>
              )}
              <p className="mt-0.5 line-clamp-2 text-xs leading-relaxed text-ink-300">
                {pick.reason}
              </p>
              <div className="mt-auto flex items-center gap-1.5 pt-2">
                <Button
                  size="sm"
                  variant="primary"
                  loading={opening}
                  onClick={() => onRead(pick.entry)}
                >
                  <Gi name="bookshelf" className="size-3.5" />
                  Read it
                </Button>
                {onReroll && (
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={onReroll}
                    aria-label={`Reroll ${label}`}
                  >
                    <Gi name="refresh" className="size-3.5" />
                  </Button>
                )}
              </div>
            </div>
          </div>
        </div>
      ) : (
        <div className="mt-2.5 flex flex-1 items-center justify-center rounded-xl border border-dashed border-white/10 px-4 py-6 text-center text-xs text-ink-600">
          Nothing to suggest here.
        </div>
      )}
    </div>
  );
}

function ReadCardSkeleton() {
  return (
    <div className="rounded-xl border border-white/[0.06] bg-ink-850/50 p-3">
      <div className="skeleton h-3.5 w-24 rounded" />
      <div className="skeleton mt-2.5 h-24 rounded-xl" />
    </div>
  );
}
