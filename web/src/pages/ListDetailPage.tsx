import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  rectSortingStrategy,
  sortableKeyboardCoordinates,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { ArrowLeft, GripVertical, ListTree, Pencil, Sparkles, Trash2 } from "lucide-react";
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";

import { GameCard, GameCardSkeleton } from "@/components/GameCard";
import { SmartListBuilder } from "@/components/SmartListBuilder";
import { Button, EmptyState, Input } from "@/components/ui/primitives";
import { Dialog } from "@/components/ui/Dialog";
import { useDeleteList, useList, useReorderListItem, useUpdateList } from "@/hooks/useLists";
import { formatHours, toHours } from "@/lib/format";
import type { Entry, RuleSet } from "@/lib/types";

/**
 * A GameCard that can be dragged. The handle is a small overlay button rather
 * than the whole card, so clicking the cover still navigates to the game.
 */
function SortableGameCard({ entry }: { entry: Entry }) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } =
    useSortable({ id: entry.id });

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={isDragging ? "relative z-10 opacity-80" : "relative"}
    >
      <GameCard entry={entry} />
      <button
        ref={setActivatorNodeRef}
        {...attributes}
        {...listeners}
        aria-label={`Reorder ${entry.game.name}`}
        className="absolute right-1.5 top-1.5 cursor-grab touch-none rounded-lg bg-ink-950/75 p-1 text-ink-300 opacity-0 backdrop-blur-sm transition-opacity hover:text-white focus-visible:opacity-100 focus-visible:focus-ring active:cursor-grabbing group-hover:opacity-100 [.group:hover_&]:opacity-100"
      >
        <GripVertical className="size-4" />
      </button>
    </div>
  );
}

