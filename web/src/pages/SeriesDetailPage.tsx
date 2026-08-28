import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import { restrictToParentElement, restrictToVerticalAxis } from "@dnd-kit/modifiers";
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { cn } from "@/lib/cn";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";

import { GameCover } from "@/components/GameCover";
import { StatusBadge } from "@/components/StatusBadge";
import { Gi } from "@/components/ui/Gi";
import { Select, Skeleton } from "@/components/ui/primitives";
import { useReorderSeries, useSeriesDetail, useSetSeriesPlayOrder } from "@/hooks/useSeries";
import { accentStyle, formatDuration, formatHours, releaseYear } from "@/lib/format";
import { PLAY_ORDERS, type SeriesMember, type Status } from "@/lib/types";

/** The rating floor for the "just the good ones" view. */
const GOOD_ONES_FLOOR = 75;

/**
 * One series as a journey: the full member list in the user's chosen play
 * order, with DLC nested by their parent game.
 */
export function SeriesDetailPage() {
  const { seriesId } = useParams<{ seriesId: string }>();
  const { data: detail, isLoading, isError } = useSeriesDetail(seriesId);
  const setOrder = useSetSeriesPlayOrder();

  const isCustom = detail?.play_order === "custom";
  const isGoodOnes = detail?.play_order === "good_ones";
  const [showAll, setShowAll] = useState(false);

  const members = detail?.members ?? [];
  const hidden = isGoodOnes && !showAll
    ? members.filter((m) => m.game.igdb_rating == null || m.game.igdb_rating < GOOD_ONES_FLOOR)
    : [];
  const visible = isGoodOnes && !showAll
    ? members.filter((m) => m.game.igdb_rating != null && m.game.igdb_rating >= GOOD_ONES_FLOOR)
    : members;

  if (isLoading) {
    return (
      <div className="mx-auto max-w-4xl space-y-3 px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
        <Skeleton className="h-28" />
        {Array.from({ length: 5 }).map((_, i) => (
          <Skeleton key={i} className="h-16" />
        ))}
      </div>
    );
  }

  if (!detail || isError) {
    return (
      <div className="mx-auto max-w-3xl px-6 py-20 text-center">
        <p className="text-ink-300">That series doesn't exist.</p>
        <Link to="/series" className="mt-4 inline-block text-sm text-brand-400 hover:text-brand-300">
          Back to series
        </Link>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-4xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <Link
        to="/series"
        className="mb-5 inline-flex items-center gap-1.5 rounded-lg text-sm text-ink-400 transition-colors hover:text-ink-100 focus-visible:focus-ring"
      >
        <Gi name="arrow-left" className="size-4" />
        Series
      </Link>

      <header className="mb-5 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h1 className="flex items-center gap-2.5 text-2xl font-semibold tracking-tight text-ink-100">
            <Gi name="layers" className="size-6 text-brand-400" />
            {detail.name}
          </h1>
          <p className="mt-1.5 text-sm text-ink-400">
            {detail.played_count}/{detail.owned_count} played · {Math.round(detail.completion)}%
            complete
            {detail.remaining_hours > 0 && ` · ${formatHours(detail.remaining_hours)} left`}
            {detail.dlc_hours > 0 && ` · ${formatHours(detail.dlc_hours)} of it DLC`}
          </p>
        </div>

        <div className="w-full sm:w-64">
          <Select
            value={detail.play_order}
            onChange={(event) =>
              setOrder.mutate({ id: detail.id, playOrder: event.target.value as typeof detail.play_order })
            }
            aria-label="Play order"
          >
            {PLAY_ORDERS.map((order) => (
              <option key={order.value} value={order.value}>
                {order.label}
              </option>
            ))}
          </Select>
        </div>
      </header>

      {isGoodOnes && (
        <p className="mb-3 rounded-xl bg-ink-850 px-3 py-2 text-xs leading-relaxed text-ink-400">
          Showing the good ones — members rated {GOOD_ONES_FLOOR}+ on IGDB.
          {hidden.length > 0 && (
            <>
              {" "}
              <button
                onClick={() => setShowAll(true)}
                className="font-medium text-brand-400 hover:text-brand-300 focus-visible:focus-ring"
              >
                Show all {members.length}
              </button>
            </>
          )}
        </p>
      )}

      {isCustom ? (
        <CustomJourney seriesId={detail.id} members={visible} />
      ) : (
        <ol className="space-y-2">
          {visible.map((member, index) => (
            <MemberRow key={member.game.id} member={member} index={index} />
          ))}
        </ol>
      )}

      {showAll && hidden.length > 0 && (
        <>
          <p className="mt-4 px-1 text-xs text-ink-500">Below the {GOOD_ONES_FLOOR} floor:</p>
          <ol className="mt-2 space-y-2">
            {hidden.map((member, index) => (
              <MemberRow key={member.game.id} member={member} index={index} dimmed />
            ))}
          </ol>
        </>
      )}
    </div>
  );
}

/** The custom journey: drag to reorder, persisted with fractional positions. */
function CustomJourney({ seriesId, members }: { seriesId: string; members: SeriesMember[] }) {
  const reorder = useReorderSeries(seriesId);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const applyMove = (oldIndex: number, newIndex: number) => {
    if (oldIndex === -1 || newIndex === -1 || oldIndex === newIndex) return;
    const reordered = arrayMove(members, oldIndex, newIndex);
    reorder.mutate({
      gameId: members[oldIndex].game.id,
      beforeId: reordered[newIndex - 1]?.game.id ?? 0,
      afterId: reordered[newIndex + 1]?.game.id ?? 0,
      reordered: reordered.map((m) => m.game.id),
    });
  };

  const onDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    applyMove(
      members.findIndex((m) => m.game.id === active.id),
      members.findIndex((m) => m.game.id === over.id),
    );
  };

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCenter}
      onDragEnd={onDragEnd}
      modifiers={[restrictToVerticalAxis, restrictToParentElement]}
    >
      <SortableContext
        items={members.map((m) => m.game.id)}
        strategy={verticalListSortingStrategy}
      >
        <ol className="space-y-2">
          {members.map((member, index) => (
            <SortableMemberRow key={member.game.id} member={member} index={index} />
          ))}
        </ol>
      </SortableContext>
    </DndContext>
  );
}

