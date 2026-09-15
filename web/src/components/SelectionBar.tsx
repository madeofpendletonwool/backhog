import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";

import { STATUS_ICONS } from "@/components/StatusBadge";
import { Dialog } from "@/components/ui/Dialog";
import { Gi } from "@/components/ui/Gi";
import { Button, Input } from "@/components/ui/primitives";
import { dispatchUnlocks } from "@/hooks/useAchievements";
import { invalidateLibrary } from "@/hooks/useLibrary";
import { useLists } from "@/hooks/useLists";
import { useProjects } from "@/hooks/useProjects";
import type { Selection } from "@/hooks/useSelection";
import { api } from "@/lib/api";
import { cn } from "@/lib/cn";
import { STATUSES, statusLabel, type Entry, type MediaType, type Status } from "@/lib/types";

/**
 * The bar that appears while a shelf is in selection mode: how many are
 * picked, and the things you can do to all of them at once. Both arenas use
 * it — lists and projects are keyed on entry ids and know nothing about
 * media, and a status is a status.
 *
 * It sits above the audiobook player when one is open, using the height the
 * player publishes for exactly this reason.
 */
export function SelectionBar({
  selection,
  entries,
  media,
  shown,
}: {
  selection: Selection;
  /** The picked entries, in the order they were picked. */
  entries: Entry[];
  media: MediaType;
  /** How many are on screen, for "select all". */
  shown: number;
}) {
  const [dialog, setDialog] = useState<"list" | "project" | "status" | "remove" | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const noticeTimer = useRef(0);
  useEffect(() => () => window.clearTimeout(noticeTimer.current), []);

  const say = (message: string) => {
    setNotice(message);
    window.clearTimeout(noticeTimer.current);
    noticeTimer.current = window.setTimeout(() => setNotice(null), 5000);
  };

  if (!selection.active) return null;

  const count = entries.length;
  const noun = media === "book" ? "book" : "game";
  const plural = (n: number) => `${n} ${noun}${n === 1 ? "" : "s"}`;

  return (
    <>
      <div
        className="fixed inset-x-2 z-40 lg:left-[17rem]"
        style={{ bottom: "calc(var(--player-h, 0px) + 0.5rem)" }}
        role="region"
        aria-label="Selection"
      >
        <div className="f-panel mx-auto flex max-w-[1600px] flex-wrap items-center gap-2 px-3 py-2.5">
          <p className="mr-1 text-sm font-medium text-ink-100">
            {count === 0 ? `Pick some ${noun}s` : `${plural(count)} selected`}
          </p>
          <button
            type="button"
            onClick={selection.allSelected ? selection.clear : selection.selectAll}
            className="text-xs text-ink-400 transition-colors hover:text-ink-200 focus-visible:focus-ring"
          >
            {selection.allSelected ? "Clear" : `Select all ${shown} shown`}
          </button>

          {notice && (
            <p className="min-w-0 truncate text-xs text-emerald-300" role="status">
              {notice}
            </p>
          )}

          <div className="ml-auto flex flex-wrap items-center gap-1.5">
            <Button size="sm" variant="secondary" disabled={count === 0} onClick={() => setDialog("list")}>
              <Gi name="list-checks" className="size-3.5" />
              Add to list
            </Button>
            <Button size="sm" variant="secondary" disabled={count === 0} onClick={() => setDialog("project")}>
              <Gi name="target" className="size-3.5" />
              Add to project
            </Button>
            <Button size="sm" variant="secondary" disabled={count === 0} onClick={() => setDialog("status")}>
              <Gi name="circle-dashed" className="size-3.5" />
              Set status
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={count === 0}
              className="text-red-400 hover:bg-red-500/10 hover:text-red-300"
              onClick={() => setDialog("remove")}
            >
              <Gi name="trash" className="size-3.5" />
              Remove
            </Button>
            <Button size="sm" variant="ghost" onClick={selection.exit} aria-label="Done selecting">
              <Gi name="x" className="size-3.5" />
              Done
            </Button>
          </div>
        </div>
      </div>

      <AddToListDialog
        open={dialog === "list"}
        onClose={() => setDialog(null)}
        entries={entries}
        onDone={(message) => {
          setDialog(null);
          say(message);
        }}
      />
      <AddToProjectDialog
        open={dialog === "project"}
        onClose={() => setDialog(null)}
        entries={entries}
        media={media}
        onDone={(message) => {
          setDialog(null);
          say(message);
        }}
      />
      <SetStatusDialog
        open={dialog === "status"}
        onClose={() => setDialog(null)}
        entries={entries}
        media={media}
        onDone={(message) => {
          setDialog(null);
          say(message);
        }}
      />
      <RemoveDialog
        open={dialog === "remove"}
        onClose={() => setDialog(null)}
        entries={entries}
        media={media}
        onDone={(message) => {
          setDialog(null);
          selection.clear();
          say(message);
        }}
      />
    </>
  );
}