export function ListDetailPage() {
  const { listId } = useParams<{ listId: string }>();
  const navigate = useNavigate();
  const { data, isLoading } = useList(listId);
  const update = useUpdateList();
  const remove = useDeleteList();

  const [editing, setEditing] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [draftName, setDraftName] = useState("");
  const [draftRules, setDraftRules] = useState<RuleSet | null>(null);

  const list = data?.list;
  const entries = data?.entries ?? [];
  const totalHours = entries.reduce((sum, entry) => sum + toHours(entry.game.time_to_beat_main), 0);

  const reorder = useReorderListItem(listId);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const onDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;

    const oldIndex = entries.findIndex((entry) => entry.id === active.id);
    const newIndex = entries.findIndex((entry) => entry.id === over.id);
    if (oldIndex === -1 || newIndex === -1) return;

    const reordered = arrayMove(entries, oldIndex, newIndex);
    reorder.mutate({
      entryId: String(active.id),
      beforeId: reordered[newIndex - 1]?.id ?? "",
      afterId: reordered[newIndex + 1]?.id ?? "",
      reordered,
    });
  };

  const openEditor = () => {
    if (!list) return;
    setDraftName(list.name);
    setDraftRules(list.rules ?? { match: "all", rules: [] });
    setEditing(true);
  };

  const save = () => {
    if (!listId) return;
    update.mutate(
      {
        id: listId,
        patch: {
          name: draftName.trim(),
          ...(list?.kind === "smart" && draftRules ? { rules: draftRules } : {}),
        },
      },
      { onSuccess: () => setEditing(false) },
    );
  };

  if (isLoading) {
    return (
      <div className="mx-auto max-w-[1600px] px-4 py-8 sm:px-6 lg:px-8">
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-4 lg:grid-cols-6">
          {Array.from({ length: 12 }).map((_, index) => (
            <GameCardSkeleton key={index} />
          ))}
        </div>
      </div>
    );
  }

  if (!list) {
    return (
      <div className="mx-auto max-w-3xl px-6 py-20 text-center">
        <p className="text-ink-300">That list doesn't exist.</p>
        <Link to="/lists" className="mt-4 inline-block text-sm text-brand-400 hover:text-brand-300">
          Back to lists
        </Link>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-[1600px] px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <Link
        to="/lists"
        className="mb-5 inline-flex items-center gap-1.5 rounded-lg text-sm text-ink-400 transition-colors hover:text-ink-100 focus-visible:focus-ring"
      >
        <ArrowLeft className="size-4" />
        Lists
      </Link>

      <header className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="flex items-center gap-2 text-2xl font-semibold tracking-tight text-ink-100">
            {list.kind === "smart" && <Sparkles className="size-5 shrink-0 text-brand-400" />}
            {list.name}
          </h1>
          <p className="mt-1 text-sm text-ink-400">
            {entries.length} game{entries.length === 1 ? "" : "s"}
            {totalHours > 0 && ` · ${formatHours(totalHours)} of playing`}
            {list.description && ` · ${list.description}`}
          </p>
        </div>

        <div className="flex gap-2">
          <Button onClick={openEditor}>
            <Pencil className="size-4" />
            Edit
          </Button>
          <Button
            variant="ghost"
            className="text-red-400 hover:bg-red-500/10 hover:text-red-300"
            onClick={() => setConfirmDelete(true)}
            aria-label="Delete list"
          >
            <Trash2 className="size-4" />
          </Button>
        </div>
      </header>

      {entries.length === 0 ? (
        <EmptyState
          icon={<ListTree className="size-7" />}
          title="Nothing here yet"
          description={
            list.kind === "smart"
              ? "No games match these rules right now. Loosen them, or add more games to your library."
              : "This list is empty. Open a game and add it to this list to get started."
          }
          action={
            list.kind === "smart" ? (
              <Button variant="secondary" onClick={openEditor}>
                Edit rules
              </Button>
            ) : undefined
          }
        />
      ) : list.kind === "manual" ? (
        // Only manual lists are sortable — a smart list's order comes from its
        // own sort rule, so dragging would be undone on the next evaluation.
        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          onDragEnd={onDragEnd}
        >
          <SortableContext items={entries.map((entry) => entry.id)} strategy={rectSortingStrategy}>
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
              {entries.map((entry) => (
                <SortableGameCard key={entry.id} entry={entry} />
              ))}
            </div>
          </SortableContext>
        </DndContext>
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
          {entries.map((entry) => (
            <GameCard key={entry.id} entry={entry} />
          ))}
        </div>
      )}

      {list.kind === "manual" && entries.length > 1 && (
        <p className="mt-6 text-center text-xs text-ink-600">
          Drag the handle on a cover to reorder this list.
        </p>
      )}

      <Dialog open={editing} onClose={() => setEditing(false)} label="Edit list" className="max-w-xl">
        <h2 className="text-lg font-semibold text-ink-100">Edit list</h2>

        <div className="mt-5 space-y-4">
          <label className="block">
            <span className="mb-1.5 block text-xs font-medium text-ink-300">Name</span>
            <Input value={draftName} onChange={(event) => setDraftName(event.target.value)} />
          </label>

          {list.kind === "smart" && draftRules && (
            <div className="rounded-xl border border-white/[0.06] bg-ink-900/50 p-3">
              <SmartListBuilder value={draftRules} onChange={setDraftRules} />
            </div>
          )}

          {update.isError && (
            <p role="alert" className="rounded-xl bg-red-500/10 px-3 py-2 text-sm text-red-300">
              {(update.error as Error).message}
            </p>
          )}

          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setEditing(false)}>
              Cancel
            </Button>
            <Button
              variant="primary"
              loading={update.isPending}
              disabled={!draftName.trim()}
              onClick={save}
            >
              Save changes
            </Button>
          </div>
        </div>
      </Dialog>

      <Dialog open={confirmDelete} onClose={() => setConfirmDelete(false)} label="Confirm deletion">
        <h2 className="text-lg font-semibold text-ink-100">Delete "{list.name}"?</h2>
        <p className="mt-2 text-sm text-ink-400">
          The list goes away, but the games in it stay in your library.
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <Button variant="ghost" onClick={() => setConfirmDelete(false)}>
            Cancel
          </Button>
          <Button
            variant="danger"
            loading={remove.isPending}
            onClick={() =>
              remove.mutate(list.id, { onSuccess: () => navigate("/lists") })
            }
          >
            Delete list
          </Button>
        </div>
      </Dialog>
    </div>
  );
}
