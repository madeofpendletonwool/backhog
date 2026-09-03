import { createContext, useCallback, useContext, useEffect, useMemo } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { usePersistentState } from "./usePersistentState";
import { arenaForLocation, arenaHome, type Arena } from "@/lib/arena";

/**
 * Which arena the app is currently in.
 *
 * This used to be local state inside Layout, which was fine while the arena
 * only picked a nav list. It is a provider now because the *theme* follows the
 * arena too, and ThemeProvider sits above Layout — the two need one answer, not
 * two copies of the same localStorage key (`usePersistentState` has no storage
 * listener, so two readers would drift the moment one of them wrote).
 *
 * Mount this inside the router: it needs `useLocation`.
 */

interface ArenaValue {
  arena: Arena;
  /** Switch arenas and land on that arena's home page. */
  switchArena: (next: Arena) => void;
}

const ArenaContext = createContext<ArenaValue>({ arena: "games", switchArena: () => {} });

export function ArenaProvider({ children }: { children: React.ReactNode }) {
  const { pathname, search } = useLocation();
  const navigate = useNavigate();

  // The remembered mode, so a reload lands where you left off; the URL
  // overrides it whenever the URL is unambiguous.
  const [savedArena, setSavedArena] = usePersistentState<Arena>("backhog:arena", "games");
  const routeArena = arenaForLocation(pathname, search);
  const arena: Arena = routeArena ?? savedArena;

  useEffect(() => {
    if (routeArena && routeArena !== savedArena) setSavedArena(routeArena);
  }, [routeArena, savedArena, setSavedArena]);

  const switchArena = useCallback(
    (next: Arena) => {
      setSavedArena(next);
      navigate(arenaHome[next]);
    },
    [navigate, setSavedArena],
  );

  const value = useMemo<ArenaValue>(() => ({ arena, switchArena }), [arena, switchArena]);

  return <ArenaContext.Provider value={value}>{children}</ArenaContext.Provider>;
}

export function useArena() {
  return useContext(ArenaContext);
}
