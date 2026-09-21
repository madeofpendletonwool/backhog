import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { EntryCover } from "./EntryCover";
import { StatusBadge } from "./StatusBadge";
import { Dialog } from "./ui/Dialog";
import { Gi } from "./ui/Gi";
import { LoadingDot } from "./ui/primitives";
import { useContinueReading } from "@/hooks/useContinueReading";
import { useDebounced } from "@/hooks/useLibrary";
import { api } from "@/lib/api";
import type { Arena } from "@/lib/arena";
import { cn } from "@/lib/cn";
import { entryAccent, entryHref, entrySubtitle, entryTitle } from "@/lib/entry";
import { accentStyle } from "@/lib/format";
import { isBookEntry, type Entry } from "@/lib/types";

/**
 * ⌘K: jump to something you own.
 *
 * The palette that ⌘K means everywhere else. Type a title and go — and for
 * a book you are in the middle of, "go" means the page you stopped on, not
 * the dossier about it. With nothing typed it shows what you touched last,
 * so the shortcut alone is a "continue" button.
 *
 * Adding is the last row rather than a separate shortcut: the search that
 * found nothing on your shelf is the search you want to run against the
 * provider, so the typed text carries over into the add dialog.
 */
export function JumpDialog({
  open,
  onClose,
  arena,
  onAdd,
}: {
  open: boolean;
  onClose: () => void;
  arena: Arena;
  /** Open the add dialog, seeded with what was typed. */
  onAdd: (query: string) => void;
}) {
  const navigate = useNavigate();
  const continueReading = useContinueReading();
  const [term, setTerm] = useState("");
  const [highlighted, setHighlighted] = useState(0);
  const listRef = useRef<HTMLUListElement>(null);
  const debounced = useDebounced(term, 150);
  const media = arena === "books" ? "book" : "game";

  useEffect(() => {
    if (!open) return;
    setTerm("");
    setHighlighted(0);
  }, [open]);

  const { data, isFetching } = useQuery({
    queryKey: ["jump", media, debounced],
    queryFn: () => api.library({ media, q: debounced, sort: "updated", limit: 8 }),
    enabled: open,
    placeholderData: (previous) => previous,
  });
  const entries = data?.entries ?? [];
  // The add row exists once there is something to add; a blank palette is
  // a "continue" list, not a prompt to type a title.
  const rows = entries.length + (term.trim() ? 1 : 0);

  useEffect(() => {
    setHighlighted(0);
  }, [debounced]);

  const noun = arena === "books" ? "book" : "game";

  const go = (entry: Entry) => {
    onClose();
    // A book with a stored position resumes where it was; everything else
    // opens on its own page.
    if (isBookEntry(entry) && entry.status === "playing" && (entry.progress_percent ?? 0) > 0) {
      continueReading(entry);
      return;
    }
    navigate(entryHref(entry));
  };

  const choose = (index: number) => {
    if (index < entries.length) go(entries[index]);
    else if (term.trim()) {
      onClose();
      onAdd(term.trim());
    }
  };

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setHighlighted((index) => (rows === 0 ? 0 : (index + 1) % rows));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setHighlighted((index) => (rows === 0 ? 0 : (index - 1 + rows) % rows));
    } else if (event.key === "Enter") {
      event.preventDefault();
      choose(highlighted);
    }
  };

  useEffect(() => {
    listRef.current
      ?.querySelector<HTMLElement>(`[data-index="${highlighted}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [highlighted]);

  return (
    <Dialog open={open} onClose={onClose} bare label={`Find a ${noun}`} className="max-w-xl">
      <div className="panel overflow-hidden">
        <div className="flex items-center gap-3 border-b border-edge px-4">
          <Gi name="search" className="size-4 shrink-0 text-ink-500" />
          <input
            autoFocus
            value={term}
            onChange={(event) => setTerm(event.target.value)}
            onKeyDown={onKeyDown}
            placeholder={arena === "books" ? "Jump to a book…" : "Jump to a game…"}
            aria-label={`Find a ${noun}`}
            role="combobox"
            aria-expanded={rows > 0}
            aria-controls="jump-results"
            aria-activedescendant={rows > 0 ? `jump-option-${highlighted}` : undefined}
            className="h-14 w-full bg-transparent text-[15px] text-ink-100 outline-none placeholder:text-ink-500"
          />
          {isFetching ? (
            <LoadingDot className="size-2.5 shrink-0 bg-ink-500" />
          ) : (
            <kbd className="shrink-0 rounded border border-edge px-1.5 py-0.5 text-[10px] text-ink-500">
              esc
            </kbd>
          )}
        </div>

        <ul
          ref={listRef}
          id="jump-results"
          role="listbox"
          className="max-h-[55vh] overflow-y-auto py-1.5"
        >
          {!term.trim() && entries.length > 0 && (
            <li className="px-4 pb-1 pt-1.5 text-[11px] font-semibold uppercase tracking-wider text-ink-500">
              Recent
            </li>
          )}
          {entries.map((entry, index) => {
            const book = isBookEntry(entry);
            const percent = book ? Math.round(entry.progress_percent ?? 0) : 0;
            const resumes = book && entry.status === "playing" && percent > 0;
            return (
              <li
                key={entry.id}
                id={`jump-option-${index}`}
                data-index={index}
                role="option"
                aria-selected={highlighted === index}
                onMouseEnter={() => setHighlighted(index)}
                onClick={() => choose(index)}
                style={accentStyle(entryAccent(entry))}
                className={cn(
                  "mx-1.5 flex cursor-pointer items-center gap-3 rounded-lg px-2.5 py-2 transition-colors",
                  highlighted === index ? "bg-fill-active" : "hover:bg-fill-hover",
                )}
              >
                <div className={cn("shrink-0 overflow-hidden rounded-md ring-1 ring-art", book ? "w-8" : "w-9")}>
                  <EntryCover entry={entry} sizes="40px" />
                </div>
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium text-ink-100">{entryTitle(entry)}</p>
                  <p className="truncate text-xs text-ink-500">
                    {entrySubtitle(entry)}
                    {resumes && <span className="text-ink-400"> · {percent}% read</span>}
                  </p>
                </div>
                <StatusBadge status={entry.status} media={entry.media_type} className="shrink-0" />
                {highlighted === index && (
                  <span className="hidden shrink-0 text-[11px] text-ink-500 sm:inline">
                    {resumes ? "↵ continue" : "↵ open"}
                  </span>
                )}
              </li>
            );
          })}

          {term.trim() && (
            <li
              id={`jump-option-${entries.length}`}
              data-index={entries.length}
              role="option"
              aria-selected={highlighted === entries.length}
              onMouseEnter={() => setHighlighted(entries.length)}
              onClick={() => choose(entries.length)}
              className={cn(
                "mx-1.5 flex cursor-pointer items-center gap-3 rounded-lg px-2.5 py-2.5 transition-colors",
                entries.length > 0 && "mt-1.5 border-t border-edge pt-3",
                highlighted === entries.length ? "bg-fill-active" : "hover:bg-fill-hover",
              )}
            >
              <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-fill-hover text-hl-bright">
                <Gi name="plus" className="size-4" />
              </span>
              <p className="min-w-0 flex-1 truncate text-sm text-ink-200">
                Add <span className="font-medium text-ink-100">“{term.trim()}”</span> to your{" "}
                {arena === "books" ? "shelf" : "library"}…
              </p>
              {highlighted === entries.length && (
                <span className="hidden shrink-0 text-[11px] text-ink-500 sm:inline">↵ add</span>
              )}
            </li>
          )}

          {rows === 0 && !isFetching && (
            <li className="px-4 py-6 text-center text-sm text-ink-500">
              Nothing here yet. Type a title to find or add a {noun}.
            </li>
          )}
        </ul>
      </div>
    </Dialog>
  );
}
