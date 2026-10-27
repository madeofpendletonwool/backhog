import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router-dom";

import { EditionPicker } from "@/components/EditionPicker";
import { ScanPageDialog } from "@/components/ScanPageDialog";
import { Gi } from "@/components/ui/Gi";
import { Button, Input, Panel } from "@/components/ui/primitives";
import { useUpdateEntry } from "@/hooks/useLibrary";
import { api } from "@/lib/api";
import { explainPage, formatPage } from "@/lib/booktext";
import { editionISBN, editionLabel, formatDate, normalizeISBN, pageCountFor } from "@/lib/format";
import type { BookEdition } from "@/lib/types";

/**
 * The copy in your hands: which printing it is, whether it is yours or the
 * library's, and the map from its page numbers into the text.
 *
 * The printing is asked once. It used to be asked twice — a Printings list
 * that anchored the entry and an On paper dropdown that registered a copy —
 * for what is one fact: page numbers belong to a printing, and the copy that
 * maps them is the copy of the printing the entry reads against. So the
 * printing is picked here, and the paper questions are about that printing.
 *
 * The rest is about making the mechanic legible: where the copy came from
 * and when it goes back, how many pages are mapped, what page you are on and
 * how sure it is of that, and that scanning more improves it — because a map
 * that silently gets better is a map nobody bothers to feed.
 */
