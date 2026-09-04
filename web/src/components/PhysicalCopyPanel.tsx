import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router-dom";

import { ScanPageDialog } from "@/components/ScanPageDialog";
import { Gi } from "@/components/ui/Gi";
import { Button, Input, Panel, Select } from "@/components/ui/primitives";
import { api } from "@/lib/api";
import { explainPage, formatPage } from "@/lib/booktext";
import { editionLabel, formatDate } from "@/lib/format";
import type { BookEdition } from "@/lib/types";

/**
 * The paper half of a book: the printing the reader holds — owned or
 * borrowed from the library — and the map from its page numbers into the
 * text.
 *
 * Everything here is about making the mechanic legible. The panel says
 * where the copy came from and when it goes back, how many pages are
 * mapped, what page you are on and how sure it is of that, and that
 * scanning more improves it — because a map that silently gets better is
 * a map nobody bothers to feed.
 */
export function PhysicalCopyPanel({
  entryId,
  editions,
}: {
  entryId: string;
  editions: BookEdition[];
}) {
  const queryClient = useQueryClient();
  const [scanning, setScanning] = useState(false);
  const [chosen, setChosen] = useState("");
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
      api.createBookCopy(entryId, chosen || offered[0].id, opts),
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
  const invalidate = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: ["bookCopies", entryId] }),
      queryClient.invalidateQueries({ queryKey: ["bookPosition", entryId] }),
    ]);

  // Page numbers belong to a printing, so registering a copy is the reader
  // saying which printing this entry is. The server anchors an unanchored
  // entry to it; a reader who already holds another printing keeps that
  // one, and the copy below says so rather than quietly mapping nothing.
  const offered = editions;
  const copy = copies.data?.copies.find((c) => c.drives_pages) ?? copies.data?.copies[0] ?? null;
  const page = position.data?.page ?? null;
  const pageCount = editions.find((edition) => edition.page_count)?.page_count ?? null;

  const borrowed = copy?.acquisition === "borrowed";
  const returned = copy?.returned_at != null;
  const actionError =
    register.error ?? giveBack.error ?? checkOutAgain.error ?? buyIt.error ?? drop.error;

  if (editions.length === 0) return null;

  return (
    <Panel className="p-5">
      <h2 className="mb-1 text-sm font-semibold text-ink-200">On paper</h2>
      <p className="mb-3 text-xs leading-relaxed text-ink-500">
        Scan a page and Backhog knows where you are in the book — and in the audiobook.
      </p>

      {!copy ? (
        <>
          <p className="text-xs leading-relaxed text-ink-400">
            Register the printing you hold — yours or the library's — and its page numbers become a
            position like any other.
          </p>
          {offered.length > 1 && (
            <Select
              value={chosen}
              onChange={(event) => setChosen(event.target.value)}
              className="mt-3"
              aria-label="Which printing do you hold?"
            >
              <option value="">Which printing?</option>
              {offered.map((edition) => (
                <option key={edition.id} value={edition.id}>
                  {editionLabel(edition)}
                </option>
              ))}
            </Select>
          )}
          <div className="mt-3 flex flex-wrap gap-2">
            <Button
              size="sm"
              variant="secondary"
              loading={register.isPending}
              disabled={offered.length > 1 && chosen === ""}
              onClick={() => register.mutate({ acquisition: "owned" })}
            >
              <Gi name="book-pile" className="size-3.5" />
              I own this on paper
            </Button>
            <Button
              size="sm"
              variant="secondary"
              disabled={(offered.length > 1 && chosen === "") || register.isPending}
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
          {actionError && (
            <p className="mt-2 text-xs text-red-300">
              {actionError instanceof Error ? actionError.message : "That did not register."}
            </p>
          )}
        </>
      ) : (
        <>
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

          {!copy.drives_pages && (
            <p className="mt-1 text-xs leading-relaxed text-amber-300/90">
              Your progress is tracked against a different printing of this book, so this copy's
              pages are recorded but not shown.
            </p>
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
            {copy.anchor_count === 0
              ? "Until you scan one, page numbers are stretched evenly across the text — right to a chapter or so, no better."
              : "Accuracy improves as you scan more."}
          </p>

          <div className="mt-3 flex flex-wrap gap-2">
            <Button size="sm" variant="secondary" onClick={() => setScanning(true)}>
              <Gi name="camera" className="size-3.5" />
              Scan a page
            </Button>
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

          {actionError && (
            <p className="mt-2 text-xs text-red-300">
              {actionError instanceof Error ? actionError.message : "That did not work."}
            </p>
          )}

          <ScanPageDialog
            open={scanning}
            onClose={() => setScanning(false)}
            entryId={entryId}
            copyId={copy.id}
            anchorCount={copy.anchor_count}
          />
        </>
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
