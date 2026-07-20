import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createContext, useCallback, useContext, type ReactNode } from "react";

import { ApiError, api } from "@/lib/api";
import type { User } from "@/lib/types";

interface AuthValue {
  user: User | null;
  loading: boolean;
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();

  const { data, isLoading } = useQuery({
    queryKey: ["me"],
    queryFn: api.me,
    // A 401 here is the normal "not logged in" state, not a transient failure.
    retry: (failureCount, error) =>
      !(error instanceof ApiError && error.status === 401) && failureCount < 2,
    staleTime: 5 * 60 * 1000,
  });

  const setUser = useCallback(
    (user: User) => {
      queryClient.setQueryData(["me"], user);
    },
    [queryClient],
  );

  const login = useCallback(
    async (email: string, password: string) => setUser(await api.login(email, password)),
    [setUser],
  );

  const register = useCallback(
    async (email: string, username: string, password: string) =>
      setUser(await api.register(email, username, password)),
    [setUser],
  );

  const logout = useCallback(async () => {
    await api.logout();
    // Drop every cached query: none of it belongs to the next user.
    queryClient.clear();
    queryClient.setQueryData(["me"], null);
  }, [queryClient]);

  return (
    <AuthContext.Provider
      value={{ user: data ?? null, loading: isLoading, login, register, logout }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthValue {
  const context = useContext(AuthContext);
  if (!context) throw new Error("useAuth must be used inside an AuthProvider");
  return context;
}