interface ActionDialogProps {
  open: boolean;
  onClose: () => void;
  entries: Entry[];
  onDone: (message: string) => void;
}

/**
 * Pick a manual list, or name a new one and it is created with the selection
 * already in it. Smart lists are absent for the same reason as on the detail
 * page: their contents are decided by rules.
 */
function AddToListDialog({ open, onClose, entries, onDone }: ActionDialogProps) {
  const queryClient = useQueryClient();
  const { data } = useLists();
  const [name, setName] = useState("");
  const manual = data?.lists.filter((list) => list.kind === "manual") ?? [];
  const ids = entries.map((entry) => entry.id);

  const invalidate = (listId: string) => {
    queryClient.invalidateQueries({ queryKey: ["lists"] });
    queryClient.invalidateQueries({ queryKey: ["list", listId] });
    for (const id of ids) queryClient.invalidateQueries({ queryKey: ["entry-lists", id] });
  };

  const add = useMutation({
    mutationFn: async (list: { id: string; name: string }) => {
      await api.addListItems(list.id, ids);
      return list;
    },
    onSuccess: (list) => {
      invalidate(list.id);
      onDone(`Added ${counted(entries)} to ${list.name}`);
    },
  });

  const create = useMutation({
    mutationFn: async (listName: string) => {
      const list = await api.createList({ name: listName, kind: "manual" });
      await api.addListItems(list.id, ids);
      return list;
    },
    onSuccess: (list) => {
      setName("");
      invalidate(list.id);
      onDone(`Made ${list.name} with ${counted(entries)} in it`);
    },
  });

  const pending = add.isPending || create.isPending;
  const error = add.error ?? create.error;

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (name.trim() && !pending) create.mutate(name.trim());
  };

  return (
    <Dialog open={open} onClose={onClose} label="Add to list">
      <h2 className="text-lg font-semibold text-ink-100">Add {counted(entries)} to a list</h2>

      {manual.length === 0 ? (
        <p className="mt-2 text-sm text-ink-400">No manual lists yet — name one below.</p>
      ) : (
        <div className="mt-4 max-h-72 space-y-0.5 overflow-y-auto pr-1">
          {manual.map((list) => (
            <button
              key={list.id}
              type="button"
              disabled={pending}
              onClick={() => add.mutate({ id: list.id, name: list.name })}
              className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-sm text-ink-200 transition-colors hover:bg-fill-hover hover:text-ink-100 focus-visible:focus-ring disabled:cursor-not-allowed disabled:opacity-60"
            >
              <Gi name="list-checks" className="size-3.5 shrink-0 text-ink-500" />
              <span className="min-w-0 flex-1 truncate">{list.name}</span>
              <span className="shrink-0 text-[11px] tabular-nums text-ink-600">{list.count}</span>
            </button>
          ))}
        </div>
      )}

      <form onSubmit={submit} className="mt-4 flex items-center gap-2">
        <Input
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="New list…"
          aria-label="New list name"
          disabled={pending}
        />
        <Button type="submit" variant="primary" size="sm" loading={create.isPending} disabled={!name.trim()}>
          <Gi name="plus" className="size-3.5" />
          Create
        </Button>
      </form>

      {error && <p className="mt-2 text-xs text-red-300">{describe(error)}</p>}
      <p className="mt-3 text-xs text-ink-500">
        Anything already on the list stays where it is.{" "}
        <Link to="/lists" className="text-brand-400 hover:text-brand-300">
          All lists
        </Link>
      </p>
    </Dialog>
  );
}

