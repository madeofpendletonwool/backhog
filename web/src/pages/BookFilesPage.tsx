import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";

import { Dialog } from "@/components/ui/Dialog";
import { Gi } from "@/components/ui/Gi";
import { Button, EmptyState, Input, Panel, Select, Spinner } from "@/components/ui/primitives";
import { api, ApiError } from "@/lib/api";
import { cn } from "@/lib/cn";
import { byline, formatDuration } from "@/lib/format";
import type { Book, MediaCandidate, MediaSuggestion } from "@/lib/types";

/**
 * The attach review queue. The scanner knows what files exist, the matcher
 * proposes which book each one is, and this page is where a human says yes:
 * confirm the suggestion, pick a different book, or ignore the files — with
 * a bulk action for the high-confidence pile, because a first run against a
 * real NAS has hundreds of these.
 */
export function BookFilesPage() {
  const queryClient = useQueryClient();
  const [pickFor, setPickFor] = useState<MediaCandidate | null>(null);
  const [busyKeys, setBusyKeys] = useState<Set<string>>(new Set());
  const [status, setStatus] = useState<{ kind: "ok" | "error"; message: string } | null>(null);
  const [showSkipped, setShowSkipped] = useState(false);
  const [kind, setKind] = useState<"" | "audio" | "epub">("");

  const scan = useQuery({ queryKey: ["media", "scan"], queryFn: api.mediaScanStatus });
  const queue = useQuery({
    queryKey: ["media", "candidates"],
    queryFn: api.mediaCandidates,
  });

  // While a scan runs, poll both the progress counters and the queue it is
  // filling — the review list should grow as the walk discovers files.
  useEffect(() => {
    if (!scan.data?.running) return;
    const timer = setInterval(() => {
      scan.refetch();
      queue.refetch();
    }, 1500);
    return () => clearInterval(timer);
  }, [scan.data?.running, scan, queue]);

  // The matcher fills suggestions in at the provider's pace, in the
  // background. While any candidate sits unmatched, keep peeking so
  // matches surface without a manual reload — and stop once they're all
  // decided or the review queue empties out.
  const hasUnmatched = (queue.data?.candidates ?? []).some(
    (c) => (c.suggestions ?? []).length === 0,
  );
  useEffect(() => {
    if (!hasUnmatched) return;
    const timer = setInterval(() => queue.refetch(), 10_000);
    return () => clearInterval(timer);
  }, [hasUnmatched, queue]);

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ["media"] });
    queryClient.invalidateQueries({ queryKey: ["library"] });
  };

  const markBusy = (key: string, on: boolean) =>
    setBusyKeys((prev) => {
      const next = new Set(prev);
      if (on) next.add(key);
      else next.delete(key);
      return next;
    });

  const confirm = useMutation({
    mutationFn: async ({ candidate, suggestion }: { candidate: MediaCandidate; suggestion: MediaSuggestion }) => {
      // The attach API is entry-keyed. If the user doesn't own the book
      // yet, adding it to the library produces the entry to attach to.
      let entryId = suggestion.entry_id;
      if (!entryId) {
        const entry = await api.addBookToLibrary(suggestion.book.id);
        entryId = entry.id;
      }
      return api.attachFiles(
        entryId,
        candidate.files.map((f) => f.id),
        candidate.kind,
      );
    },
    onSuccess: (_data, { candidate }) => {
      markBusy(candidate.key, false);
      invalidate();
    },
    onError: (error, { candidate }) => {
      markBusy(candidate.key, false);
      const message =
        error instanceof ApiError && error.status === 409
          ? "One of these files is already attached to another book — detach it there first."
          : (error as Error).message;
      setStatus({ kind: "error", message });
    },
  });

  const ignore = useMutation({
    mutationFn: (candidate: MediaCandidate) =>
      api.ignoreMediaFiles(candidate.files.map((f) => f.id)),
    onSuccess: invalidate,
  });

  const bulkConfirm = async () => {
    const bulkable = (queue.data?.candidates ?? []).filter(
      (c) => c.high_confidence && c.suggestions?.[0],
    );
    setStatus(null);
    for (const candidate of bulkable) {
      markBusy(candidate.key, true);
      try {
        const suggestion = candidate.suggestions?.[0];
        if (!suggestion) continue;
        let entryId = suggestion.entry_id;
        if (!entryId) {
          const entry = await api.addBookToLibrary(suggestion.book.id);
          entryId = entry.id;
        }
        await api.attachFiles(
          entryId,
          candidate.files.map((f) => f.id),
          candidate.kind,
        );
      } catch (error) {
        if (error instanceof ApiError && error.status === 409) continue;
        setStatus({
          kind: "error",
          message: `Bulk confirm stopped at "${candidate.title_guess || candidate.dir_path}": ${(error as Error).message}`,
        });
        markBusy(candidate.key, false);
        break;
      }
    }
    invalidate();
  };

  const allCandidates = queue.data?.candidates ?? [];
  const candidates = kind ? allCandidates.filter((c) => c.kind === kind) : allCandidates;
  const skipped = queue.data?.skipped ?? [];
  const bulkableCount = candidates.filter((c) => c.high_confidence && c.suggestions?.[0]).length;

  return (
    <div className="mx-auto max-w-4xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <header className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-ink-100">Book files</h1>
          <p className="mt-1 text-sm text-ink-400">
            Scanned NAS files, matched to books. Confirm the good guesses, fix the rest.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Select
            aria-label="Filter by file type"
            value={kind}
            onChange={(event) => setKind(event.target.value as "" | "audio" | "epub")}
            className="w-auto"
          >
            <option value="">All types</option>
            <option value="audio">Audio</option>
            <option value="epub">Ebooks</option>
          </Select>
          {scan.data?.running && (
            <span className="flex items-center gap-2 text-xs text-ink-400">
              <Spinner />
              <span className="font-display uppercase tracking-wider">
                Scanning {scan.data.found} found
              </span>
            </span>
          )}
          <Button
            onClick={async () => {
              const res = await api.kickMediaScan();
              if (res.started) scan.refetch();
            }}
            disabled={scan.data?.running}
          >
            <Gi name="refresh" className="size-4" />
            Scan now
          </Button>
        </div>
      </header>

      {status && (
        <p
          role="alert"
          className={cn(
            "mb-4 px-3 py-2 text-sm",
            status.kind === "ok"
              ? "rounded-xl bg-emerald-500/10 text-emerald-300"
              : "rounded-xl bg-red-500/10 text-red-300",
          )}
        >
          {status.message}
        </p>
      )}

      {queue.isLoading ? (
        <div className="flex justify-center py-20">
          <Spinner className="size-6" />
        </div>
      ) : candidates.length === 0 ? (
        allCandidates.length > 0 ? (
          <EmptyState
            icon={<Gi name={kind === "audio" ? "headphones" : "scroll-unfurled"} className="size-8" />}
            title={`No ${kind === "audio" ? "audiobooks" : "ebooks"} to review`}
            description="Every candidate of this type is attached or decided. Clear the filter to see the rest of the queue."
            action={
              <Button variant="ghost" onClick={() => setKind("")}>
                Show all types
              </Button>
            }
          />
        ) : (
        <EmptyState
          icon={<Gi name="book-pile" className="size-8" />}
          title={queue.data ? "Nothing to review" : "No scan yet"}
          description={
            queue.data
              ? "Every scanned file is attached or ignored. Kick a scan to pick up new files."
              : "Kick a scan to inventory the NAS mount, then the matcher will propose books for what it finds."
          }
          action={
            <Button onClick={() => api.kickMediaScan().then(() => scan.refetch())}>
              <Gi name="refresh" className="size-4" />
              Scan now
            </Button>
          }
        />
        )
      ) : (
        <>
          {bulkableCount > 0 && (
            <Panel className="mb-4 flex flex-wrap items-center justify-between gap-3 p-4">
              <p className="text-sm text-ink-300">
                <span className="font-display text-[11px] uppercase tracking-wider text-ink-100">
                  {bulkableCount} high-confidence match{bulkableCount === 1 ? "" : "es"}
                </span>{" "}
                — confirm them all in one go?
              </p>
              <Button variant="primary" onClick={bulkConfirm}>
                <Gi name="check" className="size-4" />
                Confirm {bulkableCount}
              </Button>
            </Panel>
          )}

          <ul className="space-y-3">
            {candidates.map((candidate) => (
              <CandidateRow
                key={candidate.key}
                candidate={candidate}
                busy={busyKeys.has(candidate.key)}
                onConfirm={(suggestion) => {
                  markBusy(candidate.key, true);
                  confirm.mutate({ candidate, suggestion });
                }}
                onPick={() => setPickFor(candidate)}
                onIgnore={() => ignore.mutate(candidate)}
                ignoring={ignore.isPending && ignore.variables?.key === candidate.key}
              />
            ))}
          </ul>
        </>
      )}

      {skipped.length > 0 && (
        <div className="mt-6">
          <button
            type="button"
            onClick={() => setShowSkipped((open) => !open)}
            aria-expanded={showSkipped}
            className="flex w-full items-center justify-between rounded-xl px-3 py-2 text-left text-sm text-ink-400 transition-colors hover:text-ink-200 focus-visible:focus-ring"
          >
            <span>
              <Gi name="ban" className="mr-2 inline size-4" />
              {skipped.length} file{skipped.length === 1 ? "" : "s"} skipped — unsupported formats
              and DRM, never inventoried
            </span>
            <Gi
              name={showSkipped ? "chevron-up" : "chevron-down"}
              className="size-4"
              label={showSkipped ? "Hide skipped files" : "Show skipped files"}
            />
          </button>
          {showSkipped && <SkippedList skipped={skipped} />}
        </div>
      )}

      <PickBookDialog
        candidate={pickFor}
        onClose={() => setPickFor(null)}
        onPicked={async (book, entryId) => {
          const candidate = pickFor;
          setPickFor(null);
          if (!candidate) return;
          markBusy(candidate.key, true);
          let resolved = entryId;
          try {
            if (!resolved) {
              const entry = await api.addBookToLibrary(book.id);
              resolved = entry.id;
            }
            await api.attachFiles(
              resolved,
              candidate.files.map((f) => f.id),
              candidate.kind,
            );
            setStatus({ kind: "ok", message: `Attached to ${book.title}.` });
          } catch (error) {
            setStatus({ kind: "error", message: (error as Error).message });
          }
          markBusy(candidate.key, false);
          invalidate();
        }}
      />
    </div>
  );
}

