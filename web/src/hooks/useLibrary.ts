import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { api } from "@/lib/api";
import type { Entry, Status } from "@/lib/types";

export interface LibraryParams {
  status?: string;
  q?: string;
  platform?: number;
  genre?: number;
  sort?: string;
  list?: string;
}

/** Invalidates every view whose contents depend on entry state. */
function invalidateLibrary(queryClient: ReturnType<typeof useQueryClient>) {
  for (const key of ["library", "queue", "stats", "lists", "list", "entry", "facets"]) {
    queryClient.invalidateQueries({ queryKey: [key] });
  }
}

/** How many entries each library page requests. */
export const PAGE_SIZE = 60;

/**
 * Paged library query. The grid appends pages rather than replacing them, so
 * "load more" never loses your scroll position. Changing any filter changes the
 * query key, which starts a fresh page 1.
 */
export function useLibrary(params: LibraryParams) {
  return useInfiniteQuery({
    queryKey: ["library", params],
    initialPageParam: 0,
    queryFn: ({ pageParam }) =>
      api.library({
        ...(params as Record<string, string | number | undefined>),
        limit: PAGE_SIZE,
        offset: pageParam,
      }),
    getNextPageParam: (lastPage, allPages) => {
      const loaded = allPages.reduce((count, page) => count + page.entries.length, 0);
      return loaded < lastPage.total ? loaded : undefined;
    },
    placeholderData: (previous) => previous, // keep the grid stable while filtering
  });
}

export function useStats() {
  return useQuery({ queryKey: ["stats"], queryFn: api.stats });
}

export function useFacets() {
  return useQuery({ queryKey: ["facets"], queryFn: api.facets });
}

export function useEntry(id: string | undefined) {
  return useQuery({
    queryKey: ["entry", id],
    queryFn: () => api.getEntry(id!),
    enabled: Boolean(id),
  });
}

export function useAddToLibrary() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ gameId, status }: { gameId: number; status?: Status }) =>
      api.addToLibrary(gameId, status),
    onSuccess: () => invalidateLibrary(queryClient),
  });
}

export function useUpdateEntry() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: Record<string, unknown> }) =>
      api.updateEntry(id, patch),
    onSuccess: (entry) => {
      queryClient.setQueryData(["entry", entry.id], entry);
      invalidateLibrary(queryClient);
    },
  });
}

export function useDeleteEntry() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteEntry(id),
    onSuccess: () => invalidateLibrary(queryClient),
  });
}

export function useQueue() {
  return useQuery({ queryKey: ["queue"], queryFn: api.queue });
}

/**
 * Reorders the play queue optimistically: the list is rewritten locally the
 * instant a drag ends, and rolled back if the server rejects the move.
 */
export function useReorderQueue() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ entryId, beforeId, afterId }: {
      entryId: string;
      beforeId: string;
      afterId: string;
      reordered: Entry[];
    }) => api.reorderQueue(entryId, beforeId, afterId),

    onMutate: async ({ reordered }) => {
      await queryClient.cancelQueries({ queryKey: ["queue"] });
      const previous = queryClient.getQueryData<{ entries: Entry[] }>(["queue"]);
      queryClient.setQueryData(["queue"], { entries: reordered });
      return { previous };
    },

    onError: (_error, _variables, context) => {
      if (context?.previous) queryClient.setQueryData(["queue"], context.previous);
    },

    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["queue"] });
    },
  });
}

/** Debounces a rapidly changing value, for search-as-you-type. */
export function useDebounced<T>(value: T, delay = 250): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}
