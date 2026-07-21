import { useEffect, useState } from "react";

/**
 * useState that survives navigation and reload by mirroring the value into
 * localStorage under `key`. Used for the library's filter/sort/view controls so
 * the page doesn't snap back to defaults every time you leave and come back.
 *
 * A bad or stale stored value (hand-edited, or written by an older build) falls
 * back to `initial` rather than throwing — a corrupt key must never white-screen
 * the page.
 */
export function usePersistentState<T>(key: string, initial: T) {
  const [value, setValue] = useState<T>(() => {
    try {
      const stored = localStorage.getItem(key);
      return stored === null ? initial : (JSON.parse(stored) as T);
    } catch {
      return initial;
    }
  });

  useEffect(() => {
    try {
      localStorage.setItem(key, JSON.stringify(value));
    } catch {
      // Storage full or unavailable (private mode): the in-memory value still
      // works for this session, so there's nothing useful to do here.
    }
  }, [key, value]);

  return [value, setValue] as const;
}