export function YourCopyPanel({
  entryId,
  editionId,
  editions,
}: {
  entryId: string;
  /** The printing the entry is anchored to; null for a book added by title alone. */
  editionId: string | null;
  editions: BookEdition[];
}) {
  const queryClient = useQueryClient();
  const [scanning, setScanning] = useState(false);
  const [picking, setPicking] = useState(false);
  const [checkingOut, setCheckingOut] = useState(false);
  const [dueDate, setDueDate] = useState("");
  const [reopenDue, setReopenDue] = useState("");

  const copies = useQuery({
    queryKey: ["bookCopies", entryId],
    queryFn: () => api.bookCopies(entryId),
  });
  const position = useQuery({
    queryKey: ["bookPosition", entryId],
    queryFn: () => api.bookPosition(entryId),
  });

  const register = useMutation({
    mutationFn: (opts: { acquisition: "owned" | "borrowed"; dueAt?: string }) =>
      api.createBookCopy(entryId, editionId!, opts),
    onSuccess: () => {
      setCheckingOut(false);
      setDueDate("");
      invalidate();
    },
  });
  const drop = useMutation({
    mutationFn: (copyId: string) => api.deleteBookCopy(entryId, copyId),
    onSuccess: () => invalidate(),
  });
  const giveBack = useMutation({
    mutationFn: (copyId: string) => api.returnBookCopy(entryId, copyId),
    onSuccess: () => invalidate(),
  });
  const checkOutAgain = useMutation({
    mutationFn: (copyId: string) => api.reopenBookCopy(entryId, copyId, reopenDue || undefined),
    onSuccess: () => {
      setReopenDue("");
      invalidate();
    },
  });
  const buyIt = useMutation({
    mutationFn: (copyId: string) => api.ownBookCopy(entryId, copyId),
    onSuccess: () => invalidate(),
  });
  // A PDF is a printing: its own pages can grow this copy's map before any
  // scan, at seed confidence. Scans still win — a seed never evicts one —
  // and re-seeding after a re-parse refreshes the seeds wholesale.
  const seedFromPDF = useMutation({
    mutationFn: (copyId: string) => api.seedBookCopyFromPDF(entryId, copyId),
    onSuccess: () => invalidate(),
  });
  // Anchoring the entry to a printing. Page maps are untouched — every copy
  // keeps its own — so this only changes which one the position endpoints
  // read, and picking the old printing again brings its map straight back.
  const update = useUpdateEntry();
  const choosePrinting = (next: string | null) => {
    if (next === editionId) {
      setPicking(false);
      return;
    }
    update.mutate(
      { id: entryId, patch: { edition_id: next } },
      {
        onSuccess: () => {
          setPicking(false);
          void invalidate();
        },
      },
    );
  };
  const invalidate = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: ["bookCopies", entryId] }),
      queryClient.invalidateQueries({ queryKey: ["bookPosition", entryId] }),
    ]);

  // The copy of the chosen printing is the one that maps pages. Copies of
  // other printings keep their maps and are offered as a switch below.
  const allCopies = copies.data?.copies ?? [];
  const copy = editionId ? (allCopies.find((c) => c.edition_id === editionId) ?? null) : null;
  const otherCopies = allCopies.filter((c) => c !== copy);
  const edition = editions.find((e) => e.id === editionId) ?? null;
  const pdfSeed = copies.data?.pdf_seed ?? null;
  const page = position.data?.page ?? null;
  const pageCount = pageCountFor(editions, copy?.edition_id, editionId);

  const borrowed = copy?.acquisition === "borrowed";
  const returned = copy?.returned_at != null;
  // The provenance split behind "N pages mapped": how many came off the
  // reader's own paper versus seeded from the PDF, unverified.
  const seeded = copy?.seeded_count ?? 0;
  const scanned = copy ? copy.anchor_count - seeded : 0;
  const actionError =
    register.error ??
    giveBack.error ??
    checkOutAgain.error ??
    buyIt.error ??
    drop.error ??
    seedFromPDF.error ??
    update.error;

  if (editions.length === 0) return null;

  return (
    <Panel className="p-5">
      <h2 className="mb-1 text-sm font-semibold text-ink-200">Your copy</h2>
      <p className="mb-3 text-xs leading-relaxed text-ink-500">
        {edition
          ? "Page counts and scanned pages are read against this printing."
          : "Say which printing you hold and its page numbers line up with your copy — until then they're stretched evenly across the text."}
      </p>

      {edition && !picking ? (
        <div className="flex items-center gap-3 rounded-xl border border-edge px-3 py-2.5">
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm text-ink-100">
              {editionLabel(edition) || "Unlabelled printing"}
            </p>
            {editionISBN(edition) && (
              <p className="font-mono text-[11px] tabular-nums text-ink-500">
                ISBN {normalizeISBN(editionISBN(edition))}
              </p>
            )}
          </div>
          <Button size="sm" variant="ghost" onClick={() => setPicking(true)}>
            <Gi name="pencil" className="size-3.5" />
            Change
          </Button>
        </div>
      ) : (
        <>
          <EditionPicker
            editions={editions}
            value={editionId}
            onChange={choosePrinting}
            unsureLabel={editionId ? "I'm not sure which printing this is" : undefined}
            busy={update.isPending}
            autoFocus={picking}
          />
          {picking && (
            <Button
              size="sm"
              variant="ghost"
              className="mt-2 text-ink-400"
              onClick={() => setPicking(false)}
            >
              Keep this printing
            </Button>
          )}
        </>
      )}

      {edition && !copy && !picking && (
        <>
          <p className="mt-4 text-xs leading-relaxed text-ink-400">
            Holding it on paper — yours or the library's? Register it and a photo of any page
            becomes your position.
          </p>
          <div className="mt-3 flex flex-wrap gap-2">
            <Button
              size="sm"
              variant="secondary"
              loading={register.isPending}
              onClick={() => register.mutate({ acquisition: "owned" })}
            >
              <Gi name="book-pile" className="size-3.5" />
              I own this on paper
            </Button>
            <Button
              size="sm"
              variant="secondary"
              disabled={register.isPending}
              onClick={() => setCheckingOut(true)}
            >
              <Gi name="bookshelf" className="size-3.5" />
              Library copy
            </Button>
          </div>
          {checkingOut && (
            <div className="mt-3">
              <p className="text-xs leading-relaxed text-ink-400">
                When is it due back? Optional — the date is a reminder, nothing stricter.
              </p>
              <div className="mt-2 flex flex-wrap items-center gap-2">
                <Input
                  type="date"
                  value={dueDate}
                  onChange={(event) => setDueDate(event.target.value)}
                  className="w-40"
                  aria-label="Due back on (optional)"
                />
                <Button
                  size="sm"
                  variant="secondary"
                  loading={register.isPending}
                  onClick={() => register.mutate({ acquisition: "borrowed", dueAt: dueDate || undefined })}
                >
                  Check it out
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  className="text-ink-400"
                  onClick={() => {
                    setCheckingOut(false);
                    setDueDate("");
                  }}
                >
                  Never mind
                </Button>
              </div>
            </div>
          )}
        </>
      )}

      {copy && !picking && (
        <div className="mt-4 border-t border-edge pt-4">
          {page && (
            <>
              <p className="font-display text-[11px] uppercase tracking-wider text-ink-200">
                {formatPage(page)}
                {pageCount !== null && (
                  <span className="text-ink-400"> of {pageCount}</span>
                )}
              </p>
              <p className="mt-1.5 text-xs leading-relaxed text-ink-500">{explainPage(page)}</p>
            </>
          )}

          <p className="mt-3 font-display text-[11px] uppercase tracking-wider text-ink-300">
            {borrowed ? "Library copy" : "Owned on paper"}
            {borrowed && !returned && copy.due_at && (
              <span className="text-ink-400"> · due {formatDate(copy.due_at)}</span>
            )}
            {borrowed && returned && copy.returned_at && (
              <span className="text-ink-400"> · returned {formatDate(copy.returned_at)}</span>
            )}
          </p>
          {borrowed && returned && (
            <p className="mt-1 text-xs leading-relaxed text-ink-500">
              Its page map is kept — check the printing out again and the same map reopens with it.
            </p>
          )}

          <p className="mt-3 font-display text-[11px] uppercase tracking-wider text-ink-300">
            {copy.anchor_count === 0
              ? "No pages mapped yet"
              : `${copy.anchor_count} page${copy.anchor_count === 1 ? "" : "s"} mapped`}
          </p>
          <p className="mt-1 text-xs leading-relaxed text-ink-500">
            {copy.anchor_count === 0 ? (
              pdfSeed?.available
                ? "Until you scan one, page numbers are stretched evenly across the text — or seeded from the PDF below, which knows where its own pages sit."
                : "Until you scan one, page numbers are stretched evenly across the text — right to a chapter or so, no better."
            ) : seeded === 0 ? (
              "Accuracy improves as you scan more."
            ) : seeded === copy.anchor_count ? (
              "Seeded from the PDF, unscanned — a real page is a guess until you scan one. Any scan corrects its page on the spot."
            ) : (
              `${scanned} scanned, ${seeded} seeded from the PDF — where they disagree, the scan wins.`
            )}
          </p>

          <div className="mt-3 flex flex-wrap gap-2">
            <Button size="sm" variant="secondary" onClick={() => setScanning(true)}>
              <Gi name="camera" className="size-3.5" />
              Scan a page
            </Button>
            {pdfSeed?.available && (
              <Button
                size="sm"
                variant="secondary"
                loading={seedFromPDF.isPending}
                onClick={() => seedFromPDF.mutate(copy.id)}
              >
                <Gi name="sparkles" className="size-3.5" />
                {seeded > 0 ? "Seed again from PDF" : "Seed from PDF"}
              </Button>
            )}
            {borrowed && !returned && (
              <Button
                size="sm"
                variant="secondary"
                loading={giveBack.isPending}
                onClick={() => giveBack.mutate(copy.id)}
              >
                <Gi name="arrow-left" className="size-3.5" />
                Return it
              </Button>
            )}
            {borrowed && returned && (
              <>
                <Input
                  type="date"
                  value={reopenDue}
                  onChange={(event) => setReopenDue(event.target.value)}
                  className="w-40"
                  aria-label="New due date (optional)"
                />
                <Button
                  size="sm"
                  variant="secondary"
                  loading={checkOutAgain.isPending}
                  onClick={() => checkOutAgain.mutate(copy.id)}
                >
                  <Gi name="refresh" className="size-3.5" />
                  Check it out again
                </Button>
              </>
            )}
            {borrowed && (
              <Button
                size="sm"
                variant="ghost"
                className="text-ink-400"
                loading={buyIt.isPending}
                onClick={() => buyIt.mutate(copy.id)}
              >
                <Gi name="tags" className="size-3.5" />
                I bought it
              </Button>
            )}
            <Button
              size="sm"
              variant="ghost"
              className="text-ink-400"
              loading={drop.isPending}
              onClick={() => drop.mutate(copy.id)}
            >
              <Gi name="trash" className="size-3.5" />
              Forget this copy
            </Button>
          </div>

          <ScanPageDialog
            open={scanning}
            onClose={() => setScanning(false)}
            entryId={entryId}
            copyId={copy.id}
            anchorCount={copy.anchor_count}
          />
        </div>
      )}

      {!picking && otherCopies.length > 0 && (
        <div className="mt-4 border-t border-edge pt-4">
          <p className="text-xs leading-relaxed text-ink-500">
            Also registered — {otherCopies.length === 1 ? "its page map is" : "their page maps are"}{" "}
            kept, and switching to it reads against it again.
          </p>
          <ul className="mt-2 space-y-1">
            {otherCopies.map((other) => {
              const printing = editions.find((e) => e.id === other.edition_id);
              return (
                <li key={other.id} className="flex items-center gap-2 text-sm">
                  <span className="min-w-0 flex-1 truncate text-ink-300">
                    {(printing && editionLabel(printing)) || "Another printing"}
                    <span className="text-ink-500">
                      {" "}
                      · {other.acquisition === "borrowed" ? "library" : "owned"} ·{" "}
                      {other.anchor_count} page{other.anchor_count === 1 ? "" : "s"} mapped
                    </span>
                  </span>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="shrink-0 text-ink-400 hover:text-ink-200"
                    loading={update.isPending && update.variables?.patch.edition_id === other.edition_id}
                    onClick={() => choosePrinting(other.edition_id)}
                  >
                    Use this one
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="shrink-0 text-ink-500"
                    aria-label="Forget this copy"
                    loading={drop.isPending && drop.variables === other.id}
                    onClick={() => drop.mutate(other.id)}
                  >
                    <Gi name="trash" className="size-3.5" />
                  </Button>
                </li>
              );
            })}
          </ul>
        </div>
      )}

      {actionError && (
        <p role="alert" className="mt-2 text-xs text-red-300">
          {actionError instanceof Error ? actionError.message : "That did not work."}
        </p>
      )}

      {copy && !position.data?.char_count && (
        <p className="mt-3 text-xs leading-relaxed text-ink-400">
          Scanning needs the ebook attached — that is the text a photographed page gets matched
          against.{" "}
          <Link to="/books/files" className="text-brand-400 hover:text-brand-300">
            Book files
          </Link>
        </p>
      )}
    </Panel>
  );
}
