import { useQuery } from "@tanstack/react-query";

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
