import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from "react";

import { Gi } from "@/components/ui/Gi";
import { Button, Input } from "@/components/ui/primitives";
import { barcodeScanningSupported, createISBNDetector } from "@/lib/barcode";
import { cn } from "@/lib/cn";
import { matchEditions, sortEditions, type ISBNHit } from "@/lib/editions";
import { editionISBN, formatPages, looksLikeISBN, normalizeISBN } from "@/lib/format";
import type { BookEdition } from "@/lib/types";

/**
 * Pick the printing in your hands out of however many the catalogue holds.
 *
 * A native select was fine for eight printings and hopeless for three
 * hundred that share a page count, so this is a filter first and a list
 * second: type any run of the ISBN off the back cover (or a publisher, a
 * year, a page count) and the list narrows as you type; arrows and Enter
 * pick without the mouse. Where the browser can read barcodes, the camera
 * fills the search in for you.
 */

/** Rows rendered at once; past this the list asks for a narrower query. */
const RENDER_LIMIT = 80;

export function EditionPicker({
  editions,
  value,
  onChange,
  unsureLabel,
  busy = false,
  autoFocus = false,
  className,
}: {
  editions: BookEdition[];
  /** The printing currently chosen; null for "not sure". */
  value: string | null;
  onChange: (editionId: string | null) => void;
  /** Offers a "not sure which printing" choice when set. */
  unsureLabel?: string;
  busy?: boolean;
  autoFocus?: boolean;
  className?: string;
}) {
  const listId = useId();
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const [scanning, setScanning] = useState(false);
  const canScan = useMemo(barcodeScanningSupported, []);
  const listRef = useRef<HTMLUListElement>(null);

  const sorted = useMemo(() => sortEditions(editions), [editions]);
  const matches = useMemo(() => matchEditions(sorted, query), [sorted, query]);
  const shown = matches.slice(0, RENDER_LIMIT);

  // A new query starts the cursor at its best match.
  useEffect(() => setActive(0), [query]);

  useEffect(() => {
    listRef.current
      ?.querySelector(`[data-index="${active}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [active]);

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setActive((i) => Math.min(i + 1, shown.length - 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActive((i) => Math.max(i - 1, 0));
    } else if (event.key === "Enter") {
      event.preventDefault();
      const pick = shown[active];
      if (pick) onChange(pick.edition.id);
    } else if (event.key === "Escape" && query) {
      event.preventDefault();
      setQuery("");
    }
  };

  return (
    <div className={className}>
      <div className="flex gap-2">
        <div className="relative min-w-0 flex-1">
          <Gi
            name="search"
            className="pointer-events-none absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-ink-500"
          />
          <Input
            type="search"
            role="combobox"
            aria-expanded="true"
            aria-controls={listId}
            aria-activedescendant={shown[active] ? `${listId}-${active}` : undefined}
            aria-label="Find your printing"
            autoFocus={autoFocus}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={onKeyDown}
            placeholder="ISBN off the back, publisher, year, pages…"
            className="pl-8"
          />
        </div>
        {canScan && (
          <Button
            variant="secondary"
            onClick={() => setScanning((on) => !on)}
            aria-pressed={scanning}
            title="Scan the barcode on the back"
          >
            <Gi name="camera" className="size-3.5" />
            <span className="sr-only sm:not-sr-only">{scanning ? "Stop" : "Scan"}</span>
          </Button>
        )}
      </div>

      {scanning && (
        <BarcodeViewfinder
          onISBN={(isbn) => {
            setQuery(isbn);
            setScanning(false);
          }}
        />
      )}

      <p className="mt-2 text-[11px] text-ink-500" aria-live="polite">
        {query
          ? matches.length === 0
            ? "No printing matches that."
            : `${matches.length} of ${editions.length} printing${editions.length === 1 ? "" : "s"}`
          : `${editions.length} printing${editions.length === 1 ? "" : "s"} on file — the ISBN narrows it fastest.`}
      </p>

      <ul
        ref={listRef}
        id={listId}
        role="listbox"
        aria-label="Printings"
        className="mt-1.5 max-h-72 space-y-0.5 overflow-y-auto rounded-xl border border-edge p-1"
      >
        {unsureLabel && !query && (
          <li
            role="option"
            aria-selected={value === null}
            onClick={() => onChange(null)}
            className={cn(
              "flex cursor-pointer items-center gap-2 rounded-lg px-2.5 py-2 text-sm text-ink-400 hover:bg-fill-hover",
              value === null && "f-chip-active text-ink-100",
            )}
          >
            <span className="flex-1">{unsureLabel}</span>
            {value === null && <Gi name="check" className="size-3.5 shrink-0" />}
          </li>
        )}
        {shown.map(({ edition, isbnHit }, index) => (
          <EditionRow
            key={edition.id}
            id={`${listId}-${index}`}
            index={index}
            edition={edition}
            isbnHit={isbnHit}
            active={index === active}
            selected={edition.id === value}
            busy={busy && edition.id !== value}
            onHover={() => setActive(index)}
            onPick={() => onChange(edition.id)}
          />
        ))}
        {matches.length > shown.length && (
          <li className="px-2.5 py-2 text-[11px] text-ink-500">
            {matches.length - shown.length} more — keep typing to narrow it down.
          </li>
        )}
      </ul>
    </div>
  );
}

function EditionRow({
  id,
  index,
  edition,
  isbnHit,
  active,
  selected,
  busy,
  onHover,
  onPick,
}: {
  id: string;
  index: number;
  edition: BookEdition;
  isbnHit: ISBNHit | null;
  active: boolean;
  selected: boolean;
  busy: boolean;
  onHover: () => void;
  onPick: () => void;
}) {
  const isbn = normalizeISBN(editionISBN(edition));
  const title =
    [edition.publisher, edition.published_year ? String(edition.published_year) : ""]
      .filter(Boolean)
      .join(" · ") || "Unlabelled printing";
  const detail = [edition.binding, formatPages(edition.page_count), edition.language]
    .filter(Boolean)
    .join(" · ");

  return (
    <li
      id={id}
      data-index={index}
      role="option"
      aria-selected={selected}
      aria-disabled={busy}
      onMouseMove={onHover}
      onClick={busy ? undefined : onPick}
      className={cn(
        "flex cursor-pointer items-center gap-3 rounded-lg px-2.5 py-2",
        active && "bg-fill-hover",
        selected && "f-chip-active",
      )}
    >
      <div className="min-w-0 flex-1">
        <p className={cn("truncate text-sm", selected ? "text-ink-100" : "text-ink-200")}>
          {title}
        </p>
        {detail && <p className="truncate text-xs text-ink-500">{detail}</p>}
      </div>
      {isbn ? (
        <span className="shrink-0 font-mono text-[11px] tabular-nums text-ink-500">
          {isbnHit ? (
            <>
              {isbn.slice(0, isbnHit.start)}
              <mark className="rounded-sm bg-brand-500/25 text-ink-100">
                {isbn.slice(isbnHit.start, isbnHit.end)}
              </mark>
              {isbn.slice(isbnHit.end)}
            </>
          ) : (
            isbn
          )}
        </span>
      ) : (
        <span className="shrink-0 text-[11px] text-ink-600">no ISBN</span>
      )}
      <span className="w-3.5 shrink-0">
        {selected && <Gi name="check" className="size-3.5 text-ink-100" />}
      </span>
    </li>
  );
}

/**
 * A minimal barcode viewfinder: the first book ISBN it reads goes into the
 * search, and the camera closes. The add-book scanner's cadence and
 * fallbacks, without its lookup — the printings are already here.
 */
function BarcodeViewfinder({ onISBN }: { onISBN: (isbn: string) => void }) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [error, setError] = useState("");
  const onISBNRef = useRef(onISBN);
  onISBNRef.current = onISBN;

  useEffect(() => {
    const detector = createISBNDetector();
    if (!detector) {
      setError("This browser can't read barcodes — type the ISBN instead.");
      return;
    }
    let stream: MediaStream | null = null;
    let timer: number | undefined;
    let stopped = false;

    void (async () => {
      try {
        stream = await navigator.mediaDevices.getUserMedia({
          video: { facingMode: { ideal: "environment" } },
        });
      } catch {
        setError("Couldn't open the camera — type the ISBN instead.");
        return;
      }
      if (stopped || !videoRef.current) {
        stream.getTracks().forEach((track) => track.stop());
        return;
      }
      videoRef.current.srcObject = stream;
      await videoRef.current.play().catch(() => undefined);
      timer = window.setInterval(async () => {
        const video = videoRef.current;
        if (!video || video.readyState < 2) return;
        try {
          for (const code of await detector.detect(video)) {
            const isbn = normalizeISBN(code.rawValue);
            if (looksLikeISBN(isbn)) {
              onISBNRef.current(isbn);
              return;
            }
          }
        } catch {
          // One bad frame is not worth stopping for.
        }
      }, 350);
    })();

    return () => {
      stopped = true;
      if (timer !== undefined) window.clearInterval(timer);
      stream?.getTracks().forEach((track) => track.stop());
    };
  }, []);

  if (error) return <p className="mt-2 text-xs text-amber-300/90">{error}</p>;
  return (
    <div className="relative mt-2 overflow-hidden rounded-xl bg-ink-900 ring-1 ring-edge">
      <video
        ref={videoRef}
        muted
        playsInline
        aria-label="Barcode viewfinder"
        className="aspect-[3/1] w-full object-cover"
      />
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-x-8 top-1/2 h-px -translate-y-1/2 bg-red-400/70"
      />
    </div>
  );
}