/** Pick an open checklist in this arena; goal projects have no members. */
function AddToProjectDialog({
  open,
  onClose,
  entries,
  media,
  onDone,
}: ActionDialogProps & { media: MediaType }) {
  const queryClient = useQueryClient();
  const { data } = useProjects();
  const checklists =
    data?.projects.filter(
      (project) =>
        project.kind === "checklist" && !project.completed_at && project.media_scope === media,
    ) ?? [];
  const ids = entries.map((entry) => entry.id);

  const add = useMutation({
    mutationFn: async (project: { id: string; name: string }) => {
      await api.addProjectItems(project.id, ids);
      return project;
    },
    onSuccess: (project) => {
      queryClient.invalidateQueries({ queryKey: ["projects"] });
      queryClient.invalidateQueries({ queryKey: ["project", project.id] });
      for (const id of ids) queryClient.invalidateQueries({ queryKey: ["entry-projects", id] });
      onDone(`Added ${counted(entries)} to ${project.name}`);
    },
  });

  return (
    <Dialog open={open} onClose={onClose} label="Add to project">
      <h2 className="text-lg font-semibold text-ink-100">Add {counted(entries)} to a project</h2>

      {checklists.length === 0 ? (
        <p className="mt-2 text-sm text-ink-400">
          No open checklist projects in this arena.{" "}
          <Link to="/projects" className="text-brand-400 hover:text-brand-300">
            Start one
          </Link>{" "}
          and come back — goal projects count the whole library, so they have nothing to add to.
        </p>
      ) : (
        <div className="mt-4 max-h-72 space-y-0.5 overflow-y-auto pr-1">
          {checklists.map((project) => (
            <button
              key={project.id}
              type="button"
              disabled={add.isPending}
              onClick={() => add.mutate({ id: project.id, name: project.name })}
              className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-sm text-ink-200 transition-colors hover:bg-fill-hover hover:text-ink-100 focus-visible:focus-ring disabled:cursor-not-allowed disabled:opacity-60"
            >
              <Gi name="target" className="size-3.5 shrink-0 text-ink-500" />
              <span className="min-w-0 flex-1 truncate">{project.name}</span>
              <span className="shrink-0 text-[11px] tabular-nums text-ink-600">
                {project.progress.completed_count}/{project.progress.target_count}
              </span>
            </button>
          ))}
        </div>
      )}

      {add.error && <p className="mt-2 text-xs text-red-300">{describe(add.error)}</p>}
    </Dialog>
  );
}

/**
 * One status for the lot. The single-entry menu asks which platform a game
 * was finished on; here it does not, because the answer differs per game and
 * forty prompts is not a shortcut. Platforms can still be set one at a time
 * afterwards.
 */
