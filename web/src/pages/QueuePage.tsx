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
import { useRef } from "react";
import { useOutletContext } from "react-router-dom";

import { MediaFilter, useMediaFilter } from "@/components/MediaFilter";
import { QueueRow } from "@/components/QueueRow";
import { Gi } from "@/components/ui/Gi";
import { Button, EmptyState, Skeleton } from "@/components/ui/primitives";
import { useEggUnlock } from "@/hooks/useAchievements";
import { useQueue, useReorderQueue } from "@/hooks/useLibrary";
import { entryHours, matchesMedia } from "@/lib/entry";
import { formatHours } from "@/lib/format";
import { isBookEntry } from "@/lib/types";

export function QueuePage() {
  const { openAddDialog } = useOutletContext<{ openAddDialog: () => void }>();
  const [media, setMedia] = useMediaFilter();
  const { data, isLoading } = useQueue();
  const reorder = useReorderQueue();
  const fireEgg = useEggUnlock();

  // The gremlin watch: shuttling the same game back to the top five times
  // in one sitting hatches the Chaos Gremlin egg. Each game counts its own
  // runs — the streak has to be one game, five times.
  const topMoves = useRef(new Map<string, number>());
  const onGremlinMove = (entryId: string) => {
    const count = (topMoves.current.get(entryId) ?? 0) + 1;
    if (count >= 5) {
      topMoves.current.delete(entryId);
      void fireEgg("queue_shuffler");
      return;
    }
    topMoves.current.set(entryId, count);
  };

  // One queue holds both arenas. The filter decides which half of it you are
  // ordering; reordering within a filtered view still moves the entry between
  // the neighbours you can see, which is the move you meant.
  const queued = data?.entries ?? [];
  const entries = queued.filter((entry) => matchesMedia(entry, media));
  const books = media === "book";
  // A library with no books has nothing to filter, so the control stays out of
  // the way — unless the filter is already set to something other than games,
  // which has to remain switchable back.
  const showFilter = media !== "game" || queued.some(isBookEntry);

  const sensors = useSensors(
    // A small distance threshold keeps clicks on the row from starting a drag.
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const totalHours = entries.reduce((sum, entry) => sum + entryHours(entry), 0);

  // Persist a move from oldIndex to newIndex. Both drag and the quick-move
  // buttons funnel through here: reorder locally, then tell the server the new
  // neighbours. The mutation is optimistic and rolls back if the move is
  // rejected.
  const applyMove = (oldIndex: number, newIndex: number) => {
    if (oldIndex === -1 || newIndex === -1 || oldIndex === newIndex) return;
    const reordered = arrayMove(entries, oldIndex, newIndex);
    reorder.mutate({
      entryId: entries[oldIndex].id,
      beforeId: reordered[newIndex - 1]?.id ?? "",
      afterId: reordered[newIndex + 1]?.id ?? "",
      reordered,
    });
  };

  const onDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    applyMove(
      entries.findIndex((entry) => entry.id === active.id),
      entries.findIndex((entry) => entry.id === over.id),
    );
  };

  const moveBy = (index: number, kind: "top" | "up" | "down" | "bottom") => {
    const target =
      kind === "top" ? 0 : kind === "bottom" ? entries.length - 1 : kind === "up" ? index - 1 : index + 1;
    if (kind === "top") onGremlinMove(entries[index].id);
    applyMove(index, target);
  };

  return (
    <div className="mx-auto max-w-4xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <header className="mb-6 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-tight text-ink-100">
            {books ? "Reading Queue" : "Play Queue"}
          </h1>
          <p className="mt-1 text-sm text-ink-400">
            {entries.length > 0 ? (
              <>
                {entries.length} {books ? "book" : media === "all" ? "item" : "game"}
                {entries.length === 1 ? "" : "s"}
                {totalHours > 0 && (
                  <>
                    {" · "}
                    <span className="text-ink-300">{formatHours(totalHours)} deep</span>
                  </>
                )}{" "}
                · drag to reorder
              </>
            ) : books ? (
              "The order you plan to read things in."
            ) : (
              "The order you plan to play things in."
            )}
          </p>
        </div>
        {showFilter && <MediaFilter value={media} onChange={setMedia} />}
      </header>

      {reorder.isError && (
        <p role="alert" className="mb-4 rounded-xl bg-red-500/10 px-3 py-2 text-sm text-red-300">
          Couldn't save that move — the queue has been restored.
        </p>
      )}

      {isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 6 }).map((_, index) => (
            <Skeleton key={index} className="h-[76px]" />
          ))}
        </div>
      ) : entries.length === 0 ? (
        <EmptyState
          icon={<Gi name="list-ordered" className="size-7" />}
          title="Nothing queued up"
          description={
            books
              ? "Books on the to-read shelf appear here. Starting or finishing one takes it out of the queue."
              : "Games in your backlog appear here. Marking one as playing or played takes it out of the queue."
          }
          action={
            <Button variant="primary" onClick={openAddDialog}>
              {books ? "Add a book" : "Add a game"}
            </Button>
          }
        />
      ) : (
        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          onDragEnd={onDragEnd}
          modifiers={[restrictToVerticalAxis, restrictToParentElement]}
        >
          <SortableContext items={entries.map((entry) => entry.id)} strategy={verticalListSortingStrategy}>
            <ol className="space-y-2">
              {entries.map((entry, index) => (
                <QueueRow
                  key={entry.id}
                  entry={entry}
                  position={index + 1}
                  isFirst={index === 0}
                  isLast={index === entries.length - 1}
                  onMove={(kind) => moveBy(index, kind)}
                  showCumulative={!books}
                  cumulativeHours={entries
                    .slice(0, index + 1)
                    .reduce((sum, e) => sum + entryHours(e), 0)}
                />
              ))}
            </ol>
          </SortableContext>
        </DndContext>
      )}
    </div>
  );
}
