import { cn } from "@/lib/cn";
import { useEffect, useMemo, useRef, useState } from "react";

import { BookCover } from "./BookCover";
import { Dialog } from "./ui/Dialog";
import { Gi } from "./ui/Gi";
import { Button, Input, LoadingDot, Select } from "./ui/primitives";
import { useAddBookToLibrary, useBook, useBookByISBN, useBookSearch } from "@/hooks/useBooks";
import { useDebounced } from "@/hooks/useLibrary";
import { ApiError } from "@/lib/api";
import { barcodeScanningSupported, createISBNDetector } from "@/lib/barcode";
import {
  accentStyle,
  byline,
  editionISBN,
  editionLabel,
  looksLikeISBN,
  normalizeISBN,
  publishYear,
} from "@/lib/format";
import { BOOK_STATUS_LABELS, type Book, type BookEdition, type Status } from "@/lib/types";

type Mode = "scan" | "isbn" | "search";

/** The statuses a book can be added in. */
const ADD_STATUSES: Status[] = ["backlog", "playing", "played", "wishlist"];

/**
 * Three ways to put a book on the shelf, in the order they are fastest:
 * point the camera at the barcode, type the ISBN off the back, or search by
 * title and author. The middle one is the floor — every browser can type —
 * so the scanner is an accelerator, never a requirement.
 *
 * Whichever way you got here, the last step is the same: confirm the work and
 * say which printing you own. The edition is what page numbers will hang off,
 * so it is asked once, here, while the book is still in your hand.
 */
export function AddBookDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const canScan = useMemo(barcodeScanningSupported, []);
  const [mode, setMode] = useState<Mode>(canScan ? "scan" : "isbn");
  const [picked, setPicked] = useState<{ book: Book; isbn: string } | null>(null);

  useEffect(() => {
    if (open) return;
    // Reset to a clean dialog on close, so reopening never resumes someone
    // else's half-finished add.
    setMode(canScan ? "scan" : "isbn");
    setPicked(null);
  }, [open, canScan]);

  return (
    <Dialog open={open} onClose={onClose} bare label="Add a book" className="max-w-2xl">
      <div className="panel overflow-hidden">
        {picked ? (
          <ConfirmStep
            book={picked.book}
            isbn={picked.isbn}
            onBack={() => setPicked(null)}
            onAdded={onClose}
          />
        ) : (
          <>
            <div
              role="tablist"
              aria-label="How to add a book"
              className="flex items-center gap-1 border-b border-white/[0.06] px-3 py-2"
            >
              <ModeTab active={mode === "scan"} onClick={() => setMode("scan")} icon="camera">
                Scan
              </ModeTab>
              <ModeTab active={mode === "isbn"} onClick={() => setMode("isbn")} icon="keyboard">
                ISBN
              </ModeTab>
              <ModeTab active={mode === "search"} onClick={() => setMode("search")} icon="search">
                Search
              </ModeTab>
            </div>

            {mode === "scan" && (
              <ScanPanel
                canScan={canScan}
                onTypeInstead={() => setMode("isbn")}
                onResolved={(book, isbn) => setPicked({ book, isbn })}
              />
            )}
            {mode === "isbn" && (
              <IsbnPanel onResolved={(book, isbn) => setPicked({ book, isbn })} />
            )}
            {mode === "search" && (
              <SearchPanel onResolved={(book) => setPicked({ book, isbn: "" })} />
            )}
          </>
        )}
      </div>
    </Dialog>
  );
}

function ModeTab({
  active,
  onClick,
  icon,
  children,
}: {
  active: boolean;
  onClick: () => void;
  icon: "camera" | "keyboard" | "search";
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm transition-colors focus-visible:focus-ring",
        active ? "bg-white/[0.09] text-ink-100" : "text-ink-400 hover:bg-white/[0.05] hover:text-ink-200",
      )}
    >
      <Gi name={icon} className="size-3.5" />
      {children}
    </button>
  );
}

/* ------------------------------------------------------------------ scan */

/**
 * The camera path. The video element is the viewfinder and the detector runs
 * against it on a timer rather than every frame — a barcode does not move
 * fast, and a 350ms cadence keeps a phone from cooking in your hand.
 */