function CandidateRow({
  candidate,
  busy,
  ignoring,
  onConfirm,
  onPick,
  onIgnore,
}: {
  candidate: MediaCandidate;
  busy: boolean;
  ignoring: boolean;
  onConfirm: (suggestion: MediaSuggestion) => void;
  onPick: () => void;
  onIgnore: () => void;
}) {
  const top = candidate.suggestions?.[0];
  const isAudio = candidate.kind === "audio";

  return (
    <li>
      <Panel className="p-4 sm:p-5">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="f-chip px-2 py-1 font-display text-[10px] uppercase tracking-wider text-ink-300">
                {isAudio ? "Audio" : "Ebook"}
              </span>
              {candidate.high_confidence && (
                <span className="f-chip-active px-2 py-1 font-display text-[10px] uppercase tracking-wider text-ink-100">
                  High confidence
                </span>
              )}
            </div>
            <h3 className="mt-2 truncate text-sm font-semibold text-ink-100">
              {candidate.title_guess || candidate.files[0]?.path || candidate.dir_path}
              {candidate.author_guess && (
                <span className="font-normal text-ink-400"> — {candidate.author_guess}</span>
              )}
            </h3>
            <p className="mt-0.5 truncate font-mono text-xs text-ink-500">
              {candidate.dir_path === "." ? candidate.root : `${candidate.root}/${candidate.dir_path}`}
            </p>
            <p className="mt-1 text-xs text-ink-500">
              {candidate.files.length} file{candidate.files.length === 1 ? "" : "s"}
              {isAudio && candidate.total_duration_seconds > 0 && (
                <> · {formatDuration(candidate.total_duration_seconds)}</>
              )}
            </p>
            <details className="mt-2">
              <summary className="cursor-pointer text-xs text-ink-500 transition-colors hover:text-ink-300">
                Show files
              </summary>
              <ol className="mt-1 space-y-0.5">
                {candidate.files.slice(0, 5).map((file) => (
                  <li key={file.id} className="truncate font-mono text-xs text-ink-500">
                    {file.path}
                  </li>
                ))}
                {candidate.files.length > 5 && (
                  <li className="text-xs text-ink-500">
                    and {candidate.files.length - 5} more, in track order
                  </li>
                )}
              </ol>
            </details>
          </div>

          <div className="flex w-full shrink-0 flex-col items-stretch gap-2 sm:w-auto sm:max-w-64 sm:min-w-0 sm:items-end">
            {top ? (
              <>
                <div className="min-w-0 max-w-full text-right">
                  <p className="truncate text-sm font-semibold text-ink-100" title={top.book.title}>
                    {top.book.title}
                  </p>
                  <p className="truncate text-xs text-ink-400" title={byline(top.book)}>
                    {byline(top.book)}
                  </p>
                  <p className="mt-0.5 font-display text-[10px] uppercase tracking-wider text-ink-500">
                    {Math.round(top.confidence * 100)}% · from {top.signal} ·{" "}
                    {top.in_library ? "in your library" : "Open Library"}
                  </p>
                </div>
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    variant="primary"
                    loading={busy}
                    disabled={ignoring}
                    onClick={() => onConfirm(top)}
                  >
                    <Gi name="check" className="size-4" />
                    Confirm
                  </Button>
                  <Button size="sm" disabled={busy || ignoring} onClick={onPick}>
                    <Gi name="search" className="size-4" />
                    Pick book
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="text-ink-400 hover:text-ink-200"
                    loading={ignoring}
                    disabled={busy}
                    onClick={onIgnore}
                  >
                    <Gi name="ban" className="size-4" />
                    Ignore
                  </Button>
                </div>
              </>
            ) : (
              <div className="flex flex-col items-end gap-2">
                <p className="max-w-48 text-right text-xs text-ink-500">
                  No confident match — pick the book yourself, or ignore these files.
                </p>
                <div className="flex gap-2">
                  <Button size="sm" disabled={busy || ignoring} onClick={onPick}>
                    <Gi name="search" className="size-4" />
                    Pick book
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="text-ink-400 hover:text-ink-200"
                    loading={ignoring}
                    disabled={busy}
                    onClick={onIgnore}
                  >
                    <Gi name="ban" className="size-4" />
                    Ignore
                  </Button>
                </div>
              </div>
            )}
          </div>
        </div>
      </Panel>
    </li>
  );
}

