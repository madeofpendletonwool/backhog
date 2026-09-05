import { useEffect, useRef } from "react";

import { useAudioClock, useAudioPlayer } from "@/hooks/useAudioPlayer";
import { bookCoverUrl } from "@/lib/api";
import { accentStyle, byline, formatRemaining, formatTimecode } from "@/lib/format";
import { Gi } from "../ui/Gi";
import { Button, LoadingDot } from "../ui/primitives";
import { FullPlayer } from "./FullPlayer";
import { SeekBar, Timecode, Transport } from "./parts";

/**
 * The player's bar, mounted once above the router so it keeps playing while
 * you browse. It is deliberately thin: a transport, an identity, and the whole
 * book as one seek bar. Everything else — skips, speed, the sleep timer, the
 * chapter list, the way back to the text — lives in the full-screen view,
 * which the bar raises and which renders over it.
 *
 * Everything it shows is in **global seconds** — the bar is the whole book,
 * not the file currently open — because "twenty minutes left" is a fact about
 * a book and "twenty minutes left of part 07" is a fact about a filesystem.
 */
export function AudioPlayer() {
  const player = useAudioPlayer();
  const { entry, error, playing, loading } = player;
  const frameRef = useRef<HTMLDivElement>(null);

  /* The bar is fixed, so it would sit on top of the last inch of every page.
     Its real height goes onto the document as --player-h and <main> pads
     itself by that much: no magic number to fall out of date, and nothing for
     the full-screen view to disturb — the bar stays mounted underneath it. */
  useEffect(() => {
    const node = frameRef.current;
    const root = document.documentElement;
    if (!node) {
      root.style.removeProperty("--player-h");
      return;
    }
    const observer = new ResizeObserver(([box]) => {
      root.style.setProperty("--player-h", `${Math.round(box.contentRect.height) + 24}px`);
    });
    observer.observe(node);
    return () => {
      observer.disconnect();
      root.style.removeProperty("--player-h");
    };
  }, [entry]);

  if (!entry) return null;
  const { book } = entry;

  return (
    <>
      <div
        ref={frameRef}
        style={accentStyle(book)}
        className="fixed inset-x-2 bottom-2 z-40 lg:left-[17rem]"
      >
        <section className="f-panel px-3 py-2.5" aria-label="Audiobook player">
          {error && (
            <p className="mb-2.5 flex items-start gap-2 px-1 text-xs leading-relaxed text-red-300">
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

          <div className="flex items-center gap-2 sm:gap-3">
            {/* The identity block *is* the expand gesture, and it is flex-1 so
                it also owns the dead space between the title and the
                transport: anything on the bar that is not a button opens the
                full player. The link to the book's page moves in there. */}
            <button
              type="button"
              onClick={() => player.setFullscreen(true)}
              aria-label={`Open the full player for ${book.title}`}
              aria-haspopup="dialog"
              className="flex min-w-0 flex-1 items-center gap-3 text-left focus-visible:focus-ring"
            >
              <span className="size-10 shrink-0 overflow-hidden rounded-xl bg-ink-800">
                {book.cover_url && (
                  <img src={bookCoverUrl(book.id)} alt="" className="size-full object-cover" />
                )}
              </span>
              <span className="min-w-0 flex-1">
                {/* The title is prose, not a label: system face, real case. */}
                <span className="block truncate text-sm font-medium text-ink-100">
                  {book.title}
                </span>
                <span className="block truncate text-xs text-ink-400">
                  {byline(book) || "Audiobook"}
                </span>
              </span>
            </button>

            <div className="flex shrink-0 items-center gap-0.5 sm:gap-1">
              <Transport
                label="Previous track"
                icon="skip-back"
                onClick={player.previousTrack}
                className="hidden sm:flex"
              />
              <Transport
                label="Back 30 seconds"
                icon="rewind"
                step="30"
                onClick={() => player.skip(-30)}
              />
              <Button
                variant="primary"
                size="icon"
                onClick={player.toggle}
                aria-label={playing ? "Pause" : "Play"}
                className="mx-0.5"
              >
                {loading && !playing ? (
                  <LoadingDot />
                ) : (
                  <Gi name={playing ? "pause" : "play"} className="size-4" />
                )}
              </Button>
              <Transport
                label="Forward 30 seconds"
                icon="fast-forward"
                step="30"
                onClick={() => player.skip(30)}
              />
              <Transport
                label="Next track"
                icon="skip-forward"
                onClick={player.nextTrack}
                className="hidden sm:flex"
              />
            </div>

            <div className="flex shrink-0 items-center gap-0.5">
              <Transport
                label="Open full player"
                icon="chevrons-up"
                onClick={() => player.setFullscreen(true)}
              />
              <Transport label="Close player" icon="x" onClick={player.close} />
            </div>
          </div>

          <SeekRow />
        </section>
      </div>

      <FullPlayer />
    </>
  );
}

/**
 * Elapsed, the bar, and how much book is left after here. Its own component
 * so the clock's four-times-a-second tick re-renders the timecodes and the
 * bar, and not the transport above them.
 */
function SeekRow() {
  const { duration } = useAudioPlayer();
  const clock = useAudioClock();

  return (
    <div className="mt-2 flex items-center gap-2.5">
      <Timecode>{formatTimecode(clock)}</Timecode>
      <SeekBar />
      <Timecode>{duration > 0 ? formatRemaining(duration - clock) : "—"}</Timecode>
    </div>
  );
}
