import { cn } from "@/lib/cn";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { RATES, SLEEP_MINUTES, useAudioClock, useAudioPlayer } from "@/hooks/useAudioPlayer";
import { useTheme } from "@/hooks/useTheme";
import { api, bookCoverUrl } from "@/lib/api";
import { explainPage, formatPage } from "@/lib/booktext";
import { accentStyle, byline, formatRemaining, formatTimecode } from "@/lib/format";
import type { GiName } from "@/lib/gameicons";
import { Gi } from "./ui/Gi";
import { Button, LoadingDot } from "./ui/primitives";

/**
 * The player, mounted once above the router so it keeps playing while you
 * browse. Collapsed it is a transport and a bar; expanded it grows upward
 * into the skips, the speed control, the sleep timer and the chapter list.
 *
 * Everything it shows is in **global seconds** — the bar is the whole book,
 * not the file currently open — because "twenty minutes left" is a fact about
 * a book and "twenty minutes left of part 07" is a fact about a filesystem.
 */
export function AudioPlayer() {
  const player = useAudioPlayer();
  const { entry, expanded, error, playing, loading } = player;
  const frameRef = useRef<HTMLDivElement>(null);

  /* The bar is fixed, so it would sit on top of the last inch of every page.
     Its real height goes onto the document as --player-h and <main> pads
     itself by that much: no magic number to fall out of date when the drawer
     opens, and no page re-render when it does. */
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

        {expanded && <Expanded />}

        <div className="flex items-center gap-2 sm:gap-3">
          <Link
            to={`/books/${entry.id}`}
            className="flex min-w-0 flex-1 items-center gap-3 focus-visible:focus-ring"
          >
            <span className="size-10 shrink-0 overflow-hidden rounded-xl bg-ink-800">
              {book.cover_url && (
                <img src={bookCoverUrl(book.id)} alt="" className="size-full object-cover" />
              )}
            </span>
            <span className="min-w-0 flex-1">
              {/* The title is prose, not a label: system face, real case. */}
              <span className="block truncate text-sm font-medium text-ink-100">{book.title}</span>
              <span className="block truncate text-xs text-ink-400">
                {byline(book) || "Audiobook"}
              </span>
            </span>
          </Link>

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
              label={expanded ? "Hide player controls" : "Show player controls"}
              icon={expanded ? "chevron-down" : "chevron-up"}
              onClick={() => player.setExpanded(!expanded)}
            />
            <Transport label="Close player" icon="x" onClick={player.close} />
          </div>
        </div>

        <SeekRow />
      </section>
    </div>
  );
}

/** Elapsed, the bar, and how much book is left after here. */
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

/**
 * The whole book as one track. A transparent range input sits over the frame,
 * so dragging, arrow keys and screen readers all work without hand-rolling a
 * slider; the frame underneath is the visible part, and its fill is the one
 * place inside a framed surface where the book's accent is allowed to show.
 */
function SeekBar() {
  const { duration, seek } = useAudioPlayer();
  const clock = useAudioClock();
  const [scrubbing, setScrubbing] = useState<number | null>(null);

  const value = scrubbing ?? clock;
  const percent = duration > 0 ? Math.min(100, Math.max(0, (value / duration) * 100)) : 0;

  const commit = () => {
    if (scrubbing == null) return;
    seek(scrubbing);
    setScrubbing(null);
  };

  return (
    <div className="relative min-w-0 flex-1 has-[:focus-visible]:focus-ring">
      <div className="f-bar w-full overflow-hidden">
        <div className="h-full" style={{ width: `${percent}%`, background: "var(--accent)" }} />
      </div>
      <input
        type="range"
        min={0}
        max={Math.max(1, Math.round(duration))}
        step={1}
        value={Math.round(value)}
        disabled={duration <= 0}
        aria-label="Position in the book"
        aria-valuetext={`${formatTimecode(value)} of ${formatTimecode(duration)}`}
        onChange={(event) => setScrubbing(Number(event.target.value))}
        onPointerUp={commit}
        onPointerCancel={commit}
        onKeyUp={commit}
        onBlur={commit}
        className="absolute -inset-y-2 left-0 w-full cursor-pointer appearance-none bg-transparent opacity-0 disabled:cursor-default"
      />
    </div>
  );
}

