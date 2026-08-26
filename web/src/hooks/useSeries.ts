import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api } from "@/lib/api";
import type { PlayOrder, SeriesDetail } from "@/lib/types";

/**
 * Series queries. Backfill status is polled while a walk runs so the index
 * fills in as IGDB data lands.
 */
export function useSeriesIndex() {
  return useQuery({
    queryKey: ["series"],
    queryFn: api.seriesIndex,
  });
}

export function useSeriesBackfillStatus(enabled: boolean) {
  return useQuery({
    queryKey: ["series", "backfill"],
    queryFn: api.seriesBackfillStatus,
    enabled,
    // Poll an active walk; the walk paces itself against the IGDB rate limit,
    // so a gentle interval is plenty.
    refetchInterval: (query) => (query.state.data?.running ? 4000 : false),
  });
}

export function useKickSeriesBackfill() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: api.kickSeriesBackfill,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["series"] });
    },
  });
}

export function useSeriesDetail(id: string | undefined) {
  return useQuery({
    queryKey: ["series", id],
    queryFn: () => api.seriesDetail(id!),
    enabled: Boolean(id),
  });
}

/** The series a game belongs to, for the detail-page chip. */
export function useGameSeries(gameId: number | undefined) {
  return useQuery({
    queryKey: ["series", "game", gameId],
    queryFn: () => api.gameSeries(gameId!),
    enabled: Boolean(gameId),
  });
}

export function useSetSeriesPlayOrder() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, playOrder }: { id: string; playOrder: PlayOrder }) =>
      api.setSeriesPlayOrder(id, playOrder),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["series"] });
    },
  });
}

/** One optimistic custom-journey move. */
interface ReorderSeriesInput {
  gameId: number;
  beforeId: number;
  afterId: number;
  /** The full desired order, applied optimistically before the server replies. */
  reordered: number[];
}

/**
 * Reorders a custom journey optimistically: the member list is rewritten
 * locally the instant a drag ends, and rolled back if the server disagrees.
 */
export function useReorderSeries(id: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ gameId, beforeId, afterId }: ReorderSeriesInput) =>
      api.reorderSeries(id, gameId, beforeId, afterId),

    onMutate: async ({ reordered }) => {
      await queryClient.cancelQueries({ queryKey: ["series", id] });
      const previous = queryClient.getQueryData<SeriesDetail>(["series", id]);
      if (previous) {
        const byID = new Map(previous.members.map((member) => [member.game.id, member]));
        queryClient.setQueryData<SeriesDetail>(["series", id], {
          ...previous,
          members: reordered
            .map((gameId) => byID.get(gameId))
            .filter((member): member is SeriesDetail["members"][number] => member != null),
        });
      }
      return { previous };
    },

    onError: (_error, _variables, context) => {
      if (context?.previous) queryClient.setQueryData(["series", id], context.previous);
    },

    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["series", id] });
    },
  });
}
