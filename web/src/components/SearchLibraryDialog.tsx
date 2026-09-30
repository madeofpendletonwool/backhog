import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { Dialog } from "./ui/Dialog";
import { Gi } from "./ui/Gi";
import { LoadingDot } from "./ui/primitives";
import { useDebounced } from "@/hooks/useLibrary";
import { ApiError, api } from "@/lib/api";
import { chapterTitle } from "@/lib/booktext";
import { cn } from "@/lib/cn";
import type { LibrarySearchHit } from "@/lib/types";

/**
 * Search inside every book on the shelf.
 *
 * The interaction is the in-book dialog's — type, arrow down, Enter —
 * because it is the same question one shelf larger: "where was that line?"
 * asked of every book at once instead of the one you happened to be in.
 * A row is the same object an in-book hit is (chapter, percent, the passage
 * as printed), plus the book it came from, and Enter peeks the reader to it
 * without ever moving a saved position.
 *
 * The default clamp is the reader's own positions: what has not been read
 * does not answer, and books that matched only past where they are say so
 * as titles alone. The toggle lifts it, out loud, per search.
 */

/** Below this the server refuses, so the box says so instead of asking. */
const MIN_QUERY = 3;

export function SearchLibraryDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const [term, setTerm] = useState("");
  const [spoilers, setSpoilers] = useState(false);
  const [highlighted, setHighlighted] = useState(0);
  const debounced = useDebounced(term, 250);
  const listRef = useRef<HTMLUListElement>(null);
  const navigate = useNavigate();

  const ready = debounced.trim().length >= MIN_QUERY;
  const { data, isFetching, error } = useQuery({
    queryKey: ["librarySearch", debounced, spoilers],
    queryFn: ({ signal }) =>
      api.searchLibrary(debounced, { untilNone: spoilers, signal }),
    enabled: open && ready,
    placeholderData: keepPreviousData,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });

  const results = useMemo(() => data?.results ?? [], [data]);
  const beyond = useMemo(() => data?.matched_beyond ?? [], [data]);
  const rows = results.length + beyond.length;

  useEffect(() => setHighlighted(0), [debounced]);

  useEffect(() => {
    if (!open) {
      setTerm("");
      setSpoilers(false);
      setHighlighted(0);
    }
  }, [open]);

  useEffect(() => {
    listRef.current
      ?.querySelector(`[data-index="${highlighted}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [highlighted]);

  /** Opens the reader where the hit landed — as a peek, never a move. */
  const read = (hit: LibrarySearchHit) => {
    onClose();
    navigate(`/books/${hit.book_id}/read?offset=${hit.char_start}&peek=1`);
  };

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setHighlighted((index) => (rows === 0 ? 0 : Math.min(index + 1, rows - 1)));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setHighlighted((index) => Math.max(index - 1, 0));
    } else if (event.key === "Enter") {
      event.preventDefault();
      if (results[highlighted]) read(results[highlighted]);
    }
  };

  // A 422 is the server declining a query, not breaking on one — its words
  // are the honest empty state.
  const refused = error instanceof ApiError && error.status === 422;

  return (
    <Dialog open={open} onClose={onClose} bare label="Search inside your books" className="max-w-2xl">
      <div className="panel overflow-hidden">
        <div className="flex items-center gap-3 border-b border-edge px-4">
          <Gi name="search" className="size-4 shrink-0 text-ink-500" />
          <input
            autoFocus
            value={term}
            onChange={(event) => setTerm(event.target.value)}
            onKeyDown={onKeyDown}
            placeholder="Search inside your books…"
            aria-label="Search inside your books"
            className="h-14 w-full bg-transparent text-[15px] text-ink-100 outline-none placeholder:text-ink-500"
          />
          {isFetching && <LoadingDot className="size-2.5 shrink-0 bg-ink-500" />}
        </div>

        {data?.mode === "loose" && results.length > 0 && (
          <p className="border-b border-edge bg-fill-hover px-4 py-2 text-[11px] text-ink-400">
            No book contains those words in a row — these are the closest passages.
          </p>
        )}

        <div className="max-h-[55vh] overflow-y-auto">
          {error && !refused ? (
            <Message title="Search failed" body={(error as Error).message} />
          ) : refused ? (
            <Message title="Not enough to search on" body={(error as Error).message} />
          ) : term.trim().length < MIN_QUERY ? (
            <Message
              title="Find the line you remember"
              body="Type a few words of it. Every book you can read is searched at once — capitals, punctuation and curly quotes don't matter."
            />
          ) : rows === 0 && !isFetching ? (
            <Message title="Nothing found" body={`No book on your shelf matches "${term.trim()}".`} />
          ) : (
            <ul ref={listRef} className="p-2">
              {results.map((hit, index) => (
                <HitRow
                  key={`${hit.book_id}-${hit.char_start}-${hit.char_end}`}
                  hit={hit}
                  index={index}
                  highlighted={index === highlighted}
                  onHover={() => setHighlighted(index)}
                  onRead={() => read(hit)}
                />
              ))}
              {beyond.length > 0 && (
                <li className="px-2 pb-1 pt-3 text-[11px] font-semibold uppercase tracking-wider text-ink-500">
                  Matched, beyond where you’ve read
                </li>
              )}
              {beyond.map((book, index) => (
                <li key={book.book_id} data-index={results.length + index}>
                  <div
                    onMouseMove={() => setHighlighted(results.length + index)}
                    className={cn(
                      "flex items-center gap-3 rounded-xl p-2.5 transition-colors",
                      results.length + index === highlighted
                        ? "bg-fill-active"
                        : "hover:bg-fill-hover",
                    )}
                  >
                    <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-fill-hover text-ink-500">
                      <Gi name="book-pile" className="size-4" />
                    </span>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm text-ink-200">{book.title}</p>
                      <p className="text-[11px] text-ink-500">
                        Contains it somewhere you haven’t read yet — search again with spoilers on to
                        see where.
                      </p>
                    </div>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>

        <div className="flex items-center justify-between gap-4 border-t border-edge px-4 py-2.5 text-[11px] text-ink-500">
          <span>
            <Kbd>↑</Kbd> <Kbd>↓</Kbd> navigate · <Kbd>↵</Kbd> peek here · <Kbd>esc</Kbd> close
          </span>
          <span className="flex shrink-0 items-center gap-3">
            {data && data.total > 0 && (
              <span>
                {data.truncated
                  ? `showing ${results.length} of ${data.total}`
                  : `${data.total} ${data.total === 1 ? "match" : "matches"}`}
              </span>
            )}
            <button
              type="button"
              onClick={() => setSpoilers((on) => !on)}
              aria-pressed={spoilers}
              title="Include text you haven't read yet"
              className={cn(
                "rounded-lg border px-2 py-1 transition-colors",
                spoilers
                  ? "border-brand-500/50 bg-brand-500/15 text-ink-100"
                  : "border-edge text-ink-400 hover:text-ink-200",
              )}
            >
              spoilers
            </button>
          </span>
        </div>
      </div>
    </Dialog>
  );
}

/**
 * One passage, in its book. The snippet is the book's own prose and the
 * row names the book and chapter it came from — everything an in-book hit
 * row shows, plus which shelf neighbour was holding it.
 */
function HitRow({
  hit,
  index,
  highlighted,
  onHover,
  onRead,
}: {
  hit: LibrarySearchHit;
  index: number;
  highlighted: boolean;
  onHover: () => void;
  onRead: () => void;
}) {
  return (
    <li data-index={index}>
      <div
        onMouseMove={onHover}
        className={cn(
          "flex items-start gap-3 rounded-xl p-2.5 transition-colors",
          highlighted ? "bg-fill-active" : "hover:bg-fill-hover",
        )}
      >
        <button type="button" onClick={onRead} className="min-w-0 flex-1 text-left">
          <div className="flex items-center gap-2 text-[11px] text-ink-500">
            <span className="truncate font-medium text-ink-400">{hit.title}</span>
            <span aria-hidden>·</span>
            <span className="truncate">
              {hit.chapter ? chapterTitle(hit.chapter) : "This book"}
            </span>
            <span aria-hidden>·</span>
            <span className="shrink-0">{Math.round(hit.percent)}%</span>
          </div>
          <p className="mt-1 text-sm leading-relaxed text-ink-400">
            {hit.snippet.before}
            <mark className="bg-brand-500/25 text-ink-100">{hit.snippet.passage}</mark>
            {hit.snippet.after}
          </p>
        </button>
      </div>
    </li>
  );
}

function Message({ title, body }: { title: string; body: string }) {
  return (
    <div className="px-6 py-12 text-center">
      <p className="text-sm font-medium text-ink-200">{title}</p>
      <p className="mx-auto mt-1.5 max-w-sm text-xs leading-relaxed text-ink-500">{body}</p>
    </div>
  );
}

function Kbd({ children }: { children: React.ReactNode }) {
  return (
    <kbd className="rounded border border-edge-strong bg-ink-800 px-1.5 py-0.5 font-sans text-[10px] text-ink-400">
      {children}
    </kbd>
  );
}
