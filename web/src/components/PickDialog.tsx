import { useMutation } from "@tanstack/react-query";
import { cn } from "@/lib/cn";
import { Clock, Dices, PlayCircle, RefreshCw, Star } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import { GameCover } from "./GameCover";
import { Button, Select } from "./ui/primitives";
import { Dialog } from "./ui/Dialog";
import { useFacets, useUpdateEntry } from "@/hooks/useLibrary";
import { ApiError, api } from "@/lib/api";
import { accentStyle, formatDuration, releaseYear } from "@/lib/format";
import type { Entry } from "@/lib/types";

const LENGTHS = [
  { value: 0, label: "Any length" },
  { value: 5, label: "Under 5 hours" },
  { value: 10, label: "Under 10 hours" },
  { value: 20, label: "Under 20 hours" },
  { value: 40, label: "Under 40 hours" },
];

const RATINGS = [
  { value: 0, label: "Any rating" },
  { value: 70, label: "70+ on IGDB" },
  { value: 80, label: "80+ on IGDB" },
  { value: 90, label: "90+ on IGDB" },
];

/**
 * Picks a game out of the backlog so you don't have to. The whole app is about
 * a pile you never choose from, so the decision itself is the feature.
 */
export function PickDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const navigate = useNavigate();
  const { data: facets } = useFacets();
  const update = useUpdateEntry();

  const [maxHours, setMaxHours] = useState(0);
  const [minRating, setMinRating] = useState(0);
  const [genre, setGenre] = useState("");

  const pick = useMutation({
    mutationFn: () =>
      api.pick({
        max_hours: maxHours || undefined,
        min_rating: minRating || undefined,
        genre: genre ? Number(genre) : undefined,
      }),
  });

  // Reset between openings so a stale pick never flashes up.
  useEffect(() => {
    if (!open) pick.reset();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const entry = pick.data as Entry | undefined;
  const noMatches = pick.error instanceof ApiError && pick.error.status === 404;

  const startPlaying = () => {
    if (!entry) return;
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

  return (
    <Dialog open={open} onClose={onClose} label="Pick a game for me" className="max-w-md">
      <h2 className="flex items-center gap-2 text-lg font-semibold text-ink-100">
        <Dices className="size-5 text-brand-400" />
        Pick something for me
      </h2>
      <p className="mt-1 text-sm text-ink-400">
        One game from your backlog, chosen at random.
      </p>

      <div className="mt-5 grid grid-cols-2 gap-2">
        <Select
          value={maxHours}
          onChange={(event) => setMaxHours(Number(event.target.value))}
          aria-label="Maximum length"
          className="text-xs"
        >
          {LENGTHS.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </Select>
        <Select
          value={minRating}
          onChange={(event) => setMinRating(Number(event.target.value))}
          aria-label="Minimum rating"
          className="text-xs"
        >
          {RATINGS.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </Select>
        <Select
          value={genre}
          onChange={(event) => setGenre(event.target.value)}
          aria-label="Genre"
          className="col-span-2 text-xs"
        >
          <option value="">Any genre</option>
          {(facets?.genres ?? []).map((g) => (
            <option key={g.id} value={g.id}>
              {g.name}
            </option>
          ))}
        </Select>
      </div>

      <div className="mt-4 min-h-[7.5rem]">
        {entry ? (
          <div
            style={accentStyle(entry.game)}
            className="animate-fade-rise flex items-center gap-4 rounded-xl bg-ink-850/60 p-3"
          >
            <GameCover game={entry.game} className="w-20 shrink-0 rounded-lg" />
            <div className="min-w-0">
              <p className="font-semibold leading-snug text-ink-100">{entry.game.name}</p>
              <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-ink-400">
                {releaseYear(entry.game) && <span>{releaseYear(entry.game)}</span>}
                {entry.game.time_to_beat_main && (
                  <span className="inline-flex items-center gap-1">
                    <Clock className="size-3" />
                    {formatDuration(entry.game.time_to_beat_main)}
                  </span>
                )}
                {entry.game.igdb_rating != null && (
                  <span className="inline-flex items-center gap-1">
                    <Star className="size-3" />
                    {Math.round(entry.game.igdb_rating)}
                  </span>
                )}
              </div>
            </div>
          </div>
        ) : (
          <div
            className={cn(
              "flex h-full min-h-[7.5rem] flex-col items-center justify-center rounded-xl border border-dashed border-white/10 px-4 text-center",
              noMatches ? "text-amber-300/80" : "text-ink-600",
            )}
          >
            {noMatches ? (
              <p className="text-xs">Nothing in your backlog matches those filters.</p>
            ) : pick.isError ? (
              <p className="text-xs text-red-400">{(pick.error as Error).message}</p>
            ) : (
              <p className="text-xs">Hit the button and stop deliberating.</p>
            )}
          </div>
        )}
      </div>

      <div className="mt-5 flex justify-end gap-2">
        <Button variant="ghost" onClick={onClose}>
          Close
        </Button>
        <Button variant="secondary" loading={pick.isPending} onClick={() => pick.mutate()}>
          <RefreshCw className="size-4" />
          {entry ? "Spin again" : "Pick one"}
        </Button>
        {entry && (
          <Button variant="primary" loading={update.isPending} onClick={startPlaying}>
            <PlayCircle className="size-4" />
            Play it
          </Button>
        )}
      </div>
    </Dialog>
  );
}
