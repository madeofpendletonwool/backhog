import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { dispatchUnlocks } from "@/hooks/useAchievements";
import { api } from "@/lib/api";

/** Invalidates everything that a logged session can change. */
function invalidateAfterSession(
  queryClient: ReturnType<typeof useQueryClient>,
  entryId: string | undefined,
) {
  queryClient.invalidateQueries({ queryKey: ["sessions", entryId] });
  // Logging can flip a backlog game to playing, so the entry, the queue, the
  // library and the stats can all move — and a finished game's totals with it.
  queryClient.invalidateQueries({ queryKey: ["entry", entryId] });
  queryClient.invalidateQueries({ queryKey: ["library"] });
  queryClient.invalidateQueries({ queryKey: ["queue"] });
  queryClient.invalidateQueries({ queryKey: ["stats"] });
  queryClient.invalidateQueries({ queryKey: ["lists"] });
  queryClient.invalidateQueries({ queryKey: ["achievements"] });
  queryClient.invalidateQueries({ queryKey: ["season"] });
}

export function useSessions(entryId: string | undefined) {
  return useQuery({
    queryKey: ["sessions", entryId],
    queryFn: () => api.sessions(entryId!),
    enabled: Boolean(entryId),
  });
}

export function useAddSession(entryId: string | undefined) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { minutes: number; played_on?: string; note?: string }) =>
      api.addSession(entryId!, input),
    onSuccess: ({ unlocks }) => {
      invalidateAfterSession(queryClient, entryId);
      dispatchUnlocks(unlocks);
    },
  });
}

export function useDeleteSession(entryId: string | undefined) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (sessionId: string) => api.deleteSession(sessionId),
    onSuccess: () => invalidateAfterSession(queryClient, entryId),
  });
}
