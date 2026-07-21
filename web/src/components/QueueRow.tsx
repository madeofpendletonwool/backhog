import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { cn } from "@/lib/cn";
import {
  ChevronDown,
  ChevronUp,
  ChevronsDown,
  ChevronsUp,
  Clock,
  GripVertical,
  PlayCircle,
  Star,
} from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";

import { GameCover } from "./GameCover";
import { Button } from "./ui/primitives";
import { useUpdateEntry } from "@/hooks/useLibrary";
import { accentStyle, formatDuration, formatHours, releaseYear } from "@/lib/format";
import type { Entry } from "@/lib/types";

/**
 * One draggable row of the play queue. The drag handle is a dedicated control
 * rather than the whole row, so the title stays a working link and keyboard
 * users get an explicit, focusable target.
 */
export type QueueMove = "top" | "up" | "down" | "bottom";

export function QueueRow({
  entry,
  position,
  cumulativeHours,
  isFirst,
  isLast,
  onMove,
}: {
  entry: Entry;
  position: number;
  cumulativeHours: number;
  isFirst: boolean;
  isLast: boolean;
  onMove: (kind: QueueMove) => void;
}) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } =
    useSortable({ id: entry.id });

  const update = useUpdateEntry();
  const { game } = entry;

  return (
    <li
      ref={setNodeRef}
      style={{
        transform: CSS.Transform.toString(transform),
        transition,
        ...accentStyle(game),
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
        aria-label={`Reorder ${game.name}`}
        className="shrink-0 cursor-grab touch-none rounded-lg p-1 text-ink-600 transition-colors hover:text-ink-300 focus-visible:focus-ring active:cursor-grabbing"
      >
        <GripVertical className="size-5" />
      </button>

      <span className="w-6 shrink-0 text-center text-sm font-semibold tabular-nums text-ink-500">
        {position}
      </span>

      <Link
        to={`/game/${entry.id}`}
        className="flex min-w-0 flex-1 items-center gap-3 rounded-lg focus-visible:focus-ring"
      >
        <GameCover game={game} className="w-11 shrink-0 rounded-lg" />
        <div className="min-w-0">
          <p className="truncate font-medium text-ink-100">{game.name}</p>
          <div className="mt-0.5 flex items-center gap-2.5 text-xs text-ink-500">
            {releaseYear(game) && <span>{releaseYear(game)}</span>}
            <span className="inline-flex items-center gap-1">
              <Clock className="size-3" />
              {formatDuration(game.time_to_beat_main)}
            </span>
            {game.igdb_rating != null && (
              <span className="inline-flex items-center gap-1">
                <Star className="size-3" />
                {Math.round(game.igdb_rating)}
              </span>
            )}
          </div>
        </div>
      </Link>

      {/* Running total: "if I play everything down to here, that's N hours." */}
      <span
        className="hidden shrink-0 text-xs tabular-nums text-ink-600 sm:block"
        title="Total hours through this game"
      >
        {formatHours(cumulativeHours)}
      </span>

      {/* Quick moves — dragging a 200-item queue by hand is unbearable. */}
      <div className="flex shrink-0 items-center gap-0.5">
        <MoveButton label={`Move ${game.name} to top`} disabled={isFirst} onClick={() => onMove("top")}>
          <ChevronsUp className="size-4" />
        </MoveButton>
        <MoveButton label={`Move ${game.name} up`} disabled={isFirst} onClick={() => onMove("up")}>
          <ChevronUp className="size-4" />
        </MoveButton>
        <MoveButton label={`Move ${game.name} down`} disabled={isLast} onClick={() => onMove("down")}>
          <ChevronDown className="size-4" />
        </MoveButton>
        <MoveButton label={`Move ${game.name} to bottom`} disabled={isLast} onClick={() => onMove("bottom")}>
          <ChevronsDown className="size-4" />
        </MoveButton>
      </div>

      <Button
        size="sm"
        variant="secondary"
        loading={update.isPending}
        onClick={() => update.mutate({ id: entry.id, patch: { status: "playing" } })}
        className="shrink-0 opacity-0 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100"
      >
        <PlayCircle className="size-3.5" />
        Start
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
