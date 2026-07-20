import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api } from "@/lib/api";
import type { Entry, GameList, RuleSet } from "@/lib/types";

export function useLists() {
  return useQuery({ queryKey: ["lists"], queryFn: api.lists });
}

export function useList(id: string | undefined) {
  return useQuery({
    queryKey: ["list", id],
    queryFn: () => api.getList(id!),
    enabled: Boolean(id),
  });
}

export function useSmartFields() {
  return useQuery({
    queryKey: ["smart-fields"],
    queryFn: api.smartFields,
    // The field catalogue is static for the life of the server build.
    staleTime: Infinity,
  });
}

/** Which manual lists a given entry belongs to. */
export function useEntryLists(entryId: string | undefined) {
  return useQuery({
    queryKey: ["entry-lists", entryId],
    queryFn: () => api.entryLists(entryId!),
    enabled: Boolean(entryId),
  });
}

/**
 * Toggles an entry's membership of a manual list, updating the checkbox
 * immediately and rolling back if the server disagrees.
 */
export function useToggleListMembership(entryId: string | undefined) {
  const queryClient = useQueryClient();
  const key = ["entry-lists", entryId];

  return useMutation({
    // Normalised to void: the two calls return different shapes, and a union
    // return type confuses the mutation's context inference.
    mutationFn: async ({ listId, member }: { listId: string; member: boolean }) => {
      if (member) {
        await api.removeListItem(listId, entryId!);
      } else {
        await api.addListItem(listId, entryId!);
      }
    },

    onMutate: async ({ listId, member }) => {
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<{ list_ids: string[] }>(key);
      queryClient.setQueryData<{ list_ids: string[] }>(key, (current) => ({
        list_ids: member
          ? (current?.list_ids ?? []).filter((id) => id !== listId)
          : [...(current?.list_ids ?? []), listId],
      }));
      return { previous };
    },

    onError: (_error, _variables, context) => {
      if (context?.previous) queryClient.setQueryData(key, context.previous);
    },

    onSettled: (_data, _error, { listId }) => {
      queryClient.invalidateQueries({ queryKey: key });
      queryClient.invalidateQueries({ queryKey: ["lists"] });
      queryClient.invalidateQueries({ queryKey: ["list", listId] });
    },
  });
}

/** Reorders an entry within a manual list, optimistically. */
export function useReorderListItem(listId: string | undefined) {
  const queryClient = useQueryClient();
  const key = ["list", listId];

  return useMutation({
    mutationFn: ({ entryId, beforeId, afterId }: {
      entryId: string;
      beforeId: string;
      afterId: string;
      reordered: Entry[];
    }) => api.reorderListItem(listId!, entryId, beforeId, afterId),

    onMutate: async ({ reordered }) => {
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<{ list: GameList; entries: Entry[] }>(key);
      if (previous) {
        queryClient.setQueryData(key, { ...previous, entries: reordered });
      }
      return { previous };
    },

    onError: (_error, _variables, context) => {
      if (context?.previous) queryClient.setQueryData(key, context.previous);
    },

    onSettled: () => queryClient.invalidateQueries({ queryKey: key }),
  });
}

export function useCreateList() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: api.createList,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["lists"] }),
  });
}

export function useUpdateList() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: {
      id: string;
      patch: { name?: string; description?: string; rules?: RuleSet };
    }) => api.updateList(id, patch),
    onSuccess: (list) => {
      queryClient.invalidateQueries({ queryKey: ["lists"] });
      queryClient.invalidateQueries({ queryKey: ["list", list.id] });
    },
  });
}

export function useDeleteList() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: api.deleteList,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["lists"] }),
  });
}

export function useListItemMutations(listId: string | undefined) {
  const queryClient = useQueryClient();
  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ["list", listId] });
    queryClient.invalidateQueries({ queryKey: ["lists"] });
  };

  const add = useMutation({
    mutationFn: (entryId: string) => api.addListItem(listId!, entryId),
    onSuccess: invalidate,
  });

  const remove = useMutation({
    mutationFn: (entryId: string) => api.removeListItem(listId!, entryId),
    onSuccess: invalidate,
  });

  return { add, remove };
}
