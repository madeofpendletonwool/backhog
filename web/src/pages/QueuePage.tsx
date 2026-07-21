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
import { ListOrdered } from "lucide-react";
import { useOutletContext } from "react-router-dom";

import { QueueRow } from "@/components/QueueRow";
import { Button, EmptyState, Skeleton } from "@/components/ui/primitives";
import { useQueue, useReorderQueue } from "@/hooks/useLibrary";
import { formatHours, toHours } from "@/lib/format";

export function QueuePage() {
  const { openAddDialog } = useOutletContext<{ openAddDialog: () => void }>();
  const { data, isLoading } = useQueue();
  const reorder = useReorderQueue();

  const entries = data?.entries ?? [];

  const sensors = useSensors(
    // A small distance threshold keeps clicks on the row from starting a drag.
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const totalHours = entries.reduce((sum, entry) => sum + toHours(entry.game.time_to_beat_main), 0);

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
    applyMove(index, target);
  };

  return (
    <div className="mx-auto max-w-4xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold tracking-tight text-ink-100">Play Queue</h1>
        <p className="mt-1 text-sm text-ink-400">
          {entries.length > 0 ? (
            <>
              {entries.length} game{entries.length === 1 ? "" : "s"} ·{" "}
              <span className="text-ink-300">{formatHours(totalHours)} deep</span> · drag to reorder
            </>
          ) : (
            "The order you plan to play things in."
          )}
        </p>
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
          icon={<ListOrdered className="size-7" />}
          title="Nothing queued up"
          description="Games in your backlog appear here. Marking one as playing or played takes it out of the queue."
          action={
            <Button variant="primary" onClick={openAddDialog}>
              Add a game
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
                  cumulativeHours={entries
                    .slice(0, index + 1)
                    .reduce((sum, e) => sum + toHours(e.game.time_to_beat_main), 0)}
                />
              ))}
            </ol>
          </SortableContext>
        </DndContext>
      )}
    </div>
  );
}
