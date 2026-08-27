import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api } from "@/lib/api";
import type { Project, ProjectItem, RuleSet } from "@/lib/types";

export function useProjects() {
  return useQuery({ queryKey: ["projects"], queryFn: api.projects });
}

export function useProject(id: string | undefined) {
  return useQuery({
    queryKey: ["project", id],
    queryFn: () => api.getProject(id!),
    enabled: Boolean(id),
  });
}

/** Which checklist projects a given entry belongs to. */
export function useEntryProjects(entryId: string | undefined) {
  return useQuery({
    queryKey: ["entry-projects", entryId],
    queryFn: () => api.entryProjects(entryId!),
    enabled: Boolean(entryId),
  });
}

/**
 * Toggles an entry's membership of a checklist project, updating the checkbox
 * immediately and rolling back if the server disagrees.
 */
export function useToggleProjectMembership(entryId: string | undefined) {
  const queryClient = useQueryClient();
  const key = ["entry-projects", entryId];

  return useMutation({
    mutationFn: async ({ projectId, member }: { projectId: string; member: boolean }) => {
      if (member) {
        await api.removeProjectItem(projectId, entryId!);
      } else {
        await api.addProjectItem(projectId, entryId!);
      }
    },

    onMutate: async ({ projectId, member }) => {
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<{ project_ids: string[] }>(key);
      queryClient.setQueryData<{ project_ids: string[] }>(key, (current) => ({
        project_ids: member
          ? (current?.project_ids ?? []).filter((id) => id !== projectId)
          : [...(current?.project_ids ?? []), projectId],
      }));
      return { previous };
    },

    onError: (_error, _variables, context) => {
      if (context?.previous) queryClient.setQueryData(key, context.previous);
    },

    onSettled: (_data, _error, { projectId }) => {
      queryClient.invalidateQueries({ queryKey: key });
      queryClient.invalidateQueries({ queryKey: ["projects"] });
      queryClient.invalidateQueries({ queryKey: ["project", projectId] });
    },
  });
}

export function useCreateProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: api.createProject,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["projects"] }),
  });
}

export function useUpdateProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: {
      id: string;
      patch: {
        name?: string;
        description?: string;
        target_count?: number | null;
        rules?: RuleSet;
        completed?: boolean;
      };
    }) => api.updateProject(id, patch),
    onSuccess: (project) => {
      queryClient.invalidateQueries({ queryKey: ["projects"] });
      queryClient.invalidateQueries({ queryKey: ["project", project.id] });
    },
  });
}

export function useDeleteProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: api.deleteProject,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["projects"] }),
  });
}

export function useProjectItemMutations(projectId: string | undefined) {
  const queryClient = useQueryClient();
  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ["project", projectId] });
    queryClient.invalidateQueries({ queryKey: ["projects"] });
  };

  const remove = useMutation({
    mutationFn: (entryId: string) => api.removeProjectItem(projectId!, entryId),
    onSuccess: invalidate,
  });

  /** Sets or clears the manual per-item done override. */
  const setDone = useMutation({
    mutationFn: ({ entryId, done }: { entryId: string; done: boolean | null }) =>
      api.setProjectItemDone(projectId!, entryId, done),
    onSuccess: invalidate,
  });

  return { remove, setDone };
}

/** Reorders an entry within a checklist project, optimistically. */
export function useReorderProjectItem(projectId: string | undefined) {
  const queryClient = useQueryClient();
  const key = ["project", projectId];

  return useMutation({
    mutationFn: ({ entryId, beforeId, afterId }: {
      entryId: string;
      beforeId: string;
      afterId: string;
      reordered: ProjectItem[];
    }) => api.reorderProjectItem(projectId!, entryId, beforeId, afterId),

    onMutate: async ({ reordered }) => {
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<{ project: Project; items: ProjectItem[] }>(key);
      if (previous) {
        queryClient.setQueryData(key, { ...previous, items: reordered });
      }
      return { previous };
    },

    onError: (_error, _variables, context) => {
      if (context?.previous) queryClient.setQueryData(key, context.previous);
    },

    onSettled: () => queryClient.invalidateQueries({ queryKey: key }),
  });
}
