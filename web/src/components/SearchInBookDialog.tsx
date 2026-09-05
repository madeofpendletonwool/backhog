import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { Dialog } from "./ui/Dialog";
import { Gi } from "./ui/Gi";
import { LoadingDot } from "./ui/primitives";
import { useAudioPlayer } from "@/hooks/useAudioPlayer";
import { useDebounced } from "@/hooks/useLibrary";
import { ApiError, api } from "@/lib/api";
import { chapterTitle, formatPage } from "@/lib/booktext";
import { cn } from "@/lib/cn";
import { formatTimecode } from "@/lib/format";
import type { BookEntry, BookSearchHit } from "@/lib/types";

/**
 * Search inside one book.
 *
 * The shape is the command palette's, because the interaction is the same one:
 * type, arrow down, Enter. What is different is what a row *is*. A hit here is
 * a canonical character offset — the single position this app stores — so every
 * result already knows the page of the reader's own printing and the second of
 * the audiobook, and Enter can put them at either. That is the whole feature:
 * not finding the line, but arriving at it in whichever format is to hand.
 *
 * Nothing about the query leaves the box: the text is already parsed and on
 * disk, so the debounce is short and the results land while you are still
 * typing.
 */

/** Below this the server refuses, so the box says so instead of asking. */
const MIN_QUERY = 3;

