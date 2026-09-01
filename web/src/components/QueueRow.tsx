import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { cn } from "@/lib/cn";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";

import { EntryCover } from "./EntryCover";
import { Button, Gi } from "./ui/primitives";
import { useUpdateEntry } from "@/hooks/useLibrary";
import { entryAccent, entryHref, entrySubtitle, entryTitle } from "@/lib/entry";
import { accentStyle, formatDuration, formatHours } from "@/lib/format";
import { isGameEntry, type Entry } from "@/lib/types";

/**
 * One draggable row of the queue. The drag handle is a dedicated control
 * rather than the whole row, so the title stays a working link and keyboard
 * users get an explicit, focusable target.
 *
 * The queue is one ordered list across both arenas, so the row takes any
 * entry: a game contributes its time-to-beat to the running total, a book
 * contributes its author to the line under the title.
 */
export type QueueMove = "top" | "up" | "down" | "bottom";

export function QueueRow({
  entry,
  position,
  cumulativeHours,
  showCumulative = true,
  isFirst,
  isLast,
  onMove,
}: {
  entry: Entry;
  position: number;
  cumulativeHours: number;
  /** Off for a books-only queue, where every running total would read "0h". */
  showCumulative?: boolean;
  isFirst: boolean;
  isLast: boolean;
  onMove: (kind: QueueMove) => void;
}) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } =
    useSortable({ id: entry.id });

  const update = useUpdateEntry();
  const title = entryTitle(entry);
  const subtitle = entrySubtitle(entry);

  return (
    <li
      ref={setNodeRef}
      style={{
        transform: CSS.Transform.toString(transform),
        transition,
        ...accentStyle(entryAccent(entry)),
      }}
      className={cn(
        "panel group relative flex items-center gap-3 p-3",
        isDragging && "z-10 opacity-90 shadow-2xl ring-1 ring-brand-500/40",
      )}
    >
      <button
        ref={setActivatorNodeRef}
        {...attributes}
        {...listeners}
        aria-label={`Reorder ${title}`}
        className="shrink-0 cursor-grab touch-none p-1 text-ink-500 transition-colors hover:text-ink-300 focus-visible:focus-ring active:cursor-grabbing"
      >
        <Gi name="grab" className="size-5" />
      </button>

      <span className="w-6 shrink-0 text-center text-sm font-semibold tabular-nums text-ink-500">
        {position}
      </span>

      <Link
        to={entryHref(entry)}
        className="flex min-w-0 flex-1 items-center gap-3 rounded-lg focus-visible:focus-ring"
      >
        <EntryCover entry={entry} className="w-11 shrink-0 rounded-lg" />
        <div className="min-w-0">
          <p className="truncate font-medium text-ink-100">{title}</p>
          <div className="mt-0.5 flex items-center gap-2.5 text-xs text-ink-500">
            {subtitle && <span className="truncate">{subtitle}</span>}
            {isGameEntry(entry) && (
              <>
                <span className="inline-flex items-center gap-1">
                  <Gi name="clock" className="size-3" />
                  {formatDuration(entry.game.time_to_beat_main)}
                </span>
                {entry.game.igdb_rating != null && (
                  <span className="inline-flex items-center gap-1">
                    <Gi name="star" className="size-3" />
                    {Math.round(entry.game.igdb_rating)}
                  </span>
                )}
              </>
            )}
          </div>
        </div>
      </Link>

      {/* Running total: "if I play everything down to here, that's N hours."
          Only games carry a time-to-beat, so a books-only queue hides it. */}
      {showCumulative && (
        <span
          className="hidden shrink-0 text-xs tabular-nums text-ink-600 sm:block"
          title="Total hours through this game"
        >
          {formatHours(cumulativeHours)}
        </span>
      )}

      {/* Quick moves — dragging a 200-item queue by hand is unbearable. */}
      <div className="flex shrink-0 items-center gap-0.5">
        <MoveButton label={`Move ${title} to top`} disabled={isFirst} onClick={() => onMove("top")}>
          <Gi name="chevrons-up" className="size-4" />
        </MoveButton>
        <MoveButton label={`Move ${title} up`} disabled={isFirst} onClick={() => onMove("up")}>
          <Gi name="chevron-up" className="size-4" />
        </MoveButton>
        <MoveButton label={`Move ${title} down`} disabled={isLast} onClick={() => onMove("down")}>
          <Gi name="chevron-down" className="size-4" />
        </MoveButton>
        <MoveButton label={`Move ${title} to bottom`} disabled={isLast} onClick={() => onMove("bottom")}>
          <Gi name="chevrons-down" className="size-4" />
        </MoveButton>
      </div>

      <Button
        size="sm"
        variant="secondary"
        loading={update.isPending}
        aria-label={`Mark ${title} as ${isGameEntry(entry) ? "playing" : "reading"}`}
        onClick={() => update.mutate({ id: entry.id, patch: { status: "playing" } })}
        className="shrink-0 opacity-0 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100 [@media(hover:none)]:opacity-100"
      >
        <Gi name="play" className="size-3.5" />
        <span className="hidden sm:inline">Start</span>
      </Button>
    </li>
  );
}

/** A compact icon button for the quick-move controls. */
function MoveButton({
  label,
  disabled,
  onClick,
  children,
}: {
  label: string;
  disabled: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={onClick}
      className="rounded-md p-1 text-ink-500 transition-colors hover:bg-white/[0.06] hover:text-ink-200 focus-visible:focus-ring disabled:pointer-events-none disabled:opacity-25"
    >
      {children}
    </button>
  );
}
