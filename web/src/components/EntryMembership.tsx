import { useState } from "react";

import { CreateListDialog } from "./CreateListDialog";
import { Gi } from "./ui/Gi";
import { Button, Panel } from "./ui/primitives";
import { useEntryLists, useLists, useToggleListMembership } from "@/hooks/useLists";
import { useEntryProjects, useProjects, useToggleProjectMembership } from "@/hooks/useProjects";
import type { Entry } from "@/lib/types";

/**
 * List and project membership for one entry. Both are keyed on the entry id
 * and know nothing about media, so a game detail page and a book detail page
 * render the same two panels rather than each growing their own copy.
 */

/**
 * Manual-list membership. Smart lists are excluded: their contents are decided
 * by rules, so a checkbox here would be a lie.
 */
export function ListMembership({ entry }: { entry: Entry }) {
  const { data: listData } = useLists();
  const { data: membership } = useEntryLists(entry.id);
  const toggle = useToggleListMembership(entry.id);
  const [creating, setCreating] = useState(false);

  const manualLists = listData?.lists.filter((list) => list.kind === "manual") ?? [];
  const memberOf = new Set(membership?.list_ids ?? []);

  return (
    <Panel className="p-5">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-semibold text-ink-200">Lists</h2>
        <Button size="sm" variant="ghost" onClick={() => setCreating(true)}>
          <Gi name="plus" className="size-3.5" />
          New
        </Button>
      </div>

      {manualLists.length === 0 ? (
        <p className="text-xs leading-relaxed text-ink-500">
          No manual lists yet. Create one to group games however you like.
        </p>
      ) : (
        <div className="space-y-0.5">
          {manualLists.map((list) => {
            const member = memberOf.has(list.id);
            return (
              <label
                key={list.id}
                className="flex cursor-pointer items-center gap-2.5 rounded-lg px-2 py-1.5 transition-colors hover:bg-fill-hover"
              >
                <input
                  type="checkbox"
                  checked={member}
                  disabled={toggle.isPending}
                  onChange={() => toggle.mutate({ listId: list.id, member })}
                  className="size-4 shrink-0 accent-brand-500"
                />
                <span className="min-w-0 flex-1 truncate text-sm text-ink-200">{list.name}</span>
                <span className="shrink-0 text-[11px] tabular-nums text-ink-600">{list.count}</span>
              </label>
            );
          })}
        </div>
      )}

      <CreateListDialog open={creating} onClose={() => setCreating(false)} />
    </Panel>
  );
}

/**
 * Checklist-project membership. Goal projects are excluded: their target is
 * the whole library or a rule set, not a curated list.
 */
export function ProjectMembership({ entry }: { entry: Entry }) {
  const { data: projectData } = useProjects();
  const { data: membership } = useEntryProjects(entry.id);
  const toggle = useToggleProjectMembership(entry.id);

  const checklists = projectData?.projects.filter(
    (project) => project.kind === "checklist" && !project.completed_at,
  ) ?? [];
  const memberOf = new Set(membership?.project_ids ?? []);

  if (checklists.length === 0) return null;

  return (
    <Panel className="p-5">
      <h2 className="mb-1 text-sm font-semibold text-ink-200">Projects</h2>
      <p className="mb-3 text-xs text-ink-500">Working on something? Check it in.</p>
      <div className="space-y-0.5">
        {checklists.map((project) => {
          const member = memberOf.has(project.id);
          return (
            <label
              key={project.id}
              className="flex cursor-pointer items-center gap-2.5 rounded-lg px-2 py-1.5 transition-colors hover:bg-fill-hover"
            >
              <input
                type="checkbox"
                checked={member}
                disabled={toggle.isPending}
                onChange={() => toggle.mutate({ projectId: project.id, member })}
                className="size-4 shrink-0 accent-brand-500"
              />
              <span className="min-w-0 flex-1 truncate text-sm text-ink-200">{project.name}</span>
              <span className="shrink-0 text-[11px] tabular-nums text-ink-600">
                {project.progress.completed_count}/{project.progress.target_count}
              </span>
            </label>
          );
        })}
      </div>
    </Panel>
  );
}