function ScanPanel({
  canScan,
  onTypeInstead,
  onResolved,
}: {
  canScan: boolean;
  onTypeInstead: () => void;
  onResolved: (book: Book, isbn: string) => void;
}) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [cameraError, setCameraError] = useState("");
  const [scanned, setScanned] = useState("");
  const [notABook, setNotABook] = useState("");

  useEffect(() => {
    if (!canScan || scanned) return;

    const detector = createISBNDetector();
    if (!detector) {
      setCameraError("This browser can't read barcodes.");
      return;
    }

    let stream: MediaStream | null = null;
    let timer: number | undefined;
    let stopped = false;

    const start = async () => {
      try {
        stream = await navigator.mediaDevices.getUserMedia({
          // The rear camera is the one pointed at the book.
          video: { facingMode: { ideal: "environment" } },
        });
      } catch (error) {
        setCameraError(
          error instanceof DOMException && error.name === "NotAllowedError"
            ? "Camera access was blocked. Allow it, or type the ISBN instead."
            : "No camera available. Type the ISBN instead.",
        );
        return;
      }
      if (stopped) {
        stream.getTracks().forEach((track) => track.stop());
        return;
      }
      const video = videoRef.current;
      if (!video) return;
      video.srcObject = stream;
      await video.play().catch(() => undefined);

      timer = window.setInterval(async () => {
        if (!videoRef.current || videoRef.current.readyState < 2) return;
        try {
          const found = await detector.detect(videoRef.current);
          for (const code of found) {
            const isbn = normalizeISBN(code.rawValue);
            if (looksLikeISBN(isbn)) {
              setNotABook("");
              setScanned(isbn);
              return;
            }
            setNotABook(code.rawValue);
          }
        } catch {
          // A detect() that throws on one frame is not worth stopping for;
          // the next tick tries again.
        }
      }, 350);
    };

    void start();

    return () => {
      stopped = true;
      if (timer !== undefined) window.clearInterval(timer);
      stream?.getTracks().forEach((track) => track.stop());
    };
  }, [canScan, scanned]);

  if (!canScan) {
    return (
      <Message
        title="This browser can't scan barcodes"
        body="Barcode scanning needs a Chromium-based browser on a device with a camera. Typing the ISBN works everywhere."
        action={
          <Button variant="primary" onClick={onTypeInstead}>
            <Gi name="keyboard" className="size-3.5" />
            Type the ISBN
          </Button>
        }
      />
    );
  }

  if (scanned) {
    return (
      <IsbnLookup
        isbn={scanned}
        onResolved={onResolved}
        onRetry={() => {
          setScanned("");
          setNotABook("");
        }}
        retryLabel="Scan another"
      />
    );
  }

  if (cameraError) {
    return (
      <Message
        title="Can't open the camera"
        body={cameraError}
        action={
          <Button variant="primary" onClick={onTypeInstead}>
            <Gi name="keyboard" className="size-3.5" />
            Type the ISBN
          </Button>
        }
      />
    );
  }

  return (
    <div className="p-4">
      <div className="relative overflow-hidden rounded-xl bg-ink-900 ring-1 ring-white/[0.07]">
        <video
          ref={videoRef}
          muted
          playsInline
          aria-label="Barcode viewfinder"
          className="aspect-video w-full object-cover"
        />
        {/* The aiming line: a barcode read is a horizontal sweep, so the
            guide is one too. Decorative — the label is on the video. */}
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-x-8 top-1/2 h-px -translate-y-1/2 bg-red-400/70"
        />
      </div>
      <p className="mt-3 text-center text-xs text-ink-500">
        {notABook
          ? `Read "${notABook}" — that isn't a book ISBN. Try the barcode above the price.`
          : "Hold the barcode on the back of the book steady in the frame."}
      </p>
    </div>
  );
}

/* ------------------------------------------------------------------ isbn */

/** The always-works path: the number printed under the barcode. */
function IsbnPanel({ onResolved }: { onResolved: (book: Book, isbn: string) => void }) {
  const [raw, setRaw] = useState("");
  const [submitted, setSubmitted] = useState("");

  const isbn = normalizeISBN(raw);
  const valid = looksLikeISBN(isbn);

  if (submitted) {
    return (
      <IsbnLookup
        isbn={submitted}
        onResolved={onResolved}
        onRetry={() => {
          setSubmitted("");
          setRaw("");
        }}
        retryLabel="Try another ISBN"
      />
    );
  }

  return (
    <form
      className="p-5"
      onSubmit={(event) => {
        event.preventDefault();
        if (valid) setSubmitted(isbn);
      }}
    >
      <label className="block">
        <span className="mb-1.5 block text-xs font-medium text-ink-400">ISBN</span>
        <Input
          autoFocus
          value={raw}
          onChange={(event) => setRaw(event.target.value)}
          placeholder="978-0-14-118776-1"
          inputMode="numeric"
          aria-label="ISBN"
          aria-describedby="isbn-hint"
        />
      </label>
      <p id="isbn-hint" className="mt-2 text-xs text-ink-500">
        Ten or thirteen digits, from the back cover or the copyright page. Dashes and spaces are
        fine.
      </p>
      <div className="mt-4 flex justify-end">
        <Button type="submit" variant="primary" disabled={!valid}>
          <Gi name="search" className="size-3.5" />
          Look it up
        </Button>
      </div>
    </form>
  );
}

