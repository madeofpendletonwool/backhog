import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";

/**
 * What the sign-in pages may offer: whether the front door is open, whether
 * this is a fresh install with no accounts yet, and what an invite token in
 * the URL grants.
 *
 * It is read unauthenticated, before anyone has an account, and re-read on
 * every mount rather than cached for long: an admin closing registration in
 * one tab should not leave a sign-up form standing in another.
 */
export function useAuthConfig(inviteToken?: string) {
  return useQuery({
    queryKey: ["auth-config", inviteToken ?? ""],
    queryFn: () => api.authConfig(inviteToken),
    staleTime: 30 * 1000,
    retry: 1,
  });
}
