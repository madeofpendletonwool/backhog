import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";

import { api } from "@/lib/api";
import type { AchievementStatus } from "@/lib/types";

/**
 * Unlocks arrive on mutation responses (PATCH entry, POST session) rather than
 * a poll, so the hooks that fired them broadcast through a window event and
 * the toast stack mounted in the Layout listens — no prop-drilling through
 * every status menu and session form.
 */
export const UNLOCK_EVENT = "backhog:unlocks";

export function dispatchUnlocks(unlocks: AchievementStatus[] | undefined) {
  if (!unlocks || unlocks.length === 0) return;
  window.dispatchEvent(new CustomEvent<AchievementStatus[]>(UNLOCK_EVENT, { detail: unlocks }));
}

export function useAchievements() {
  return useQuery({ queryKey: ["achievements"], queryFn: api.achievements });
}

/** The current year's season by default. */
export function useSeason() {
  return useQuery({ queryKey: ["season"], queryFn: () => api.season() });
}

/** The books arena's year card, same shape as useSeason. */
export function useReadingSeason() {
  return useQuery({ queryKey: ["reading-season"], queryFn: () => api.readingSeason() });
}

/**
 * Fires an easter egg. Silent by design: when the endpoint rejects the id,
 * throttles, or is unreachable, nothing happens — an egg that doesn't hatch
 * shouldn't announce itself. The reveal rides the normal unlock toast, and
 * only when this call is the one that unlocked it.
 */
export function useEggUnlock() {
  const queryClient = useQueryClient();
  return useCallback(
    async (id: string) => {
      try {
        const { unlocked, achievement } = await api.unlockEgg(id);
        if (unlocked) {
          dispatchUnlocks([achievement]);
          void queryClient.invalidateQueries({ queryKey: ["achievements"] });
        }
      } catch {
        // Eggs stay quiet on failure.
      }
    },
    [queryClient],
  );
}
