import { useMutation, useQuery } from "@tanstack/react-query";
import { Check, Download, Loader2 } from "lucide-react";
import { useState } from "react";

import { Button, Input, Select } from "./ui/primitives";
import { Dialog } from "./ui/Dialog";
import { api } from "@/lib/api";
import { useQueryClient } from "@tanstack/react-query";
import type { Status, SteamMatch } from "@/lib/types";

/**
 * Bulk-import an owned Steam library.
 *
 * Steam appids are mapped to IGDB through IGDB's external_games table, which is
 * an exact join rather than a name guess — name matching mangles things like
 * "Prey" (2006 vs 2017) and any title with unusual punctuation.
 */
export function SteamImportDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const queryClient = useQueryClient();
  const [steamId, setSteamId] = useState("");
  const [status, setStatus] = useState<Status>("backlog");
  const [selected, setSelected] = useState<Set<number>>(new Set());

  const { data: health } = useQuery({ queryKey: ["health"], queryFn: api.health });

  const preview = useMutation({
    mutationFn: () => api.steamPreview(steamId.trim()),
    onSuccess: (data) => {
      // Preselect everything importable; deselecting a few is less work than
      // ticking two hundred boxes.
      setSelected(
        new Set(
          data.matches
            .filter((m) => m.game && !m.in_library)
            .map((m) => m.game!.id),
        ),
      );
    },
  });

  const runImport = useMutation({
    mutationFn: () => api.bulkAdd([...selected], status),
    onSuccess: () => {
      for (const key of ["library", "stats", "queue", "lists", "facets"]) {
        queryClient.invalidateQueries({ queryKey: [key] });
      }
    },
  });

  const close = () => {
    setSteamId("");
    setSelected(new Set());
    preview.reset();
    runImport.reset();
    onClose();
  };

  const matches = preview.data?.matches ?? [];
  const importable = matches.filter((m) => m.game && !m.in_library);

  const toggle = (gameId: number) => {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(gameId)) next.delete(gameId);
      else next.add(gameId);
      return next;
    });
  };

  if (health && !health.steam) {
    return (
      <Dialog open={open} onClose={close} label="Steam import" className="max-w-md">
        <h2 className="text-lg font-semibold text-ink-100">Steam import isn't configured</h2>
        <p className="mt-2 text-sm leading-relaxed text-ink-400">
          Add a Steam Web API key to your <code className="text-ink-300">.env</code> as{" "}
          <code className="text-ink-300">STEAM_API_KEY</code> and restart the stack. You can get one
          free at{" "}
          <a
            href="https://steamcommunity.com/dev/apikey"
            target="_blank"
            rel="noreferrer noopener"
            className="text-brand-400 hover:text-brand-300"
          >
            steamcommunity.com/dev/apikey
          </a>
          .
        </p>
        <div className="mt-6 flex justify-end">
          <Button onClick={close}>Close</Button>
        </div>
      </Dialog>
    );
  }

  return (
    <Dialog open={open} onClose={close} label="Import from Steam" className="max-w-2xl">
      <h2 className="text-lg font-semibold text-ink-100">Import from Steam</h2>
      <p className="mt-1 text-sm text-ink-400">
        Pull in the games you own. Your Steam profile's game details must be public.
      </p>

      {runImport.isSuccess ? (
        <div className="py-10 text-center">
          <div className="mx-auto mb-4 flex size-14 items-center justify-center rounded-2xl bg-emerald-500/15 text-emerald-300">
            <Check className="size-7" />
          </div>
          <p className="text-sm font-medium text-ink-100">
            Added {runImport.data.added} game{runImport.data.added === 1 ? "" : "s"}
          </p>
          {runImport.data.skipped > 0 && (
            <p className="mt-1 text-xs text-ink-500">
              {runImport.data.skipped} skipped (already in your library)
            </p>
          )}
          <Button variant="primary" className="mt-6" onClick={close}>
            Done
          </Button>
        </div>
      ) : (
        <>
          <div className="mt-5 flex gap-2">
            <Input
              value={steamId}
              onChange={(event) => setSteamId(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && steamId.trim()) preview.mutate();
              }}
              placeholder="SteamID64, vanity name, or profile URL"
              aria-label="Steam profile"
              autoFocus
            />
            <Button
              variant="secondary"
              loading={preview.isPending}
              disabled={!steamId.trim()}
              onClick={() => preview.mutate()}
            >
              Look up
            </Button>
          </div>

          {preview.isError && (
            <p role="alert" className="mt-3 rounded-xl bg-red-500/10 px-3 py-2 text-sm text-red-300">
              {(preview.error as Error).message}
            </p>
          )}

          {preview.data && (
            <>
              <div className="mt-4 flex flex-wrap items-center gap-3 text-xs text-ink-400">
                <span>
                  <span className="text-ink-200">{preview.data.total}</span> games on Steam
                </span>
                <span>
                  <span className="text-ink-200">{importable.length}</span> new
                </span>
                {preview.data.unmatched > 0 && (
                  <span className="text-amber-300/80">
                    {preview.data.unmatched} not found on IGDB
                  </span>
                )}
                <button
                  onClick={() =>
                    setSelected(
                      selected.size === importable.length
                        ? new Set()
                        : new Set(importable.map((m) => m.game!.id)),
                    )
                  }
                  className="ml-auto text-brand-400 hover:text-brand-300"
                >
                  {selected.size === importable.length ? "Deselect all" : "Select all"}
                </button>
              </div>

              <div className="mt-3 max-h-[45vh] overflow-y-auto rounded-xl border border-white/[0.06]">
                {matches.map((match) => (
                  <MatchRow
                    key={match.app_id}
                    match={match}
                    checked={match.game ? selected.has(match.game.id) : false}
                    onToggle={() => match.game && toggle(match.game.id)}
                  />
                ))}
              </div>

              <div className="mt-4 flex items-center gap-2">
                <span className="text-xs text-ink-400">Import as</span>
                <Select
                  value={status}
                  onChange={(event) => setStatus(event.target.value as Status)}
                  className="h-9 w-auto text-xs"
                >
                  <option value="backlog">Backlog</option>
                  <option value="played">Played</option>
                  <option value="wishlist">Wishlist</option>
                </Select>

                <div className="ml-auto flex gap-2">
                  <Button variant="ghost" onClick={close}>
                    Cancel
                  </Button>
                  <Button
                    variant="primary"
                    loading={runImport.isPending}
                    disabled={selected.size === 0}
                    onClick={() => runImport.mutate()}
                  >
                    <Download className="size-4" />
                    Import {selected.size}
                  </Button>
                </div>
              </div>

              {runImport.isError && (
                <p role="alert" className="mt-3 text-sm text-red-300">
                  {(runImport.error as Error).message}
                </p>
              )}
            </>
          )}

          {preview.isPending && (
            <div className="flex items-center justify-center gap-2 py-12 text-sm text-ink-500">
              <Loader2 className="size-4 animate-spin" />
              Reading your Steam library and matching against IGDB…
            </div>
          )}
        </>
      )}
    </Dialog>
  );
}

