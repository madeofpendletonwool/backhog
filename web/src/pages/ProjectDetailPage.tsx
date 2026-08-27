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
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { cn } from "@/lib/cn";
import {
  ArrowLeft,
  Check,
  GripVertical,
  ListChecks,
  Pencil,
  Sparkles,
  Target,
  Trash2,
  X,
} from "lucide-react";
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";

import { GameCard } from "@/components/GameCard";
import { GameCover } from "@/components/GameCover";
import { ProgressBar } from "@/components/ProgressBar";
import { SmartListBuilder } from "@/components/SmartListBuilder";
import { StatusBadge } from "@/components/StatusBadge";
import { Button, EmptyState, Input, Panel, Skeleton } from "@/components/ui/primitives";
import { Dialog } from "@/components/ui/Dialog";
import {
  useDeleteProject,
  useProject,
  useProjectItemMutations,
  useReorderProjectItem,
  useUpdateProject,
} from "@/hooks/useProjects";
import { formatDate, formatDuration, formatHours, releaseYear } from "@/lib/format";
import {
  PROJECT_KIND_LABELS,
  type Project,
  type ProjectItem,
  type ProjectKind,
  type RuleSet,
} from "@/lib/types";

const KIND_ICONS: Record<ProjectKind, React.ReactNode> = {
  checklist: <ListChecks className="size-5 shrink-0" />,
  count_goal: <Target className="size-5 shrink-0" />,
  rule_goal: <Sparkles className="size-5 shrink-0 text-brand-400" />,
};