function SortableMemberRow({ member, index }: { member: SeriesMember; index: number }) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } =
    useSortable({ id: member.game.id });

  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn("relative", isDragging && "z-10 opacity-90")}
    >
      <div className="absolute left-1 top-1/2 z-10 -translate-y-1/2">
        <button
          ref={setActivatorNodeRef}
          {...attributes}
          {...listeners}
          aria-label={`Reorder ${member.game.name}`}
          className="cursor-grab touch-none rounded-lg p-1 text-ink-600 transition-colors hover:text-ink-300 focus-visible:focus-ring active:cursor-grabbing"
        >
          <Gi name="grab" className="size-5" />
        </button>
      </div>
      <MemberRow member={member} index={index} />
    </li>
  );
}

/** One member of the journey: cover, name, kind, hours, and library status. */
function MemberRow({ member, index, dimmed = false }: { member: SeriesMember; index: number; dimmed?: boolean }) {
  const { game } = member;
  const owned = member.status !== "unowned";

  const content = (
    <>
      <span className="w-6 shrink-0 text-center text-sm font-semibold tabular-nums text-ink-500">
        {index + 1}
      </span>

      <GameCover game={game} className="w-11 shrink-0 rounded-lg" />

      <div className="min-w-0 flex-1">
        <p className="flex flex-wrap items-center gap-2">
          <span className="truncate font-medium text-ink-100">{game.name}</span>
          {member.kind !== "game" && (
            <span className="rounded-full bg-white/[0.07] px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide text-ink-400">
              {member.kind}
            </span>
          )}
        </p>
        <div className="mt-0.5 flex items-center gap-2.5 text-xs text-ink-500">
          {releaseYear(game) && <span>{releaseYear(game)}</span>}
          {game.time_to_beat_main != null && (
            <span className="inline-flex items-center gap-1">
              <Gi name="clock" className="size-3" />
              {formatDuration(game.time_to_beat_main)}
            </span>
          )}
          {game.igdb_rating != null && (
            <span className="inline-flex items-center gap-1">
              <Gi name="star" className="size-3" />
              {Math.round(game.igdb_rating)}
            </span>
          )}
        </div>
      </div>

      <div className="shrink-0">
        {owned ? (
          <StatusBadge status={member.status as Status} showLabel={false} />
        ) : (
          <span className="text-xs text-ink-600">Not owned</span>
        )}
      </div>
    </>
  );

  const className = cn(
    "panel flex items-center gap-3 p-3 pl-10",
    dimmed && "opacity-55",
    !owned && "opacity-80",
  );

  return (
    <li style={accentStyle(game)} className="list-none">
      {owned && member.entry_id ? (
        <Link
          to={`/game/${member.entry_id}`}
          className={cn(className, "rounded-2xl transition-colors hover:border-white/[0.12] focus-visible:focus-ring")}
        >
          {content}
        </Link>
      ) : (
        <div className={className}>{content}</div>
      )}
    </li>
  );
}
