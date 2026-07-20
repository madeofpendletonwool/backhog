import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api } from "@/lib/api";

/** Invalidates everything that a logged session can change. */
function invalidateAfterSession(
  queryClient: ReturnType<typeof useQueryClient>,
  entryId: string | undefined,
) {
  queryClient.invalidateQueries({ queryKey: ["sessions", entryId] });
  // Logging can flip a backlog game to playing, so the entry, the queue, the
  // library and the stats can all move.
  queryClient.invalidateQueries({ queryKey: ["entry", entryId] });
  queryClient.invalidateQueries({ queryKey: ["library"] });
  queryClient.invalidateQueries({ queryKey: ["queue"] });
  queryClient.invalidateQueries({ queryKey: ["stats"] });
  queryClient.invalidateQueries({ queryKey: ["lists"] });
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
    onSuccess: () => invalidateAfterSession(queryClient, entryId),
  });
}

export function useDeleteSession(entryId: string | undefined) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (sessionId: string) => api.deleteSession(sessionId),
    onSuccess: () => invalidateAfterSession(queryClient, entryId),
  });
}
