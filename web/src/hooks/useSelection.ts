import { useCallback, useEffect, useMemo, useRef, useState } from "react";

/**
 * Multi-select for a shelf. The page owns the order of what is on screen;
 * this owns which of it is picked, in the order it was picked — a list built
 * from a selection lands in the order you chose, not the order you sorted.
 *
 * Range selection follows the desktop convention: shift-click extends from
 * the last thing you clicked to this one, through everything shown between.
 * Escape leaves selection mode; leaving it always clears the selection, so
 * a stale pick from ten minutes ago never rides along with a fresh one.
 */
export function useSelection(visibleIds: string[]) {
  const [active, setActive] = useState(false);
  const [picked, setPicked] = useState<string[]>([]);
  const anchorRef = useRef<string | null>(null);

  const selected = useMemo(() => new Set(picked), [picked]);

  const toggle = useCallback(
    (id: string, options: { range?: boolean } = {}) => {
      setPicked((current) => {
        const has = current.includes(id);
        const anchor = anchorRef.current;
        anchorRef.current = id;

        if (options.range && anchor && anchor !== id) {
          const from = visibleIds.indexOf(anchor);
          const to = visibleIds.indexOf(id);
          if (from >= 0 && to >= 0) {
            const span = visibleIds.slice(Math.min(from, to), Math.max(from, to) + 1);
            // Extending from a selected anchor selects the span; from a
            // deselected one, deselects it — the same gesture in reverse.
            if (current.includes(anchor)) {
              const seen = new Set(current);
              return [...current, ...span.filter((each) => !seen.has(each))];
            }
            const drop = new Set(span);
            return current.filter((each) => !drop.has(each));
          }
        }
        return has ? current.filter((each) => each !== id) : [...current, id];
      });
    },
    [visibleIds],
  );

  const selectAll = useCallback(() => {
    setPicked((current) => {
      const seen = new Set(current);
      return [...current, ...visibleIds.filter((id) => !seen.has(id))];
    });
  }, [visibleIds]);

  const clear = useCallback(() => {
    setPicked([]);
    anchorRef.current = null;
  }, []);

  const exit = useCallback(() => {
    setActive(false);
    clear();
  }, [clear]);

  const enter = useCallback(() => setActive(true), []);

  useEffect(() => {
    if (!active) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") exit();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [active, exit]);

  // Whatever scrolled off the page — a filter change, a status move that
  // took it off this tab — is no longer something the bar can act on.
  const visible = useMemo(() => new Set(visibleIds), [visibleIds]);
  const pickedVisible = useMemo(() => picked.filter((id) => visible.has(id)), [picked, visible]);

  return {
    active,
    enter,
    exit,
    selected,
    /** Selected ids that are still on screen, in the order they were picked. */
    picked: pickedVisible,
    toggle,
    selectAll,
    clear,
    allSelected: visibleIds.length > 0 && visibleIds.every((id) => selected.has(id)),
  };
}

export type Selection = ReturnType<typeof useSelection>;