/** Resolves one ISBN to its work, and says plainly when there isn't one. */
function IsbnLookup({
  isbn,
  onResolved,
  onRetry,
  retryLabel,
}: {
  isbn: string;
  onResolved: (book: Book, isbn: string) => void;
  onRetry: () => void;
  retryLabel: string;
}) {
  const { data, error, isLoading } = useBookByISBN(isbn, true);

  useEffect(() => {
    if (data) onResolved(data, isbn);
    // onResolved is a fresh arrow on every render of the parent; depending on
    // it would re-fire the hand-off on every keystroke elsewhere in the tree.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data, isbn]);

  if (isLoading) {
    return (
      <div className="flex items-center justify-center gap-2.5 px-6 py-16 text-sm text-ink-400">
        <LoadingDot className="bg-ink-500" />
        Looking up {isbn}…
      </div>
    );
  }

  if (error) {
    const notFound = error instanceof ApiError && error.status === 404;
    return (
      <Message
        title={notFound ? "No book with that ISBN" : "Lookup failed"}
        body={
          notFound
            ? `Open Library doesn't know ${isbn}. Search by title and author instead — the work is probably there under a different printing.`
            : (error as Error).message
        }
        action={
          <Button variant="secondary" onClick={onRetry}>
            {retryLabel}
          </Button>
        }
      />
    );
  }

  // Resolved: the effect above has handed the work up and the dialog is
  // already showing the confirm step.
  return null;
}

/* ---------------------------------------------------------------- search */

/** Title and author search, for the books that are not in front of you. */
function SearchPanel({ onResolved }: { onResolved: (book: Book) => void }) {
  const [term, setTerm] = useState("");
  const debounced = useDebounced(term, 300);
  const { data, isFetching, error } = useBookSearch(debounced);
  const results = data?.results ?? [];

  return (
    <>
      <div className="flex items-center gap-3 border-b border-white/[0.06] px-4">
        <Gi name="search" className="size-4 shrink-0 text-ink-500" />
        <input
          autoFocus
          value={term}
          onChange={(event) => setTerm(event.target.value)}
          placeholder="Search by title or author…"
          aria-label="Search for a book"
          className="h-14 w-full bg-transparent text-[15px] text-ink-100 outline-none placeholder:text-ink-500"
        />
        {isFetching && <LoadingDot className="size-2.5 shrink-0 bg-ink-500" />}
      </div>

      <div className="max-h-[55vh] overflow-y-auto">
        {error instanceof ApiError && error.status === 503 ? (
          <Message title="Book search is unavailable" body="The book metadata provider isn't reachable right now." />
        ) : error ? (
          <Message title="Search failed" body={(error as Error).message} />
        ) : term.trim().length < 2 ? (
          <Message
            title="Find a book"
            body="Type at least two characters. Works come from Open Library; you pick the printing on the next step."
          />
        ) : results.length === 0 && !isFetching ? (
          <Message title="No matches" body={`Nothing found for "${term}".`} />
        ) : (
          <ul className="p-2">
            {results.map((result) => (
              <li key={result.book.id}>
                <button
                  type="button"
                  onClick={() => onResolved(result.book)}
                  style={accentStyle(result.book)}
                  className="flex w-full items-center gap-3 rounded-xl p-2 text-left transition-colors hover:bg-white/[0.05] focus-visible:focus-ring"
                >
                  <BookCover book={result.book} className="w-10 shrink-0 rounded-lg" />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium text-ink-100">{result.book.title}</p>
                    <div className="mt-0.5 flex items-center gap-2.5 text-[11px] text-ink-400">
                      {byline(result.book) && <span className="truncate">{byline(result.book)}</span>}
                      {publishYear(result.book) && <span>{publishYear(result.book)}</span>}
                    </div>
                  </div>
                  <span
                    className={cn(
                      "flex shrink-0 items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-xs font-medium",
                      result.in_library ? "text-emerald-400" : "text-ink-500",
                    )}
                  >
                    {result.in_library ? (
                      <>
                        <Gi name="check" className="size-3.5" /> On the shelf
                      </>
                    ) : (
                      <>
                        <Gi name="plus" className="size-3.5" /> Add
                      </>
                    )}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </>
  );
}

/* --------------------------------------------------------------- confirm */

/**
 * The last step, shared by all three routes in. The printing picker is the
 * point of it: a work is "The Hobbit", but the copy on your shelf has a page
 * count, and page numbers are what a reading position will later be anchored
 * to. Arriving by ISBN preselects the printing that barcode belongs to.
 */
function ConfirmStep({
  book,
  isbn,
  onBack,
  onAdded,
}: {
  book: Book;
  isbn: string;
  onBack: () => void;
  onAdded: () => void;
}) {
  // Search hits are cached lean, so the work is re-read here for its
  // printings; an ISBN lookup already carries them and this resolves instantly
  // from cache.
  const { data: full, isLoading: loadingEditions } = useBook(book.id);
  const work = full ?? book;
  const editions = useMemo<BookEdition[]>(() => sortEditions(work.editions ?? []), [work.editions]);

  const [editionId, setEditionId] = useState("");
  const [status, setStatus] = useState<Status>("backlog");
  const add = useAddBookToLibrary();

  // Preselect the printing the scanned barcode names, once the list arrives.
  useEffect(() => {
    if (!isbn || editionId) return;
    const match = editions.find((edition) => editionISBN(edition) === isbn);
    if (match) setEditionId(match.id);
  }, [isbn, editions, editionId]);

  const conflict = add.error instanceof ApiError && add.error.status === 409;

  return (
    <div className="p-5">
      <button
        type="button"
        onClick={onBack}
        className="mb-4 inline-flex items-center gap-1.5 rounded-lg text-sm text-ink-400 transition-colors hover:text-ink-100 focus-visible:focus-ring"
      >
        <Gi name="arrow-left" className="size-4" />
        Back
      </button>

      <div className="flex gap-4" style={accentStyle(work)}>
        <div className="w-24 shrink-0">
          <BookCover book={work} className="ring-1 ring-white/10" />
        </div>
        <div className="min-w-0 flex-1">
          <h2 className="text-lg font-semibold leading-tight text-ink-100">{work.title}</h2>
          <p className="mt-1 text-sm text-ink-400">
            {[byline(work), publishYear(work)].filter(Boolean).join(" · ") || "Unknown author"}
          </p>
          {work.description && (
            <p className="mt-2 line-clamp-3 text-xs leading-relaxed text-ink-500">
              {work.description}
            </p>
          )}
        </div>
      </div>

      <div className="mt-5 grid gap-3 sm:grid-cols-2">
        <label className="block">
          <span className="mb-1.5 block text-xs font-medium text-ink-400">Shelf</span>
          <Select value={status} onChange={(event) => setStatus(event.target.value as Status)}>
            {ADD_STATUSES.map((value) => (
              <option key={value} value={value}>
                {BOOK_STATUS_LABELS[value]}
              </option>
            ))}
          </Select>
        </label>

        <label className="block">
          <span className="mb-1.5 block text-xs font-medium text-ink-400">
            Printing {loadingEditions && <span className="text-ink-600">· loading…</span>}
          </span>
          <Select
            value={editionId}
            disabled={editions.length === 0}
            onChange={(event) => setEditionId(event.target.value)}
          >
            <option value="">
              {editions.length === 0 ? "No printings on file" : "Not sure which one"}
            </option>
            {editions.map((edition) => (
              <option key={edition.id} value={edition.id}>
                {editionLabel(edition) || editionISBN(edition) || edition.id}
              </option>
            ))}
          </Select>
        </label>
      </div>

      <p className="mt-2 text-xs text-ink-500">
        Page counts belong to a printing, not to the work — pick the one you actually own and the
        numbers will match your copy.
      </p>

      {add.isError && (
        <p role="alert" className="mt-3 text-sm text-red-400">
          {conflict ? "That book is already on your shelf." : (add.error as Error).message}
        </p>
      )}

      <div className="mt-5 flex justify-end gap-2">
        <Button variant="ghost" onClick={onBack}>
          Cancel
        </Button>
        <Button
          variant="primary"
          loading={add.isPending}
          onClick={() =>
            add.mutate(
              { bookId: work.id, editionId: editionId || null, status },
              { onSuccess: onAdded },
            )
          }
        >
          <Gi name="plus" className="size-3.5" />
          Add to shelf
        </Button>
      </div>
    </div>
  );
}

/** Newest printings first, then the ones with a page count — the useful ones. */
function sortEditions(editions: BookEdition[]): BookEdition[] {
  return [...editions].sort((a, b) => {
    if (Boolean(a.page_count) !== Boolean(b.page_count)) return a.page_count ? -1 : 1;
    return (b.published_year ?? 0) - (a.published_year ?? 0);
  });
}

function Message({
  title,
  body,
  action,
}: {
  title: string;
  body: string;
  action?: React.ReactNode;
}) {
  return (
    <div className="px-6 py-12 text-center">
      <p className="text-sm font-medium text-ink-200">{title}</p>
      <p className="mx-auto mt-1.5 max-w-sm text-xs leading-relaxed text-ink-500">{body}</p>
      {action && <div className="mt-5 flex justify-center">{action}</div>}
    </div>
  );
}
