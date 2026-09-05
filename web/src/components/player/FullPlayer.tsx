import { cn } from "@/lib/cn";
import { useCallback, useEffect, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Link } from "react-router-dom";

import { RATES, useAudioClock, useAudioPlayer } from "@/hooks/useAudioPlayer";
import { bookCoverUrl } from "@/lib/api";
import { accentStyle, formatRemaining, formatTimecode, publishYear } from "@/lib/format";
import { BookCover } from "../BookCover";
import { Gi } from "../ui/Gi";
import { Button, LoadingDot } from "../ui/primitives";
import {
  Chip,
  ContinueReading,
  PaperPage,
  Section,
  SeekBar,
  SleepSection,
  Timecode,
  Transport,
} from "./parts";

/**
 * "Now playing": the bar opened out over the whole viewport.
 *
 * It is the only expanded surface the player has — the bar used to grow a
 * drawer upward, which fought the page behind it for the same inch of screen
 * and had no room for the things a listener actually reaches for. Here the
 * jacket is the size it deserves to be, the skips and the speed and the sleep
 * timer all fit side by side, and the names on the page are links: the author
 * filters the shelf, a subject filters the shelf, the title is the book.
 *
 * The bar stays mounted underneath, so --player-h never moves and closing
 * this costs no layout at all.
 */