const SKIP_REASONS: Record<string, (ext: string) => string> = {
  unsupported_extension: (ext) =>
    ext === ".aax" || ext === ".aaxc"
      ? "Audible DRM format — out of scope, this tool is DRM-free by decision"
      : `Unsupported file type (${ext || "no extension"}) — only mp3, m4a, m4b and epub are inventoried`,
  drm_epub: () => "DRM-wrapped EPUB (encryption.xml) — out of scope, this tool is DRM-free by decision",
};

function SkippedList({ skipped }: { skipped: { path: string; ext: string; reason: string }[] }) {
  const groups = useMemo(() => {
    const byReason = new Map<string, { label: string; files: string[] }>();
    for (const f of skipped) {
      const explain = SKIP_REASONS[f.reason] ?? ((ext: string) => `Skipped (${f.reason}${ext})`);
      const label = explain(f.ext);
      const group = byReason.get(label) ?? { label, files: [] };
      group.files.push(f.path);
      byReason.set(label, group);
    }
    return [...byReason.values()];
  }, [skipped]);

  return (
    <div className="mt-2 space-y-3">
      {groups.map((group) => (
        <details key={group.label} className="f-panel px-4 py-3">
          <summary className="cursor-pointer text-sm text-ink-300">
            <span className="font-display text-[10px] uppercase tracking-wider text-ink-400">
              {group.files.length} file{group.files.length === 1 ? "" : "s"}
            </span>{" "}
            — {group.label}
          </summary>
          <ul className="mt-2 space-y-0.5">
            {group.files.map((path) => (
              <li key={path} className="truncate font-mono text-xs text-ink-500">
                {path}
              </li>
            ))}
          </ul>
        </details>
      ))}
    </div>
  );
}

