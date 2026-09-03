import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router-dom";

import { ScanPageDialog } from "@/components/ScanPageDialog";
import { Gi } from "@/components/ui/Gi";
import { Button, Panel, Select } from "@/components/ui/primitives";
import { api } from "@/lib/api";
import { explainPage, formatPage } from "@/lib/booktext";
import { editionLabel } from "@/lib/format";
import type { BookEdition } from "@/lib/types";

/**
 * The paper half of a book: the printing the reader actually holds, and the
 * map from its page numbers into the text.
 *
 * Everything here is about making the mechanic legible. The panel says how
 * many pages are mapped, what page you are on and how sure it is of that, and
 * that scanning more improves it — because a map that silently gets better is
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

  const copies = useQuery({
    queryKey: ["bookCopies", entryId],
    queryFn: () => api.bookCopies(entryId),
  });
  const position = useQuery({
    queryKey: ["bookPosition", entryId],
    queryFn: () => api.bookPosition(entryId),
  });

  const register = useMutation({
    mutationFn: (editionId: string) => api.createBookCopy(entryId, editionId),
    onSuccess: () => invalidate(),
  });
  const drop = useMutation({
    mutationFn: (copyId: string) => api.deleteBookCopy(entryId, copyId),
    onSuccess: () => invalidate(),
  });
  const invalidate = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: ["bookCopies", entryId] }),
      queryClient.invalidateQueries({ queryKey: ["bookPosition", entryId] }),
    ]);

  // Page numbers belong to a printing, so registering a copy is the reader
  // saying which printing this entry is. The server anchors an unanchored
  // entry to it; a reader who already owns another printing keeps that one,
  // and the copy below says so rather than quietly mapping nothing.
  const offered = editions;
  const copy = copies.data?.copies.find((c) => c.drives_pages) ?? copies.data?.copies[0] ?? null;
  const page = position.data?.page ?? null;
  const pageCount = editions.find((edition) => edition.page_count)?.page_count ?? null;

  if (editions.length === 0) return null;

  return (
    <Panel className="p-5">
      <h2 className="mb-1 text-sm font-semibold text-ink-200">Your paper copy</h2>
      <p className="mb-3 text-xs leading-relaxed text-ink-500">
        Scan a page and Backhog knows where you are in the book — and in the audiobook.
      </p>

      {!copy ? (
        <>
          <p className="text-xs leading-relaxed text-ink-400">
            Register the printing you hold and its page numbers become a position like any other.
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
          <Button
            size="sm"
            variant="secondary"
            className="mt-3"
            loading={register.isPending}
            disabled={offered.length > 1 && chosen === ""}
            onClick={() => register.mutate(offered.length > 1 ? chosen : offered[0].id)}
          >
            <Gi name="book-pile" className="size-3.5" />
            I have this on paper
          </Button>
          {register.error && (
            <p className="mt-2 text-xs text-red-300">
              {register.error instanceof Error ? register.error.message : "That did not register."}
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
