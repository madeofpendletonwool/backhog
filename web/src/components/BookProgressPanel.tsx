import { useQuery } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";

import { ProgressBar } from "@/components/ProgressBar";
import { ScanPageDialog } from "@/components/ScanPageDialog";
import { Gi } from "@/components/ui/Gi";
import { Button, Panel } from "@/components/ui/primitives";
import { api } from "@/lib/api";
import { chapterTitle, explainPage, formatPage } from "@/lib/booktext";
import { formatTimecode, relativeTime } from "@/lib/format";
import type { BookEdition, BookPosition } from "@/lib/types";

/**
 * Where you are in the book, and the fastest way to say where you are now.
 *
 * One position, three readings — the page of the paperback, the chapter,
 * the second of the tape — and a progress bar across the lot. It sits
 * directly under the hero because the question it answers ("where was I?")
 * is the one a reader asks on the way *in*, before the blurb and the
 * printings and the notes.
 *
 * "Scan a page" lives here too, and not only in the paper panel further
 * down: the switch from the audiobook to the physical copy happens with the
 * phone in one hand and the book in the other, and the button has to be
 * reachable without scrolling past everything else about the book. The
 * paper panel keeps its own copy, because that is where the map is managed.
 */
export function BookProgressPanel({
  entryId,
  editions,
}: {
  entryId: string;
  editions: BookEdition[];
}) {
  const [scanning, setScanning] = useState(false);

  const { data: position } = useQuery({
    queryKey: ["bookPosition", entryId],
    queryFn: () => api.bookPosition(entryId),
  });
  const { data: copies } = useQuery({
    queryKey: ["bookCopies", entryId],
    queryFn: () => api.bookCopies(entryId),
  });

  if (!position) return null;
  // Nothing attached, nothing to be somewhere in.
  const trackable = position.char_count > 0 || position.page_count > 0 || position.audio !== null;
  if (!trackable) return null;

  const paged = position.position_mode === "page";
  const pageCount = editions.find((edition) => edition.page_count)?.page_count ?? null;
  const percent = paged
    ? (((position.page_index ?? 0) + 1) / Math.max(1, position.page_count)) * 100
    : position.percent;
  const started = position.updated_at !== null;
  const complete = percent >= 100;

  // The copy whose page map the position reads; the same choice the paper
  // panel makes, so the two never disagree about which copy a scan feeds.
  const copy = copies?.copies.find((c) => c.drives_pages) ?? copies?.copies[0] ?? null;
  // Scanning matches a photographed page against the text, so it needs one.
  const scannable = copy !== null && position.char_count > 0;

  return (
    <Panel className="p-5">
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <h2 className="text-sm font-semibold text-ink-200">Where you are</h2>
        {position.updated_at && (
          <p className="text-xs text-ink-500">
            {describeSource(position.source)} · {relativeTime(position.updated_at)}
          </p>
        )}
      </div>

      <div className="mt-3 flex flex-wrap items-end justify-between gap-x-4 gap-y-1">
        <p className="font-display text-lg uppercase tracking-wider text-ink-max">
          {headline(position, pageCount)}
        </p>
        <p className="font-display text-sm uppercase tracking-wider text-ink-300">
          {complete ? "Finished" : `${Math.round(percent)}%`}
        </p>
      </div>
      <div className="mt-2">
        <ProgressBar percent={percent} complete={complete} />
      </div>

      <dl className="mt-3 flex flex-wrap gap-x-5 gap-y-1.5 text-xs text-ink-400">
        {!paged && position.chapter && (
          <Reading icon="scroll-unfurled" label="Chapter">
            {chapterTitle(position.chapter)}
          </Reading>
        )}
        {!paged && position.page && (
          <Reading icon="book-pile" label="On paper" title={explainPage(position.page) ?? undefined}>
            {formatPage(position.page)}
            {pageCount !== null && <span className="text-ink-500"> of {pageCount}</span>}
          </Reading>
        )}
        {position.audio && (
          <Reading icon="headphones" label="On tape">
            {formatTimecode(position.audio.seconds)}
            <span className="text-ink-500"> of {formatTimecode(position.audio.total_duration)}</span>
          </Reading>
        )}
      </dl>

      {!paged && (
        <div className="mt-4 flex flex-wrap items-center gap-3">
          {scannable ? (
            <Button variant="primary" onClick={() => setScanning(true)}>
              <Gi name="camera" className="size-3.5" />
              Scan a page
            </Button>
          ) : (
            <p className="text-xs leading-relaxed text-ink-500">
              {position.char_count === 0
                ? "Attach the ebook and a photographed page can be matched into the text."
                : "Register your paper copy under On paper below, and a photo of the page you're on becomes your position."}
            </p>
          )}
          {scannable && !started && (
            <p className="text-xs text-ink-500">Not started — a scan sets your place.</p>
          )}
        </div>
      )}

      {scannable && copy && (
        <ScanPageDialog
          open={scanning}
          onClose={() => setScanning(false)}
          entryId={entryId}
          copyId={copy.id}
          anchorCount={copy.anchor_count}
        />
      )}
    </Panel>
  );
}

/**
 * The one line that answers the question, in whichever space is most
 * concrete: the printed page beats the chapter beats the bare percentage,
 * because a page number is what a person holding the book can act on.
 */
function headline(position: BookPosition, pageCount: number | null): string {
  if (position.position_mode === "page") {
    if (position.updated_at === null) return "Not started";
    return `Page ${(position.page_index ?? 0) + 1} of ${position.page_count}`;
  }
  if (position.updated_at === null || position.percent <= 0) return "Not started";
  if (position.percent >= 100) return "Finished";
  if (position.page) {
    const page = formatPage(position.page) ?? "";
    // "page 214 ± 3 of 480": the estimate first, the whole after it.
    return pageCount !== null ? `${capitalize(page)} of ${pageCount}` : capitalize(page);
  }
  if (position.chapter) return chapterTitle(position.chapter);
  return `${Math.round(position.percent)}% through`;
}

/** Which end of the app last said where you are. */
function describeSource(source: string): string {
  switch (source) {
    case "scan":
      return "From a scanned page";
    case "read":
      return "From the reader";
    case "listen":
      return "From the audiobook";
    default:
      return "Set by hand";
  }
}

function capitalize(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

function Reading({
  icon,
  label,
  title,
  children,
}: {
  icon: "scroll-unfurled" | "book-pile" | "headphones";
  label: string;
  title?: string;
  children: ReactNode;
}) {
  return (
    <div className="inline-flex items-center gap-1.5" title={title}>
      <dt className="sr-only">{label}</dt>
      <Gi name={icon} className="size-3.5 shrink-0 text-ink-500" />
      <dd className="text-ink-300">{children}</dd>
    </div>
  );
}