function PickBookDialog({
  candidate,
  onClose,
  onPicked,
}: {
  candidate: MediaCandidate | null;
  onClose: () => void;
  onPicked: (book: Book, entryId?: string) => void;
}) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<{ book: Book; in_library: boolean; entry_id?: string }[]>([]);

  useEffect(() => {
    setQuery("");
    setResults([]);
  }, [candidate]);

  useEffect(() => {
    if (!candidate || query.trim().length < 2) {
      setResults([]);
      return;
    }
    const controller = new AbortController();
    const timer = setTimeout(async () => {
      try {
        const res = await api.searchBooks(query.trim(), controller.signal);
        setResults(res.results);
      } catch {
        /* cancelled or failed; the empty list is honest enough */
      }
    }, 300);
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [candidate, query]);

  return (
    <Dialog open={candidate !== null} onClose={onClose} label="Pick the book these files belong to">
      <h2 className="text-base font-semibold text-ink-100">Pick the book</h2>
      <p className="mt-1 text-sm text-ink-400">
        {candidate
          ? `Which book is “${candidate.title_guess || candidate.files[0]?.path}”?`
          : "Which book is this?"}
      </p>
      <div className="mt-4">
        <label
          htmlFor="book-pick-search"
          className="mb-1.5 block font-display text-[10px] font-bold uppercase tracking-widest text-ink-400"
        >
          Search Open Library
        </label>
        <Input
          id="book-pick-search"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder={candidate?.title_guess || "Title or author"}
          autoFocus
        />
      </div>
      <ul className="mt-3 max-h-80 space-y-2 overflow-y-auto pr-1">
        {results.map((result) => (
          <li key={result.book.id}>
            <button
              type="button"
              onClick={() => onPicked(result.book, result.entry_id)}
              className="f-panel flex w-full flex-col items-start px-3 py-2 text-left transition-colors hover:text-ink-100 focus-visible:focus-ring"
            >
              <span className="text-sm font-semibold text-ink-100">{result.book.title}</span>
              <span className="text-xs text-ink-400">
                {(result.book.authors ?? []).join(", ")}
                {result.book.first_publish_year ? ` · ${result.book.first_publish_year}` : ""}
                {result.in_library ? " · in your library" : ""}
              </span>
            </button>
          </li>
        ))}
        {query.trim().length >= 2 && results.length === 0 && (
          <li className="px-1 py-2 text-sm text-ink-500">No matches yet.</li>
        )}
      </ul>
    </Dialog>
  );
}