export function SearchInBookDialog({
  entry,
  open,
  onClose,
}: {
  entry: BookEntry;
  open: boolean;
  onClose: () => void;
}) {
  const [term, setTerm] = useState("");
  const [highlighted, setHighlighted] = useState(0);
  const debounced = useDebounced(term, 150);
  const listRef = useRef<HTMLUListElement>(null);
  const navigate = useNavigate();
  const player = useAudioPlayer();

  const ready = debounced.trim().length >= MIN_QUERY;
  const { data, isFetching, error } = useQuery({
    queryKey: ["bookSearch", entry.id, debounced],
    queryFn: ({ signal }) => api.searchInBook(entry.id, debounced, signal),
    enabled: open && ready,
    // Superseded keystrokes must not blank the list on their way out; the
    // previous answer stays under the new one until it arrives.
    placeholderData: keepPreviousData,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });

  const results = useMemo<BookSearchHit[]>(() => data?.results ?? [], [data]);

  useEffect(() => setHighlighted(0), [debounced]);

  useEffect(() => {
    if (!open) {
      setTerm("");
      setHighlighted(0);
    }
  }, [open]);

  useEffect(() => {
    listRef.current
      ?.querySelector(`[data-index="${highlighted}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [highlighted]);

  /**
   * Opens the reader on the paragraph the hit landed in — as a peek. The
   * passage is shown without becoming "where you are": the reader holds
   * its stored position and offers the way back, because looking twenty
   * pages behind you is not reading there.
   */
  const read = (hit: BookSearchHit) => {
    onClose();
    navigate(`/books/${entry.id}/read?offset=${hit.char_offset}&peek=1`);
  };

  /** Starts the audiobook at the second the hit maps to. */
  const listen = (hit: BookSearchHit) => {
    if (!hit.audio) return;
    onClose();
    player.open(entry, { autoplay: true, startAt: hit.audio.seconds });
  };

  const onKeyDown = (event: React.KeyboardEvent) => {
    const hit = results[highlighted];
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setHighlighted((index) => Math.min(index + 1, results.length - 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setHighlighted((index) => Math.max(index - 1, 0));
    } else if (event.key === "Enter" && hit) {
      event.preventDefault();
      if (event.metaKey || event.ctrlKey) listen(hit);
      else read(hit);
    }
  };

  // A 422 is the server declining a query, not breaking on one.
  const refused = error instanceof ApiError && error.status === 422;

  return (
    <Dialog open={open} onClose={onClose} bare label="Search inside this book" className="max-w-2xl">
      <div className="panel overflow-hidden">
        <div className="flex items-center gap-3 border-b border-edge px-4">
          <Gi name="search" className="size-4 shrink-0 text-ink-500" />
          <input
            autoFocus
            value={term}
            onChange={(event) => setTerm(event.target.value)}
            onKeyDown={onKeyDown}
            placeholder="Search this book…"
            aria-label="Search inside this book"
            className="h-14 w-full bg-transparent text-[15px] text-ink-100 outline-none placeholder:text-ink-500"
          />
          {isFetching && <LoadingDot className="size-2.5 shrink-0 bg-ink-500" />}
        </div>

        {data?.mode === "loose" && results.length > 0 && (
          <p className="border-b border-edge bg-fill-hover px-4 py-2 text-[11px] text-ink-400">
            No exact match for those words — these are the closest passages.
          </p>
        )}

        <div className="max-h-[55vh] overflow-y-auto">
          {error && !refused ? (
            <Message title="Search failed" body={(error as Error).message} />
          ) : term.trim().length < MIN_QUERY ? (
            <Message
              title="Find the line you remember"
              body="Type a few words of it. Capitals, punctuation and curly quotes don't matter — the text is already folded, so type it however you remember it."
            />
          ) : results.length === 0 && !isFetching ? (
            <Message title="Nothing found" body={`No passage of this book matches "${term.trim()}".`} />
          ) : (
            <ul ref={listRef} className="p-2">
              {results.map((hit, index) => (
                <HitRow
                  key={`${hit.char_offset}-${hit.char_end}`}
                  hit={hit}
                  index={index}
                  highlighted={index === highlighted}
                  onHover={() => setHighlighted(index)}
                  onRead={() => read(hit)}
                  onListen={() => listen(hit)}
                />
              ))}
            </ul>
          )}
        </div>

        <div className="flex items-center justify-between gap-4 border-t border-edge px-4 py-2.5 text-[11px] text-ink-500">
          <span>
            <Kbd>↑</Kbd> <Kbd>↓</Kbd> navigate · <Kbd>↵</Kbd> peek here
            {results[highlighted]?.audio && (
              <>
                {" · "}
                <Kbd>⌘↵</Kbd> listen here
              </>
            )}{" "}
            · <Kbd>esc</Kbd> close
          </span>
          {data && data.total > 0 && (
            <span className="shrink-0">
              {data.truncated
                ? `showing ${results.length} of ${data.total}`
                : `${data.total} ${data.total === 1 ? "match" : "matches"}`}
            </span>
          )}
        </div>
      </div>
    </Dialog>
  );
}

/**
 * One passage. The snippet is the book's own prose — the server maps the match
 * back out of the folded text it searched — so the row reads like the page it
 * came from rather than like a grep.
 */
function HitRow({
  hit,
  index,
  highlighted,
  onHover,
  onRead,
  onListen,
}: {
  hit: BookSearchHit;
  index: number;
  highlighted: boolean;
  onHover: () => void;
  onRead: () => void;
  onListen: () => void;
}) {
  const page = formatPage(hit.page);

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
            <span className="truncate">
              {hit.chapter ? chapterTitle(hit.chapter) : "This book"}
            </span>
            <span aria-hidden>·</span>
            <span className="shrink-0">{Math.round(hit.percent)}%</span>
            {page && (
              <>
                <span aria-hidden>·</span>
                <span className="shrink-0">{page}</span>
              </>
            )}
            {hit.audio && (
              <>
                <span aria-hidden>·</span>
                <span className="inline-flex shrink-0 items-center gap-1">
                  <Gi name="headphones" className="size-3" />
                  {formatTimecode(hit.audio.seconds)}
                </span>
              </>
            )}
          </div>
          <p className="mt-1 text-sm leading-relaxed text-ink-400">
            {hit.context.before}
            <mark className="bg-brand-500/25 text-ink-100">{hit.context.passage}</mark>
            {hit.context.after}
          </p>
        </button>

        {hit.audio && (
          <button
            type="button"
            onClick={onListen}
            aria-label={`Listen from ${formatTimecode(hit.audio.seconds)}`}
            title="Listen from here"
            className="mt-0.5 shrink-0 rounded-lg p-2 text-ink-500 transition-colors hover:bg-fill-active hover:text-ink-100 focus-visible:focus-ring"
          >
            <Gi name="play" className="size-3.5" />
          </button>
        )}
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