export function ProjectDetailPage() {
  const { projectId } = useParams<{ projectId: string }>();
  const navigate = useNavigate();
  const { data, isLoading } = useProject(projectId);
  const update = useUpdateProject();
  const remove = useDeleteProject();

  const [editing, setEditing] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const project = data?.project;
  const items = data?.items ?? [];
  const { progress } = project ?? { progress: null };
  const complete = Boolean(project?.completed_at);

  const reorder = useReorderProjectItem(projectId);
  const { remove: removeItem, setDone } = useProjectItemMutations(projectId);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const onDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;

    const oldIndex = items.findIndex((item) => item.entry.id === active.id);
    const newIndex = items.findIndex((item) => item.entry.id === over.id);
    if (oldIndex === -1 || newIndex === -1) return;

    const reordered = arrayMove(items, oldIndex, newIndex);
    reorder.mutate({
      entryId: String(active.id),
      beforeId: reordered[newIndex - 1]?.entry.id ?? "",
      afterId: reordered[newIndex + 1]?.entry.id ?? "",
      reordered,
    });
  };

  const toggleComplete = () => {
    if (!projectId) return;
    update.mutate({ id: projectId, patch: { completed: !complete } });
  };

  if (isLoading) {
    return (
      <div className="mx-auto max-w-4xl px-4 py-8 sm:px-6 lg:px-8">
        <div className="space-y-3">
          {Array.from({ length: 6 }).map((_, index) => (
            <SkeletonRow key={index} />
          ))}
        </div>
      </div>
    );
  }

  if (!project || !progress) {
    return (
      <div className="mx-auto max-w-3xl px-6 py-20 text-center">
        <p className="text-ink-300">That project doesn't exist.</p>
        <Link to="/projects" className="mt-4 inline-block text-sm text-brand-400 hover:text-brand-300">
          Back to projects
        </Link>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-4xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <Link
        to="/projects"
        className="mb-5 inline-flex items-center gap-1.5 rounded-lg text-sm text-ink-400 transition-colors hover:text-ink-100 focus-visible:focus-ring"
      >
        <ArrowLeft className="size-4" />
        Projects
      </Link>

      <header className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="flex items-center gap-2 text-2xl font-semibold tracking-tight text-ink-100">
            {KIND_ICONS[project.kind]}
            {project.name}
          </h1>
          <p className="mt-1 text-sm text-ink-400">
            {PROJECT_KIND_LABELS[project.kind]}
            {project.description && ` · ${project.description}`}
          </p>
        </div>

        <div className="flex flex-wrap gap-2">
          <Button onClick={toggleComplete} loading={update.isPending}>
            <Check className="size-4" />
            {complete ? "Reopen" : "Mark done"}
          </Button>
          <Button onClick={() => setEditing(true)}>
            <Pencil className="size-4" />
            Edit
          </Button>
          <Button
            variant="ghost"
            className="text-red-400 hover:bg-red-500/10 hover:text-red-300"
            onClick={() => setConfirmDelete(true)}
            aria-label="Delete project"
          >
            <Trash2 className="size-4" />
          </Button>
        </div>
      </header>

      <ProgressPanel project={project} />

      {project.kind === "checklist" ? (
        items.length === 0 ? (
          <EmptyState
            icon={<ListChecks className="size-7" />}
            title="No games in this project yet"
            description="Open a game's page and check it into this project to start building the list."
            action={
              <Button variant="secondary" onClick={() => navigate("/library")}>
                Browse library
              </Button>
            }
          />
        ) : (
          <>
            <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
              <SortableContext
                items={items.map((item) => item.entry.id)}
                strategy={verticalListSortingStrategy}
              >
                <ul className="space-y-2">
                  {items.map((item) => (
                    <ChecklistRow
                      key={item.entry.id}
                      item={item}
                      onSetDone={(done) => setDone.mutate({ entryId: item.entry.id, done })}
                      onRemove={() => removeItem.mutate(item.entry.id)}
                    />
                  ))}
                </ul>
              </SortableContext>
            </DndContext>
            {items.length > 1 && (
              <p className="mt-4 text-center text-xs text-ink-600">
                Drag the handle to reorder · click the circle to override a game's done state
              </p>
            )}
          </>
        )
      ) : project.kind === "rule_goal" ? (
        items.length === 0 ? (
          <EmptyState
            icon={<Sparkles className="size-7" />}
            title="No games match these rules right now"
            description="Loosen them, or add more games to your library."
            action={
              <Button variant="secondary" onClick={() => setEditing(true)}>
                Edit rules
              </Button>
            }
          />
        ) : (
          <>
            <p className="mb-3 text-xs text-ink-500">
              The current match pool — finishing any of these counts toward the goal.
            </p>
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
              {items.map((item) => (
                <GameCard key={item.entry.id} entry={item.entry} />
              ))}
            </div>
          </>
        )
      ) : (
        <Panel className="p-5">
          <p className="text-sm leading-relaxed text-ink-300">
            Every game you finish counts toward this goal — no membership to manage.
            Progress updates the moment a game's status becomes{" "}
            <span className="text-ink-100">played</span>.
          </p>
        </Panel>
      )}

      {editing && <EditProjectDialog onClose={() => setEditing(false)} project={project} />}

      <Dialog open={confirmDelete} onClose={() => setConfirmDelete(false)} label="Confirm deletion">
        <h2 className="text-lg font-semibold text-ink-100">Delete "{project.name}"?</h2>
        <p className="mt-2 text-sm text-ink-400">
          The project goes away, but the games in it stay in your library.
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <Button variant="ghost" onClick={() => setConfirmDelete(false)}>
            Cancel
          </Button>
          <Button
            variant="danger"
            loading={remove.isPending}
            onClick={() => remove.mutate(project.id, { onSuccess: () => navigate("/projects") })}
          >
            Delete project
          </Button>
        </div>
      </Dialog>
    </div>
  );
}