export function FullPlayer() {
  const player = useAudioPlayer();
  const { entry, fullscreen, error, playing, loading, timeline, trackIndex } = player;

  const panelRef = useRef<HTMLDivElement>(null);
  const restoreFocusTo = useRef<HTMLElement | null>(null);

  const close = useCallback(() => player.setFullscreen(false), [player]);
  // Held in a ref for the same reason Dialog does it: the setup effect below
  // must depend on `fullscreen` alone, or every render would tear it down and
  // yank focus back to whatever opened the overlay.
  const closeRef = useRef(close);
  useEffect(() => {
    closeRef.current = close;
  });

  /* Escape, the body scroll lock, focus in and focus back out — the parts
     that are easy to leave out and immediately noticeable when missing. This
     mirrors ui/Dialog, which cannot be reused directly: its container is a
     centred max-w-lg card with a click-to-dismiss backdrop, and none of that
     is what an edge-to-edge player wants. */
  useEffect(() => {
    if (!fullscreen) return;

    restoreFocusTo.current = document.activeElement as HTMLElement | null;

    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";

    panelRef.current?.querySelector<HTMLElement>("button, [href]")?.focus();

    return () => {
      document.body.style.overflow = previousOverflow;
      restoreFocusTo.current?.focus?.();
    };
  }, [fullscreen]);

  /**
   * The shortcuts, live only while the overlay is up. Guarded against typing
   * targets so a search field somewhere behind the overlay can never lose a
   * space bar to the transport.
   */
  useEffect(() => {
    if (!fullscreen) return;

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      if (isTypingTarget(event.target)) return;

      switch (event.key) {
        case "Escape":
          event.stopPropagation();
          closeRef.current();
          return;
        case " ":
          // Space is also how a keyboard activates whatever button or link
          // has focus, and taking that away would make the overlay's own
          // controls unreachable. It only means play/pause when it would
          // otherwise have just scrolled the column.
          if (isActivatable(event.target)) return;
          event.preventDefault();
          player.toggle();
          return;
        case "k":
          player.toggle();
          return;
        case "ArrowLeft":
          event.preventDefault();
          player.skip(event.shiftKey ? -30 : -15);
          return;
        case "ArrowRight":
          event.preventDefault();
          player.skip(event.shiftKey ? 30 : 15);
          return;
        case "[":
        case "]": {
          const at = RATES.indexOf(player.rate as (typeof RATES)[number]);
          // A rate set from somewhere off the ladder (an older build, a hand-
          // edited key) lands on 1× rather than nowhere.
          const from = at === -1 ? RATES.indexOf(1) : at;
          const step = event.key === "]" ? 1 : -1;
          player.setRate(RATES[Math.min(RATES.length - 1, Math.max(0, from + step))]);
          return;
        }
      }
    };

    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [fullscreen, player]);

  if (!entry || !fullscreen) return null;
  const { book } = entry;
  const track = timeline?.tracks[trackIndex];
  const authors = book.authors ?? [];
  const subjects = (book.subjects ?? []).slice(0, 8);

  return createPortal(
    <div
      style={accentStyle(book)}
      role="dialog"
      aria-modal="true"
      aria-label={`Now playing: ${book.title}`}
      className="fixed inset-0 z-50 isolate overflow-y-auto overscroll-contain bg-ink-950"
    >
      {/* The jacket blown up and blurred behind its own artwork — the same
          recipe as the book page's hero, so arriving here from there feels
          like the same room. */}
      {book.cover_url && (
        <div
          className="fixed inset-0 -z-10 scale-110 bg-cover bg-center opacity-25 blur-3xl"
          style={{ backgroundImage: `url(${bookCoverUrl(book.id)})` }}
          aria-hidden="true"
        />
      )}
      <div
        className="fixed inset-0 -z-10 bg-gradient-to-b from-ink-950/40 via-ink-950/85 to-ink-950"
        aria-hidden="true"
      />

      <div
        ref={panelRef}
        className="animate-fade-rise mx-auto flex min-h-full w-full max-w-2xl flex-col gap-6 px-4 py-4 sm:px-6 sm:py-6"
      >
        <div className="flex items-center gap-2">
          <Transport
            label="Close full player"
            icon="chevrons-down"
            onClick={close}
            iconClassName="size-5"
          />
          <p className="min-w-0 flex-1 truncate text-center font-display text-[10px] uppercase tracking-widest text-ink-500">
            Now playing
          </p>
          <Link
            to={`/books/${entry.id}`}
            onClick={close}
            className="flex h-9 shrink-0 items-center gap-1.5 px-1.5 text-ink-300 transition-colors hover:text-ink-100 focus-visible:focus-ring"
          >
            <Gi name="external-link" className="size-4" />
            <span className="font-display text-[10px] uppercase tracking-wider">Book</span>
          </Link>
        </div>

        {error && (
          <p className="flex items-start gap-2 text-xs leading-relaxed text-red-300">
            <Gi name="ban" className="mt-px size-3.5 shrink-0" />
            <span className="min-w-0 flex-1">{error}</span>
            <button
              type="button"
              onClick={player.reload}
              className="shrink-0 font-display text-[10px] uppercase tracking-wider text-ink-300 transition-colors hover:text-ink-100 focus-visible:focus-ring"
            >
              Try again
            </button>
          </p>
        )}

        <div className="flex flex-col items-center gap-5 text-center">
          <Link
            to={`/books/${entry.id}`}
            onClick={close}
            aria-label={`${book.title} — book details`}
            className="w-44 shrink-0 rounded-xl sm:w-56 focus-visible:focus-ring"
          >
            <BookCover book={book} className="shadow-2xl ring-1 ring-edge-strong" />
          </Link>

          <div className="min-w-0 max-w-full">
            <h2 className="text-xl font-semibold tracking-tight text-ink-max sm:text-2xl">
              <Link
                to={`/books/${entry.id}`}
                onClick={close}
                className="rounded transition-colors hover:text-hl-bright focus-visible:focus-ring"
              >
                {book.title}
              </Link>
            </h2>

            {/* Each author is its own link: the shelf can filter by one, and
                a byline glued together with commas cannot be clicked apart. */}
            <p className="mt-1.5 text-sm text-ink-300">
              {authors.length > 0
                ? authors.map((name, index) => (
                    <span key={name}>
                      {index > 0 && <span className="text-ink-600">, </span>}
                      <Link
                        to={`/books?author=${encodeURIComponent(name)}`}
                        onClick={close}
                        title={`Every book by ${name} on your shelf`}
                        className="rounded underline decoration-ink-600 underline-offset-4 transition-colors hover:text-ink-100 hover:decoration-ink-300 focus-visible:focus-ring"
                      >
                        {name}
                      </Link>
                    </span>
                  ))
                : "Unknown author"}
              {publishYear(book) && (
                <span className="text-ink-500"> · {publishYear(book)}</span>
              )}
            </p>

            {track && (
              <p className="mt-1 truncate text-xs text-ink-500">
                {track.track_number}. {track.title}
              </p>
            )}
          </div>
        </div>

        <div>
          <SeekRow />
          <div className="mt-4 flex items-center justify-center gap-1 sm:gap-3">
            <Transport
              label="Previous track"
              icon="skip-back"
              onClick={player.previousTrack}
              iconClassName="size-5"
              className="h-12 px-3"
            />
            <Transport
              label="Back 30 seconds"
              icon="rewind"
              step="30"
              onClick={() => player.skip(-30)}
              iconClassName="size-5"
              className="h-12 px-3"
            />
            <Button
              variant="primary"
              size="lg"
              onClick={player.toggle}
              aria-label={playing ? "Pause" : "Play"}
              className="mx-1 w-20"
            >
              {loading && !playing ? (
                <LoadingDot />
              ) : (
                <Gi name={playing ? "pause" : "play"} className="size-5" />
              )}
            </Button>
            <Transport
              label="Forward 30 seconds"
              icon="fast-forward"
              step="30"
              onClick={() => player.skip(30)}
              iconClassName="size-5"
              className="h-12 px-3"
            />
            <Transport
              label="Next track"
              icon="skip-forward"
              onClick={player.nextTrack}
              iconClassName="size-5"
              className="h-12 px-3"
            />
          </div>
        </div>

        <div className="grid gap-5 sm:grid-cols-2">
          <Section label="Skip">
            <div className="flex flex-wrap gap-1.5">
              {[-30, -15, 15, 30].map((delta) => (
                <Chip
                  key={delta}
                  onClick={() => player.skip(delta)}
                  label={`${delta > 0 ? "Forward" : "Back"} ${Math.abs(delta)} seconds`}
                >
                  {delta > 0 ? "+" : "−"}
                  {Math.abs(delta)}s
                </Chip>
              ))}
            </div>
          </Section>

          <Section label="Speed">
            <div className="flex flex-wrap gap-1.5">
              {RATES.map((rate) => (
                <Chip
                  key={rate}
                  active={player.rate === rate}
                  onClick={() => player.setRate(rate)}
                  label={`Play at ${rate} times speed`}
                >
                  {rate}×
                </Chip>
              ))}
            </div>
          </Section>

          <div className="sm:col-span-2">
            <SleepSection />
          </div>
        </div>

        <Panel>
          <PaperPage />
          <ContinueReading onNavigate={close} />
          <Link
            to={`/books/${entry.id}/read`}
            onClick={close}
            className="flex items-center gap-1.5 text-xs text-ink-400 transition-colors hover:text-ink-200 focus-visible:focus-ring"
          >
            <Gi name="book-pile" className="size-3.5 text-ink-500" />
            Open the reader from the top
          </Link>
        </Panel>

        {subjects.length > 0 && (
          <Section label="Subjects">
            <div className="flex flex-wrap gap-1.5">
              {subjects.map((subject) => (
                <Link
                  key={subject}
                  to={`/books?subject=${encodeURIComponent(subject)}`}
                  onClick={close}
                  title={`Every ${subject} book on your shelf`}
                  className="rounded-full bg-fill-active px-2.5 py-1 text-xs text-ink-300 transition-colors hover:bg-fill-hover hover:text-ink-100 focus-visible:focus-ring"
                >
                  {subject}
                </Link>
              ))}
            </div>
          </Section>
        )}

        {timeline && timeline.tracks.length > 0 && (
          <Section label={`Chapters · ${timeline.tracks.length}`}>
            {timeline.degraded && (
              <p className="mb-2 text-xs leading-relaxed text-amber-300/90">
                At least one file's length could not be read, so every time after it is short by
                however long that file really runs.
              </p>
            )}
            <Chapters />
            <Link
              to="/books/files"
              onClick={close}
              className="mt-2 inline-flex items-center gap-1.5 text-xs text-ink-500 transition-colors hover:text-ink-300 focus-visible:focus-ring"
            >
              <Gi name="full-folder" className="size-3.5" />
              Manage this book's files
            </Link>
          </Section>
        )}
      </div>
    </div>,
    document.body,
  );
}

