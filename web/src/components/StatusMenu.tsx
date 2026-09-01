import { cn } from "@/lib/cn";
import { useState } from "react";

import { useUpdateEntry } from "@/hooks/useLibrary";
import { entryTitle } from "@/lib/entry";
import {
  QUICK_STATUSES,
  isGameEntry,
  statusLabel,
  type Entry,
  type GameEntry,
  type Status,
} from "@/lib/types";
import { STATUS_ICONS } from "./StatusBadge";
import { Gi } from "./ui/Gi";
import { Dialog } from "./ui/Dialog";

const activeStyles: Record<Status, string> = {
  backlog: "bg-slate-500 text-white",
  playing: "bg-cyan-500 text-ink-950",
  played: "bg-emerald-500 text-ink-950",
  dropped: "bg-red-500 text-white",
  ignored: "bg-zinc-500 text-white",
  wishlist: "bg-amber-500 text-ink-950",
};

/**
 * A compact segmented control for changing an entry's status straight from the
 * grid, so the most common action never needs a page change.
 *
 * `statuses` defaults to the quick-access set (no wishlist). The detail page
 * passes the full list so wishlist can be set there.
 *
 * Marking a game finished without a "playing on" platform first asks which
 * platform it was finished on: platform progress counts the platform you
 * actually played, not the ones the game shipped for. Books skip that
 * question entirely — a printing is chosen when the book is added, and it is
 * not a platform.
 */
export function StatusMenu({
  entry,
  size = "sm",
  statuses = QUICK_STATUSES,
}: {
  entry: Entry;
  size?: "sm" | "md";
  statuses?: Status[];
}) {
  const update = useUpdateEntry();
  const [askPlatform, setAskPlatform] = useState(false);

  const title = entryTitle(entry);
  const label = (status: Status) => statusLabel(status, entry.media_type);

  const markPlayed = (platformId: number | null) =>
    update.mutate({
      id: entry.id,
      patch: platformId == null ? { status: "played" } : { status: "played", platform_id: platformId },
    });

  const shouldAsk =
    isGameEntry(entry) &&
    statuses.includes("played") &&
    entry.platform_id == null &&
    entry.game.platforms.length > 0;

  return (
    <>
      <div
        role="group"
        aria-label={`Status for ${title}`}
        className={cn(
          "gap-0.5 bg-ink-900/95 p-1 ring-1 ring-white/10 backdrop-blur-md",
          size === "md" ? "grid grid-cols-3 sm:flex sm:items-center" : "flex items-center",
          update.isPending && "opacity-60",
        )}
      >
        {statuses.map((status) => {
          const isActive = entry.status === status;
          return (
            <button
              key={status}
              type="button"
              title={label(status)}
              aria-label={label(status)}
              aria-pressed={isActive}
              disabled={update.isPending}
              onClick={(event) => {
                event.preventDefault();
                event.stopPropagation();
                if (isActive) return;
                if (status === "played" && shouldAsk) {
                  setAskPlatform(true);
                  return;
                }
                update.mutate({ id: entry.id, patch: { status } });
              }}
              className={cn(
                "flex flex-1 items-center justify-center transition-colors",
                size === "sm" ? "h-7" : "h-8 px-3",
                isActive
                  ? activeStyles[status]
                  : "text-ink-400 hover:bg-white/[0.08] hover:text-ink-100",
                "focus-visible:focus-ring disabled:cursor-not-allowed",
              )}
            >
              <Gi name={STATUS_ICONS[status]} className="size-3.5" />
              {size === "md" && <span className="ml-1.5 text-xs font-medium">{label(status)}</span>}
            </button>
          );
        })}
      </div>

      {isGameEntry(entry) && (
        <PlatformPrompt
          open={askPlatform}
          entry={entry}
          pending={update.isPending}
          onPick={(platformId) => {
            setAskPlatform(false);
            markPlayed(platformId);
          }}
        />
      )}
    </>
  );
}

/** The "which platform did you finish it on?" nudge for platformless finishes. */
function PlatformPrompt({
  open,
  entry,
  pending,
  onPick,
}: {
  open: boolean;
  entry: GameEntry;
  pending: boolean;
  onPick: (platformId: number | null) => void;
}) {
  // Closing the prompt still applies the status — the click already said
  // "played"; the question only decides whether a platform rides along.
  const close = () => onPick(null);

  return (
    <Dialog open={open} onClose={close} label="Finished platform">
      <h2 className="text-lg font-semibold text-ink-100">
        Which platform did you play {entry.game.name} on?
      </h2>
      <p className="mt-1 text-sm text-ink-400">
        Platform trophies count the system you actually finished on, not every system the game
        released for.
      </p>
      <div className="mt-4 max-h-64 space-y-1 overflow-y-auto pr-1">
        {entry.game.platforms.map((platform) => (
          <button
            key={platform.id}
            type="button"
            disabled={pending}
            onClick={() => onPick(platform.id)}
            className="w-full rounded-lg px-3 py-2 text-left text-sm text-ink-200 transition-colors hover:bg-white/[0.06] hover:text-ink-100 focus-visible:focus-ring disabled:cursor-not-allowed"
          >
            {platform.name}
          </button>
        ))}
      </div>
      <div className="mt-4 flex justify-end">
        <button
          type="button"
          disabled={pending}
          onClick={close}
          className="rounded-lg px-3 py-1.5 text-sm text-ink-500 transition-colors hover:bg-white/[0.05] hover:text-ink-200 focus-visible:focus-ring disabled:cursor-not-allowed"
        >
          Skip for now
        </button>
      </div>
    </Dialog>
  );
}