function ProgressPanel({ project }: { project: Project }) {
  const { progress } = project;
  const complete = Boolean(project.completed_at);

  return (
    <Panel className="mb-8 p-5">
      <div className="mb-3 flex items-end justify-between gap-4">
        <p className="text-sm text-ink-300">
          <span className="text-lg font-semibold tabular-nums text-ink-100">
            {progress.completed_count}
          </span>
          <span className="text-ink-500"> of </span>
          <span className="text-lg font-semibold tabular-nums text-ink-100">
            {progress.target_count}
          </span>
          <span className="text-ink-500"> finished</span>
          {progress.est_hours_total > 0 && (
            <span className="text-ink-500">
              {" "}
              · {formatHours(progress.est_hours_done)} of {formatHours(progress.est_hours_total)}{" "}
              estimated
            </span>
          )}
        </p>
        <p className="text-2xl font-semibold tabular-nums tracking-tight text-ink-100">
          {Math.round(progress.percent)}%
        </p>
      </div>
      <ProgressBar percent={progress.percent} complete={complete} />
      <p className="mt-2.5 text-xs text-ink-500">
        {complete
          ? `Target met or closed · ${formatDate(project.completed_at ?? null)}`
          : progress.est_hours_remaining > 0
            ? `${formatHours(progress.est_hours_remaining)} of estimated playtime to go`
            : "No estimated hours left in this set"}
      </p>
    </Panel>
  );
}

/**
 * One checklist member: a sortable row whose completion is a first-class
 * control. The circle reflects effective done (manual override, or the entry's
 * status); clicking sets an override, and clicking a manual item clears it
 * back to status-derived.
 */
function ChecklistRow({
  item,
  onSetDone,
  onRemove,
}: {
  item: ProjectItem;
  onSetDone: (done: boolean | null) => void;
  onRemove: () => void;
}) {
  const { entry, done } = item;
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } =
    useSortable({ id: entry.id });
  const effectiveDone = done ?? entry.status === "played";

  const onClick = () => {
    if (done !== null) {
      onSetDone(null);
    } else {
      onSetDone(!effectiveDone);
    }
  };

  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn(
        "panel group flex items-center gap-3 p-3",
        isDragging && "z-10 opacity-90 shadow-2xl ring-1 ring-brand-500/40",
        effectiveDone && "opacity-70",
      )}
    >
      <button
        ref={setActivatorNodeRef}
        {...attributes}
        {...listeners}
        aria-label={`Reorder ${entry.game.name}`}
        className="shrink-0 cursor-grab touch-none rounded-lg p-1 text-ink-600 transition-colors hover:text-ink-300 focus-visible:focus-ring active:cursor-grabbing"
      >
        <GripVertical className="size-5" />
      </button>

      <button
        type="button"
        onClick={onClick}
        aria-pressed={effectiveDone}
        title={
          done === null
            ? "Done via status — click to override"
            : done
              ? "Marked done by hand — click to go back to status-derived"
              : "Excluded by hand — click to go back to status-derived"
        }
        className={cn(
          "flex size-6 shrink-0 items-center justify-center rounded-full border transition-colors focus-visible:focus-ring",
          effectiveDone
            ? done === null
              ? "border-emerald-400/60 bg-emerald-500/20 text-emerald-300"
              : "border-emerald-400 bg-emerald-500 text-white"
            : "border-white/20 text-transparent hover:border-white/40",
          done === false && "border-white/10",
        )}
      >
        <Check className="size-3.5" />
      </button>

      <Link
        to={`/game/${entry.id}`}
        className="flex min-w-0 flex-1 items-center gap-3 rounded-lg focus-visible:focus-ring"
      >
        <GameCover game={entry.game} className="w-11 shrink-0 rounded-lg" />
        <div className="min-w-0">
          <p
            className={cn(
              "truncate font-medium text-ink-100",
              effectiveDone && "line-through decoration-ink-500",
            )}
          >
            {entry.game.name}
          </p>
          <p className="mt-0.5 flex items-center gap-2.5 text-xs text-ink-500">
            {releaseYear(entry.game) && <span>{releaseYear(entry.game)}</span>}
            <span>{formatDuration(entry.game.time_to_beat_main)}</span>
          </p>
        </div>
      </Link>

      <StatusBadge status={entry.status} showLabel />

      <button
        type="button"
        onClick={onRemove}
        aria-label={`Remove ${entry.game.name} from this project`}
        className="shrink-0 rounded-lg p-1.5 text-ink-600 opacity-0 transition-all hover:bg-white/[0.06] hover:text-red-400 focus-visible:opacity-100 focus-visible:focus-ring group-hover:opacity-100 [@media(hover:none)]:opacity-100"
      >
        <X className="size-4" />
      </button>
    </li>
  );
}