/** Elapsed, the bar, and how much book is left — at full-screen size. */
function SeekRow() {
  const { duration } = useAudioPlayer();
  const clock = useAudioClock();

  return (
    <div className="flex items-center gap-3">
      <Timecode size="lg">{formatTimecode(clock)}</Timecode>
      <SeekBar size="lg" />
      <Timecode size="lg">{duration > 0 ? formatRemaining(duration - clock) : "—"}</Timecode>
    </div>
  );
}

/**
 * The running order. Scrolled to the track that is playing when the overlay
 * opens — thirty files into a book, a list that starts at track 1 is a list
 * you have to search.
 */
function Chapters() {
  const player = useAudioPlayer();
  const { timeline, trackIndex } = player;
  const activeRef = useRef<HTMLLIElement>(null);

  useEffect(() => {
    activeRef.current?.scrollIntoView({ block: "nearest" });
    // On open only: following the tape across a boundary would yank a list
    // the listener is in the middle of reading.
  }, []);

  if (!timeline) return null;

  return (
    <ul className="max-h-72 space-y-0.5 overflow-y-auto overscroll-contain pr-1">
      {timeline.tracks.map((track, index) => (
        <li key={track.id} ref={index === trackIndex ? activeRef : undefined}>
          <button
            type="button"
            onClick={() => player.jumpToTrack(index)}
            aria-current={index === trackIndex ? "true" : undefined}
            className={cn(
              "flex w-full items-baseline gap-3 px-2 py-1.5 text-left transition-colors focus-visible:focus-ring",
              index === trackIndex
                ? "f-panel-active text-ink-100"
                : "text-ink-400 hover:text-ink-200",
            )}
          >
            <span className="w-6 shrink-0 font-display text-[10px] text-ink-500">
              {track.track_number}
            </span>
            <span className="min-w-0 flex-1 truncate text-sm">{track.title}</span>
            {track.missing && (
              <span className="shrink-0 font-display text-[9px] uppercase tracking-wider text-red-300">
                Offline
              </span>
            )}
            <span className="shrink-0 font-display text-[10px] text-ink-500">
              {track.measured ? formatTimecode(track.global_start) : "—"}
            </span>
          </button>
        </li>
      ))}
    </ul>
  );
}

/** The handoff block: where you are on paper, and the ways back to the text. */
function Panel({ children }: { children: ReactNode }) {
  return <div className="f-panel space-y-3 px-4 py-3">{children}</div>;
}

/**
 * Whether a key event came from somewhere that wants the key more than the
 * transport does. The seek bar counts: it is a range input, and its own arrow
 * keys already scrub, so the shortcuts must not scrub it twice.
 */
function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  return ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName);
}

/** Whether Space would activate this element on its own. */
function isActivatable(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return target.closest("button, a[href], summary") !== null;
}