function MatchRow({
  match,
  checked,
  onToggle,
}: {
  match: SteamMatch;
  checked: boolean;
  onToggle: () => void;
}) {
  const unmatched = !match.game;

  return (
    <label
      className={
        unmatched || match.in_library
          ? "flex items-center gap-3 border-b border-white/[0.04] px-3 py-2 text-sm last:border-0 opacity-50"
          : "flex cursor-pointer items-center gap-3 border-b border-white/[0.04] px-3 py-2 text-sm transition-colors last:border-0 hover:bg-white/[0.04]"
      }
    >
      <input
        type="checkbox"
        checked={checked}
        disabled={unmatched || match.in_library}
        onChange={onToggle}
        className="size-4 shrink-0 accent-brand-500"
      />
      <span className="min-w-0 flex-1 truncate text-ink-200">{match.steam_name}</span>
      {match.in_library ? (
        <span className="shrink-0 text-xs text-emerald-400">In library</span>
      ) : unmatched ? (
        <span className="shrink-0 text-xs text-ink-600">No IGDB match</span>
      ) : (
        match.game!.name !== match.steam_name && (
          <span className="hidden shrink-0 truncate text-xs text-ink-500 sm:block">
            → {match.game!.name}
          </span>
        )
      )}
    </label>
  );
}