/** The drawer: the way back to the text, skips, speed, sleep, and chapters. */
function Expanded() {
  const player = useAudioPlayer();
  const { timeline, trackIndex } = player;

  return (
    <div className="mb-3 space-y-4 border-b-2 border-line pb-3">
      <PaperPage />

      <ContinueReading />

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

      <SleepSection />

      {timeline && timeline.tracks.length > 0 && (
        <Section label={`Chapters · ${timeline.tracks.length}`}>
          {timeline.degraded && (
            <p className="mb-2 text-xs leading-relaxed text-amber-300/90">
              At least one file's length could not be read, so every time after it is short by
              however long that file really runs.
            </p>
          )}
          <ul className="max-h-64 space-y-0.5 overflow-y-auto pr-1">
            {timeline.tracks.map((track, index) => (
              <li key={track.id}>
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
        </Section>
      )}
    </div>
  );
}

/**
 * Where the tape is, in the paper copy: "page 214 ± 3".
 *
 * This is the third leg of the translation closing — you are listening, and
 * the app can still tell you which page of the paperback on your nightstand
 * you would be looking at. It needs both maps (audio→text through the
 * alignment, text→page through the scans), so it is simply absent unless both
 * exist.
 *
 * The lookup is speculative and bucketed to the half minute rather than run
 * on every tick: a page number that moves once a minute is exactly as useful
 * as one that moves sixty times, and costs a sixtieth as much.
 */
function PaperPage() {
  const player = useAudioPlayer();
  const clock = useAudioClock();
  const entry = player.entry;
  const bucket = Math.floor(clock / PAGE_REFRESH_SECONDS) * PAGE_REFRESH_SECONDS;

  const { data } = useQuery({
    queryKey: ["bookPagePosition", entry?.id, bucket],
    queryFn: () => api.translateBookPosition(entry!.id, { audio: bucket }),
    enabled: Boolean(entry),
    retry: false,
    staleTime: Infinity,
  });

  const page = data?.page ?? null;
  if (!page) return null;

  return (
    <p
      className="font-display text-[11px] uppercase tracking-wider text-ink-300"
      title={explainPage(page) ?? undefined}
    >
      <Gi name="book-pile" className="mr-1.5 inline size-3.5 text-ink-500" />
      {formatPage(page)} in your paper copy
    </p>
  );
}

/** How coarsely the paper-page readout follows the tape, in seconds. */
const PAGE_REFRESH_SECONDS = 30;

/**
 * The reverse handoff: "Continue reading" opens the reader on the sentence
 * being narrated right now. The translation is the server's — a speculative
 * lookup, nothing stored — and the player keeps playing through the
 * navigation, so the reader's read-along takes over where the tap happened.
 */
function ContinueReading() {
  const player = useAudioPlayer();
  const clock = useAudioClock();
  const navigate = useNavigate();
  const entry = player.entry;

  // The stored position's audio view doubles as the alignment check: it is
  // derived exactly when a map exists for this book.
  const { data: position } = useQuery({
    queryKey: ["bookPosition", entry?.id],
    queryFn: () => api.bookPosition(entry!.id),
    enabled: Boolean(entry),
  });
  const aligned = position?.audio?.derived === true;

  const [pending, setPending] = useState(false);
  const [hint, setHint] = useState<string | null>(null);
  const hintTimer = useRef(0);
  useEffect(() => () => window.clearTimeout(hintTimer.current), []);

  const say = (message: string) => {
    setHint(message);
    window.clearTimeout(hintTimer.current);
    hintTimer.current = window.setTimeout(() => setHint(null), 6000);
  };

  if (!entry) return null;

  const reason = aligned
    ? "Open the reader on the sentence being narrated"
    : "This audiobook isn't aligned to the text yet — run Align on the book's page, and this opens the reader in the right place";

  const open = async () => {
    if (pending) return;
    setPending(true);
    try {
      const translation = await api.translateBookPosition(entry.id, { audio: clock });
      navigate(`/books/${entry.id}/read?offset=${translation.char_offset}`);
    } catch {
      say("This audiobook isn't aligned to the text yet — run Align on the book's page.");
    } finally {
      setPending(false);
    }
  };

  return (
    <div>
      <Button variant="secondary" className="w-full" disabled={!aligned} loading={pending} onClick={open} title={reason}>
        <Gi name="scroll-unfurled" className="size-3.5" />
        Continue reading
      </Button>
      {!aligned && position && (
        <p className="mt-1.5 text-[11px] leading-relaxed text-ink-500">{reason}.</p>
      )}
      {hint && <p className="mt-1.5 text-[11px] leading-relaxed text-amber-300/90">{hint}</p>}
    </div>
  );
}

/**
 * The sleep timer. "End of chapter" is the one people actually want — a clock
 * stops mid-sentence, a chapter boundary does not.
 */
function SleepSection() {
  const { sleep, setSleep } = useAudioPlayer();

  return (
    <Section
      label={
        sleep?.kind === "duration" ? (
          <>
            Sleep · <SleepCountdown endsAt={sleep.endsAt} />
          </>
        ) : sleep ? (
          "Sleep · at the end of this chapter"
        ) : (
          "Sleep timer"
        )
      }
    >
      <div className="flex flex-wrap gap-1.5">
        <Chip active={sleep === null} onClick={() => setSleep(null)} label="No sleep timer">
          Off
        </Chip>
        {SLEEP_MINUTES.map((minutes) => (
          <Chip
            key={minutes}
            active={sleep?.kind === "duration" && sleep.minutes === minutes}
            onClick={() =>
              setSleep({ kind: "duration", minutes, endsAt: Date.now() + minutes * 60_000 })
            }
            label={`Stop in ${minutes} minutes`}
          >
            {minutes}m
          </Chip>
        ))}
        <Chip
          active={sleep?.kind === "chapter"}
          onClick={() => setSleep({ kind: "chapter" })}
          label="Stop at the end of this chapter"
        >
          <Gi name="night-sleep" className="size-3" />
          End of chapter
        </Chip>
      </div>
    </Section>
  );
}

/**
 * "4:12" and falling. A leaf of its own, ticking once a second, so a running
 * sleep timer does not put the whole player on a one-second render loop.
 */
function SleepCountdown({ endsAt }: { endsAt: number }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, []);
  return <>{formatTimecode(Math.max(0, (endsAt - now) / 1000))}</>;
}

function Section({ label, children }: { label: ReactNode; children: ReactNode }) {
  return (
    <div>
      <p className="mb-1.5 font-display text-[10px] font-bold uppercase tracking-widest text-ink-400">
        {label}
      </p>
      {children}
    </div>
  );
}

/** A small pill control: framed plastic in the arcade, a pill in Midnight. */
function Chip({
  children,
  onClick,
  active = false,
  label,
}: {
  children: ReactNode;
  onClick: () => void;
  active?: boolean;
  label: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      aria-pressed={active}
      className={cn(
        "inline-flex items-center gap-1.5 px-2.5 py-1 font-display text-[10px] uppercase tracking-wider transition-colors focus-visible:focus-ring",
        active ? "f-chip-active text-ink-100" : "f-chip text-ink-400 hover:text-ink-200",
      )}
    >
      {children}
    </button>
  );
}

/**
 * A transport button. Icon-only, so the accessible name is on the button and
 * never on the icon; `step` prints how many seconds a skip moves, which is
 * the one thing the icon cannot say.
 */
function Transport({
  label,
  icon,
  step,
  onClick,
  className,
}: {
  label: string;
  icon: GiName;
  step?: string;
  onClick: () => void;
  className?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      title={label}
      className={cn(
        "flex h-9 shrink-0 items-center justify-center gap-0.5 px-1.5 text-ink-300 transition-colors hover:text-ink-100 focus-visible:focus-ring",
        className,
      )}
    >
      <Gi name={icon} className="size-4" />
      {step && (
        <span aria-hidden="true" className="font-display text-[9px] tracking-tight">
          {step}
        </span>
      )}
    </button>
  );
}

/** Timecodes are numbers, which is exactly what the display face is for. */
function Timecode({ children }: { children: ReactNode }) {
  const { family } = useTheme();
  return (
    <span
      className={cn(
        "shrink-0 text-ink-400",
        family === "pixel" ? "font-display text-[10px]" : "text-[11px] tabular-nums",
      )}
    >
      {children}
    </span>
  );
}
