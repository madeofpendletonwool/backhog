import { useQuery } from "@tanstack/react-query";
import { cn } from "@/lib/cn";
import { Check, Clock, Loader2, Plus, Search, Star } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

import { Dialog } from "./ui/Dialog";
import { GameCover } from "./GameCover";
import { ApiError, api } from "@/lib/api";
import { accentStyle, formatDuration, releaseYear } from "@/lib/format";
import { useAddToLibrary, useDebounced } from "@/hooks/useLibrary";
import type { SearchResult } from "@/lib/types";

/**
 * Command-palette style game search. Typing searches IGDB; Enter adds the
 * highlighted game to the backlog without leaving the keyboard.
 */
export function AddGameDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [term, setTerm] = useState("");
  const [highlighted, setHighlighted] = useState(0);
  const debounced = useDebounced(term, 300);
  const listRef = useRef<HTMLUListElement>(null);
  const add = useAddToLibrary();

  const { data, isFetching, error } = useQuery({
    queryKey: ["search", debounced],
    queryFn: ({ signal }) => api.searchGames(debounced, signal),
    enabled: debounced.trim().length >= 2,
    staleTime: 5 * 60 * 1000,
  });

  const results = useMemo<SearchResult[]>(() => data?.results ?? [], [data]);

  // Reset the cursor whenever the result set changes underneath it.
  useEffect(() => setHighlighted(0), [debounced]);

  useEffect(() => {
    if (!open) {
      setTerm("");
      setHighlighted(0);
      add.reset();
    }
    // `add` is a stable mutation object; re-running on it would loop.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // Keep the highlighted row in view during arrow-key navigation.
  useEffect(() => {
    listRef.current
      ?.querySelector(`[data-index="${highlighted}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [highlighted]);

  const addGame = (result: SearchResult) => {
    if (result.in_library) return;
    add.mutate({ gameId: result.game.id });
  };

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setHighlighted((index) => Math.min(index + 1, results.length - 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setHighlighted((index) => Math.max(index - 1, 0));
    } else if (event.key === "Enter" && results[highlighted]) {
      event.preventDefault();
      addGame(results[highlighted]);
    }
  };

  const searchUnavailable = error instanceof ApiError && error.status === 503;

  return (
    <Dialog open={open} onClose={onClose} bare label="Add a game" className="max-w-2xl">
      <div className="panel overflow-hidden">
        <div className="flex items-center gap-3 border-b border-white/[0.06] px-4">
          <Search className="size-4 shrink-0 text-ink-500" />
          <input
            autoFocus
            value={term}
            onChange={(event) => setTerm(event.target.value)}
            onKeyDown={onKeyDown}
            placeholder="Search for a game…"
            aria-label="Search for a game"
            className="h-14 w-full bg-transparent text-[15px] text-ink-100 outline-none placeholder:text-ink-500"
          />
          {isFetching && <Loader2 className="size-4 shrink-0 animate-spin text-ink-500" />}
        </div>

        <div className="max-h-[55vh] overflow-y-auto">
          {searchUnavailable ? (
            <Message
              title="Game search isn't configured"
              body="Set IGDB_CLIENT_ID and IGDB_CLIENT_SECRET in your .env file, then restart the stack."
            />
          ) : error ? (
            <Message title="Search failed" body={(error as Error).message} />
          ) : term.trim().length < 2 ? (
            <Message
              title="Find something to play"
              body="Type at least two characters. Use ↑ ↓ to move and Enter to add to your backlog."
            />
          ) : results.length === 0 && !isFetching ? (
            <Message title="No matches" body={`Nothing found for "${term}".`} />
          ) : (
            <ul ref={listRef} className="p-2">
              {results.map((result, index) => (
                <ResultRow
                  key={result.game.id}
                  result={result}
                  index={index}
                  highlighted={index === highlighted}
                  pending={add.isPending && add.variables?.gameId === result.game.id}
                  justAdded={add.isSuccess && add.variables?.gameId === result.game.id}
                  onHover={() => setHighlighted(index)}
                  onSelect={() => addGame(result)}
                />
              ))}
            </ul>
          )}
        </div>

        <div className="flex items-center justify-between border-t border-white/[0.06] px-4 py-2.5 text-[11px] text-ink-500">
          <span>
            <Kbd>↑</Kbd> <Kbd>↓</Kbd> navigate · <Kbd>↵</Kbd> add · <Kbd>esc</Kbd> close
          </span>
          {add.isError && <span className="text-red-400">{(add.error as Error).message}</span>}
        </div>
      </div>
    </Dialog>
  );
}

function ResultRow({
  result,
  index,
  highlighted,
  pending,
  justAdded,
  onHover,
  onSelect,
}: {
  result: SearchResult;
  index: number;
  highlighted: boolean;
  pending: boolean;
  justAdded: boolean;
  onHover: () => void;
  onSelect: () => void;
}) {
  const { game, in_library } = result;
  const owned = in_library || justAdded;
  const year = releaseYear(game);

  return (
    <li data-index={index}>
      <button
        type="button"
        onMouseMove={onHover}
        onClick={onSelect}
        disabled={owned || pending}
        style={accentStyle(game)}
        className={cn(
          "flex w-full items-center gap-3 rounded-xl p-2 text-left transition-colors",
          highlighted ? "bg-white/[0.07]" : "hover:bg-white/[0.04]",
          owned && "cursor-default",
        )}
      >
        <GameCover game={game} className="w-11 shrink-0 rounded-lg" />

        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium text-ink-100">{game.name}</p>
          <div className="mt-0.5 flex items-center gap-2.5 text-[11px] text-ink-400">
            {year && <span>{year}</span>}
            {game.igdb_rating != null && (
              <span className="inline-flex items-center gap-1">
                <Star className="size-3" />
                {Math.round(game.igdb_rating)}
              </span>
            )}
            {game.time_to_beat_main && (
              <span className="inline-flex items-center gap-1">
                <Clock className="size-3" />
                {formatDuration(game.time_to_beat_main)}
              </span>
            )}
            {game.genres.length > 0 && (
              <span className="truncate">{game.genres.slice(0, 2).map((g) => g.name).join(", ")}</span>
            )}
          </div>
        </div>

        <span
          className={cn(
            "flex shrink-0 items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-xs font-medium",
            owned
              ? "text-emerald-400"
              : highlighted
                ? "bg-brand-600 text-white"
                : "text-ink-500",
          )}
        >
          {pending ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : owned ? (
            <>
              <Check className="size-3.5" /> In library
            </>
          ) : (
            <>
              <Plus className="size-3.5" /> Add
            </>
          )}
        </span>
      </button>
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
    <kbd className="rounded border border-white/10 bg-ink-800 px-1.5 py-0.5 font-sans text-[10px] text-ink-400">
      {children}
    </kbd>
  );
}
