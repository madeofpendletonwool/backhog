import { useMutation, useQuery } from "@tanstack/react-query";
import { cn } from "@/lib/cn";
import { Gi } from "./ui/Gi";
import type { GiName } from "@/lib/gameicons";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import { GameCover } from "./GameCover";
import { Button } from "./ui/primitives";
import { Dialog } from "./ui/Dialog";
import { useUpdateEntry } from "@/hooks/useLibrary";
import { ApiError, api } from "@/lib/api";
import { accentStyle, formatDuration } from "@/lib/format";
import type { GameEntry, TonightPick } from "@/lib/types";

const PRESETS = [30, 60, 90, 120, 180];

const CATEGORIES = [
  { key: "continue", label: "Continue", icon: "swords", blurb: "Something in progress" },
  { key: "short_win", label: "Short win", icon: "zap", blurb: "Fits in your budget" },
  { key: "wildcard", label: "Wildcard", icon: "dices", blurb: "Never even started" },
  { key: "rescue", label: "Backlog rescue", icon: "trophy", blurb: "Owned long, played little" },
] as const satisfies { key: string; label: string; icon: GiName; blurb: string }[];

type CategoryKey = (typeof CATEGORIES)[number]["key"];
type Excludes = Record<CategoryKey, string[]>;

const NO_EXCLUDES: Excludes = { continue: [], short_win: [], wildcard: [], rescue: [] };

/**
 * The anti-deliberation device: say how long tonight is and get four picks with
 * human reasons, instead of staring at the wall of covers deciding. Every pick
 * is scored and explained server-side; this just renders it.
 */
export function PickDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const navigate = useNavigate();
  const update = useUpdateEntry();

  const [minutes, setMinutes] = useState(90);
  const [excludes, setExcludes] = useState<Excludes>(NO_EXCLUDES);

  // A fresh budget or a fresh opening starts from a clean slate of candidates.
  useEffect(() => {
    if (open) setExcludes(NO_EXCLUDES);
  }, [open, minutes]);

  const flatExcludes = Object.values(excludes).flat();
  const picks = useQuery({
    queryKey: ["tonight", minutes, ...[...flatExcludes].sort()],
    queryFn: () => api.tonight(minutes, flatExcludes),
    enabled: open,
  });

  // The old random pick, kept as the "stop thinking entirely" escape hatch.
  const roll = useMutation({ mutationFn: () => api.pick({}) });
  useEffect(() => {
    if (!open) roll.reset();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const reroll = (key: CategoryKey) => {
    const current = picks.data?.[key];
    if (!current) return;
    setExcludes((prev) => ({ ...prev, [key]: [...prev[key], current.entry.id] }));
  };

  const play = (entry: GameEntry) => {
    update.mutate(
      { id: entry.id, patch: { status: "playing" } },
      {
        onSuccess: () => {
          onClose();
          navigate(`/game/${entry.id}`);
        },
      },
    );
  };

  const budgetLabel = formatDuration(minutes * 60);

  return (
    <Dialog open={open} onClose={onClose} label="What should I play?" className="max-w-2xl">
      <h2 className="flex items-center gap-2 text-lg font-semibold text-ink-100">
        <Gi name="dices" className="size-5 text-gold-bright" />
        What should I play?
      </h2>
      <p className="mt-1 text-sm text-ink-400">Tonight's picks for {budgetLabel}.</p>

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
            className="w-14 bg-transparent text-ink-100 tabular-nums outline-none placeholder:text-ink-500"
          />
          min
        </label>
      </div>

      <div className="mt-4 grid gap-3 sm:grid-cols-2">
        {picks.isLoading || !picks.data
          ? CATEGORIES.map((category) => <PickCardSkeleton key={category.key} />)
          : CATEGORIES.map(({ key, label, blurb, icon }) => (
              <PickCard
                key={key}
                label={label}
                blurb={blurb}
                icon={<Gi name={icon} className="size-3.5" />}
                pick={picks.data[key]}
                onReroll={() => reroll(key)}
                onPlay={play}
                playing={update.isPending}
              />
            ))}
      </div>

      {picks.isError && (
        <p className="mt-3 text-xs text-red-400">
          {picks.error instanceof ApiError ? picks.error.message : "Couldn't load tonight's picks."}
        </p>
      )}

      {roll.data && (
        <div className="mt-3">
          <PickCard
            label="Just rolled"
            blurb="Straight from the whole backlog"
            icon={<Gi name="dices" className="size-3.5" />}
            pick={{ entry: roll.data, score: 0, reason: "Picked at random — no thinking involved." }}
            onPlay={play}
            playing={update.isPending}
          />
        </div>
      )}

      <div className="mt-5 flex justify-end gap-2">
        <Button variant="ghost" onClick={onClose}>
          Close
        </Button>
        <Button
          variant="secondary"
          loading={roll.isPending}
          onClick={() => roll.mutate()}
          disabled={roll.data != null}
        >
          <Gi name="refresh" className="size-4" />
          Just roll one
        </Button>
      </div>
    </Dialog>
  );
}

function PickCard({
  label,
  blurb,
  icon,
  pick,
  onReroll,
  onPlay,
  playing,
}: {
  label: string;
  blurb: string;
  icon: React.ReactNode;
  pick?: TonightPick | null;
  onReroll?: () => void;
  onPlay: (entry: GameEntry) => void;
  playing: boolean;
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
        <div className="group relative mt-2.5 flex-1" style={accentStyle(pick.entry.game)}>
          {/* Accent glow sampled from the cover; visible at rest — these are the heroes. */}
          <div
            className="pointer-events-none absolute -inset-1.5 -z-10 rounded-2xl opacity-20 blur-lg transition-opacity duration-300 group-hover:opacity-40"
            style={{ background: "var(--accent)" }}
            aria-hidden="true"
          />
          <div className="animate-fade-rise flex gap-3 rounded-xl bg-ink-900/80 p-2.5 ring-1 ring-white/[0.06]">
            <GameCover game={pick.entry.game} className="w-16 shrink-0" />
            <div className="flex min-w-0 flex-1 flex-col">
              <p className="truncate text-sm font-semibold leading-snug text-ink-100">
                {pick.entry.game.name}
              </p>
              <p className="mt-0.5 line-clamp-2 text-xs leading-relaxed text-ink-300">
                {pick.reason}
              </p>
              <div className="mt-auto flex items-center gap-1.5 pt-2">
                <Button
                  size="sm"
                  variant="primary"
                  loading={playing}
                  onClick={() => onPlay(pick.entry)}
                >
                  <Gi name="play" className="size-3.5" />
                  Play it
                </Button>
                {onReroll && (
                  <Button size="sm" variant="ghost" onClick={onReroll} aria-label={`Reroll ${label}`}>
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

function PickCardSkeleton() {
  return (
    <div className="rounded-xl border border-white/[0.06] bg-ink-850/50 p-3">
      <div className="skeleton h-3.5 w-24 rounded" />
      <div className="skeleton mt-2.5 h-24 rounded-xl" />
    </div>
  );
}