/** Fresh-mounted per open, so drafts always start from the stored project. */
function EditProjectDialog({
  onClose,
  project,
}: {
  onClose: () => void;
  project: Project;
}) {
  const update = useUpdateProject();
  const [name, setName] = useState(project.name);
  const [description, setDescription] = useState(project.description);
  const [target, setTarget] = useState(project.target_count != null ? String(project.target_count) : "");
  const [rules, setRules] = useState<RuleSet>(
    project.rules ?? { match: "all", rules: [], sort: { field: "added", dir: "desc" } },
  );

  const targetNumber = Number(target);
  const targetValid =
    project.kind === "count_goal"
      ? target.trim() !== "" && targetNumber > 0
      : target.trim() === "" || targetNumber > 0;

  const save = () => {
    update.mutate(
      {
        id: project.id,
        patch: {
          name: name.trim(),
          description: description.trim(),
          ...(project.kind !== "checklist"
            ? {
                target_count: target.trim() === "" ? null : Number(target),
              }
            : {}),
          ...(project.kind === "rule_goal" ? { rules } : {}),
        },
      },
      { onSuccess: onClose },
    );
  };

  return (
    <Dialog open onClose={onClose} label="Edit project" className="max-w-xl">
      <h2 className="text-lg font-semibold text-ink-100">Edit project</h2>

      <div className="mt-5 space-y-4">
        <label className="block">
          <span className="mb-1.5 block text-xs font-medium text-ink-300">Name</span>
          <Input value={name} onChange={(event) => setName(event.target.value)} />
        </label>

        <label className="block">
          <span className="mb-1.5 block text-xs font-medium text-ink-300">Description</span>
          <Input
            value={description}
            onChange={(event) => setDescription(event.target.value)}
          />
        </label>

        {project.kind !== "checklist" && (
          <label className="block">
            <span className="mb-1.5 block text-xs font-medium text-ink-300">
              Target
              {project.kind === "rule_goal" && (
                <span className="text-ink-600"> (empty = every matching game)</span>
              )}
            </span>
            <Input
              type="number"
              min={1}
              step={1}
              value={target}
              onChange={(event) => setTarget(event.target.value)}
              className="w-28"
            />
          </label>
        )}

        {project.kind === "rule_goal" && (
          <div className="rounded-xl border border-white/[0.06] bg-ink-900/50 p-3">
            <SmartListBuilder value={rules} onChange={setRules} />
          </div>
        )}

        {update.isError && (
          <p role="alert" className="rounded-xl bg-red-500/10 px-3 py-2 text-sm text-red-300">
            {(update.error as Error).message}
          </p>
        )}

        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            loading={update.isPending}
            disabled={!name.trim() || !targetValid}
            onClick={save}
          >
            Save changes
          </Button>
        </div>
      </div>
    </Dialog>
  );
}

function SkeletonRow() {
  return (
    <div className="panel flex items-center gap-3 p-3">
      <Skeleton className="size-5 rounded-md" />
      <div className="skeleton size-6 rounded-full" />
      <div className="skeleton h-11 w-11 rounded-lg" />
      <div className="flex-1 space-y-2">
        <div className="skeleton h-4 w-1/3 rounded-md" />
        <div className="skeleton h-3 w-1/5 rounded-md" />
      </div>
    </div>
  );
}