function SetStatusDialog({
  open,
  onClose,
  entries,
  media,
  onDone,
}: ActionDialogProps & { media: MediaType }) {
  const queryClient = useQueryClient();

  const set = useMutation({
    mutationFn: async (status: Status) => {
      const results = await Promise.all(
        entries
          .filter((entry) => entry.status !== status)
          .map((entry) => api.updateEntry(entry.id, { status })),
      );
      for (const result of results) dispatchUnlocks(result.unlocks);
      return status;
    },
    onSuccess: (status) => {
      invalidateLibrary(queryClient);
      onDone(`Marked ${counted(entries)} as ${statusLabel(status, media).toLowerCase()}`);
    },
  });

  return (
    <Dialog open={open} onClose={onClose} label="Set status">
      <h2 className="text-lg font-semibold text-ink-100">Mark {counted(entries)} as…</h2>
      <div className="mt-4 grid grid-cols-2 gap-1.5 sm:grid-cols-3">
        {STATUSES.map((status) => (
          <button
            key={status}
            type="button"
            disabled={set.isPending}
            onClick={() => set.mutate(status)}
            className="flex items-center gap-2 rounded-lg px-3 py-2 text-left text-sm text-ink-200 transition-colors hover:bg-fill-hover hover:text-ink-100 focus-visible:focus-ring disabled:cursor-not-allowed disabled:opacity-60"
          >
            <Gi name={STATUS_ICONS[status]} className="size-3.5 shrink-0 text-ink-500" />
            {statusLabel(status, media)}
          </button>
        ))}
      </div>
      {media === "game" && (
        <p className="mt-3 text-xs text-ink-500">
          Finishing games this way does not ask which platform each was played on; set that on a
          game's page if it matters to you.
        </p>
      )}
      {set.error && <p className="mt-2 text-xs text-red-300">{describe(set.error)}</p>}
    </Dialog>
  );
}

/** Off the shelf, with the same warning the detail page gives, times N. */
function RemoveDialog({
  open,
  onClose,
  entries,
  media,
  onDone,
}: ActionDialogProps & { media: MediaType }) {
  const queryClient = useQueryClient();

  const remove = useMutation({
    mutationFn: async () => {
      await Promise.all(entries.map((entry) => api.deleteEntry(entry.id)));
      return entries.length;
    },
    onSuccess: (n) => {
      invalidateLibrary(queryClient);
      onDone(`Removed ${n} ${media === "book" ? "book" : "game"}${n === 1 ? "" : "s"} from the shelf`);
    },
  });

  return (
    <Dialog open={open} onClose={onClose} label="Confirm removal">
      <h2 className="text-lg font-semibold text-ink-100">Remove {counted(entries)}?</h2>
      <p className="mt-2 text-sm text-ink-400">
        This takes them off your shelf, along with your ratings and notes. They stay searchable, so
        you can add any of them again later.
      </p>
      <div className="mt-6 flex justify-end gap-2">
        <Button variant="ghost" onClick={onClose} disabled={remove.isPending}>
          Cancel
        </Button>
        <Button variant="danger" loading={remove.isPending} onClick={() => remove.mutate()}>
          Remove
        </Button>
      </div>
      {remove.error && <p className="mt-2 text-xs text-red-300">{describe(remove.error)}</p>}
    </Dialog>
  );
}

/** "12 books" / "1 game" — the selection, counted in its own noun. */
function counted(entries: Entry[]): string {
  const n = entries.length;
  const noun = entries[0]?.media_type === "book" ? "book" : "game";
  return `${n} ${noun}${n === 1 ? "" : "s"}`;
}

function describe(error: unknown): string {
  return error instanceof Error ? error.message : "That did not work.";
}

/**
 * The overlay a card wears in selection mode: a check in the corner, a ring
 * when picked, and a full-face hit target so tapping anywhere on the cover
 * toggles it rather than opening the page.
 */
export function SelectOverlay({
  selected,
  label,
  onToggle,
}: {
  selected: boolean;
  label: string;
  onToggle: (event: React.MouseEvent) => void;
}) {
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={selected}
      aria-label={`Select ${label}`}
      onClick={onToggle}
      className={cn(
        "absolute inset-0 z-10 rounded-xl transition-shadow focus-visible:focus-ring",
        selected ? "ring-[3px] ring-brand-500" : "ring-0",
      )}
    >
      <span
        className={cn(
          "absolute right-2 top-2 flex size-6 items-center justify-center rounded-full border-2 backdrop-blur-sm transition-colors",
          selected
            ? "border-brand-500 bg-brand-500 text-white"
            : "border-white/70 bg-scrim/60 text-transparent",
        )}
        aria-hidden="true"
      >
        <Gi name="check" className="size-3.5" />
      </span>
    </button>
  );
}
