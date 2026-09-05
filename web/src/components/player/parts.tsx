import { cn } from "@/lib/cn";
import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { SLEEP_MINUTES, useAudioClock, useAudioPlayer } from "@/hooks/useAudioPlayer";
import { useTheme } from "@/hooks/useTheme";
import { api } from "@/lib/api";
import { explainPage, formatPage } from "@/lib/booktext";
import { formatTimecode } from "@/lib/format";
import type { GiName } from "@/lib/gameicons";
import { Gi } from "../ui/Gi";
import { Button } from "../ui/primitives";

/**
 * The leaves both halves of the player are built from.
 *
 * The bar (AudioPlayer) and the full-screen view (FullPlayer) show the same
 * transport, the same seek bar and the same readouts at two different sizes,
 * and the bar renders the full screen — so the shared pieces cannot live in
 * either file without the two importing each other in a circle.
 */

/**
 * The whole book as one track. A transparent range input sits over the frame,
 * so dragging, arrow keys and screen readers all work without hand-rolling a
 * slider; the frame underneath is the visible part, and its fill is the one
 * place inside a framed surface where the book's accent is allowed to show.
 */
export function SeekBar({ size = "sm" }: { size?: "sm" | "lg" }) {
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
      {/* The large variant asks the family for a thicker track rather than
          scaling one: --bar-h is what flat and library size .f-bar from, and
          the arcade's bar is a 6px nine-slice that would only blur. */}
      <div
        className="f-bar w-full overflow-hidden"
        style={size === "lg" ? ({ "--bar-h": "0.625rem" } as CSSProperties) : undefined}
      >
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
        className={cn(
          "absolute left-0 w-full cursor-pointer appearance-none bg-transparent opacity-0 disabled:cursor-default",
          size === "lg" ? "-inset-y-3" : "-inset-y-2",
        )}
      />
    </div>
  );
}

/** Timecodes are numbers, which is exactly what the display face is for. */
export function Timecode({ children, size = "sm" }: { children: ReactNode; size?: "sm" | "lg" }) {
  const { family } = useTheme();
  return (
    <span
      className={cn(
        "shrink-0 text-ink-400",
        family === "pixel"
          ? cn("font-display", size === "lg" ? "text-[11px]" : "text-[10px]")
          : cn("tabular-nums", size === "lg" ? "text-xs" : "text-[11px]"),
      )}
    >
      {children}
    </span>
  );
}

/**
 * A transport button. Icon-only, so the accessible name is on the button and
 * never on the icon; `step` prints how many seconds a skip moves, which is
 * the one thing the icon cannot say.
 */
export function Transport({
  label,
  icon,
  step,
  onClick,
  className,
  iconClassName,
}: {
  label: string;
  icon: GiName;
  step?: string;
  onClick: () => void;
  className?: string;
  iconClassName?: string;
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
      <Gi name={icon} className={cn("size-4", iconClassName)} />
      {step && (
        <span aria-hidden="true" className="font-display text-[9px] tracking-tight">
          {step}
        </span>
      )}
    </button>
  );
}

/** A small pill control: framed plastic in the arcade, a pill in Midnight. */
export function Chip({
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

export function Section({ label, children }: { label: ReactNode; children: ReactNode }) {
  return (
    <div>
      <p className="mb-1.5 font-display text-[10px] font-bold uppercase tracking-widest text-ink-400">
        {label}
      </p>
      {children}
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
export function PaperPage() {
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
export function ContinueReading({ onNavigate }: { onNavigate?: () => void }) {
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
      onNavigate?.();
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
export function SleepSection() {
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
