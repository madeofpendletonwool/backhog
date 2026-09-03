import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";

import { Dialog } from "@/components/ui/Dialog";
import { Gi } from "@/components/ui/Gi";
import { Button, Input, Label } from "@/components/ui/primitives";
import { api } from "@/lib/api";
import { cn } from "@/lib/cn";
import { cameraAvailability, releaseOcr, scanPage, type PageScan } from "@/lib/ocr";
import type { PassageResult } from "@/lib/types";

/**
 * Scan a page of the paper copy and Backhog works out where you are.
 *
 * The whole feature rests on one UI decision: the matched passage is always
 * shown, in the book's own words, beside the answer. An imperfect matcher
 * with a visible passage is perfectly usable — the reader glances at one line
 * and knows instantly whether the map is right — while a perfect-looking page
 * number with nothing behind it is a number nobody can check.
 *
 * Three ways in, and the typed one is not a fallback hidden behind a failure.
 * On a badly lit evening, typing a sentence off the page is the faster and
 * more reliable path, so it sits beside the camera as a peer.
 */

type Mode = "camera" | "photo" | "type";
type Phase = "input" | "reading" | "matching" | "matched";

interface ScanPageDialogProps {
  open: boolean;
  onClose: () => void;
  entryId: string;
  copyId: string;
  /** Pages already mapped on this copy — the improvement, made visible. */
  anchorCount: number;
}

export function ScanPageDialog({ open, onClose, entryId, copyId, anchorCount }: ScanPageDialogProps) {
  const queryClient = useQueryClient();
  const camera = cameraAvailability();

  const [mode, setMode] = useState<Mode>(camera.ok ? "camera" : "type");
  const [phase, setPhase] = useState<Phase>("input");
  const [progress, setProgress] = useState(0);
  const [status, setStatus] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [typed, setTyped] = useState("");

  const [scan, setScan] = useState<PageScan | null>(null);
  // Which path produced the offset, so an anchor records how it was made:
  // a typed sentence is a reader saying "here", a photo is a machine guess.
  const [source, setSource] = useState<"ocr" | "manual">("manual");
  const [result, setResult] = useState<PassageResult | null>(null);
  const [page, setPage] = useState("");
  const [alsoSetProgress, setAlsoSetProgress] = useState(true);

  // The engine holds tens of megabytes of language model. It is worth keeping
  // between scans in one sitting and not worth keeping after that.
  useEffect(() => {
    if (!open) releaseOcr();
  }, [open]);

  const reset = useCallback(() => {
    setPhase("input");
    setProgress(0);
    setStatus("");
    setError(null);
    setScan(null);
    setResult(null);
    setPage("");
  }, []);

  useEffect(() => {
    if (open) reset();
  }, [open, reset]);

  /** OCR, then match: the two halves of "where is this page in the book". */
  const readAndMatch = async (image: Parameters<typeof scanPage>[0]) => {
    setError(null);
    setSource("ocr");
    setPhase("reading");
    setProgress(0);
    setStatus("Starting the reader");
    try {
      const read = await scanPage(image, (phaseName, value) => {
        setStatus(phaseLabel(phaseName));
        setProgress(value);
      });
      setScan(read);
      if (read.pageNumber !== null) setPage(String(read.pageNumber));
      if (read.passage.split(" ").length < 10) {
        setPhase("input");
        setError(
          "That came back as too few readable words to place. More light, the page flatter, or type a sentence from it instead.",
        );
        return;
      }
      await match(read.passage);
    } catch (cause) {
      setPhase("input");
      setError(cause instanceof Error ? cause.message : "The page could not be read.");
    }
  };

  /** The half the typed path uses on its own. */
  const match = async (text: string) => {
    setPhase("matching");
    try {
      setResult(await api.matchBookPassage(entryId, text));
      setPhase("matched");
    } catch (cause) {
      setPhase("input");
      setError(cause instanceof Error ? cause.message : "That passage could not be placed.");
    }
  };

  const save = useMutation({
    mutationFn: async () => {
      if (!result) return;
      const printedPage = Number(page);
      if (Number.isFinite(printedPage) && printedPage > 0) {
        await api.saveBookPageAnchor(entryId, copyId, {
          printed_page: Math.round(printedPage),
          char_offset: result.match.char_offset,
          source,
          confidence: result.match.confidence,
        });
      }
      if (alsoSetProgress) {
        // 'scan' exists for exactly this: a position that came off paper,
        // distinguishable later from one the reader or the player wrote.
        await api.putBookPosition(entryId, {
          char_offset: result.match.char_offset,
          source: source === "ocr" ? "scan" : "manual",
        });
      }
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["bookCopies", entryId] }),
        queryClient.invalidateQueries({ queryKey: ["bookCopyPages", entryId, copyId] }),
        queryClient.invalidateQueries({ queryKey: ["bookPosition", entryId] }),
      ]);
      onClose();
    },
  });

  // Nothing to record is not a save: with no page number and no progress to
  // move, the button would close the dialog having done nothing at all.
  const pageIsValid = page.trim() === "" || Number(page) > 0;
  const somethingToSave = (page.trim() !== "" && Number(page) > 0) || alsoSetProgress;

  return (
    <Dialog open={open} onClose={onClose} label="Scan a page" className="max-w-xl">
      <h2 className="text-lg font-semibold text-ink-100">Scan a page</h2>
      <p className="mt-1 text-sm text-ink-400">
        {anchorCount === 0
          ? "Point at any page of prose. The first scan is what turns this printing's page numbers on."
          : `${anchorCount} page${anchorCount === 1 ? "" : "s"} mapped — accuracy improves as you scan more.`}
      </p>

      {phase === "matched" && result ? (
        <MatchReview
          result={result}
          page={page}
          onPage={setPage}
          suggestedPage={scan?.pageNumber ?? null}
          alsoSetProgress={alsoSetProgress}
          onAlsoSetProgress={setAlsoSetProgress}
          saving={save.isPending}
          canSave={pageIsValid && somethingToSave}
          error={save.error instanceof Error ? save.error.message : null}
          onRetry={reset}
          onSave={() => save.mutate()}
        />
      ) : (
        <>
          <div className="mt-4 flex gap-1.5" role="tablist" aria-label="How to read the page">
            {camera.ok && (
              <ModeTab mode="camera" current={mode} onPick={setMode} icon="camera" label="Camera" />
            )}
            <ModeTab mode="photo" current={mode} onPick={setMode} icon="download" label="Photo" />
            <ModeTab mode="type" current={mode} onPick={setMode} icon="keyboard" label="Type a sentence" />
          </div>

          {phase !== "input" ? (
            <Working phase={phase} status={status} progress={progress} />
          ) : mode === "camera" ? (
            <CameraCapture onCapture={readAndMatch} onError={setError} />
          ) : mode === "photo" ? (
            <PhotoPicker onPick={readAndMatch} />
          ) : (
            <TypeAPassage
              value={typed}
              onChange={setTyped}
              onSubmit={() => {
                setScan(null);
                setSource("manual");
                void match(typed);
              }}
            />
          )}

          {(error ?? (mode === "camera" ? null : camera.reason)) && (
            <p className="mt-3 text-sm leading-relaxed text-amber-300/90" role="status">
              {error ?? camera.reason}
            </p>
          )}
        </>
      )}
    </Dialog>
  );
}

/** Tesseract's own phase names, in words a reader would use. */
function phaseLabel(status: string): string {
  if (status.includes("core")) return "Loading the reader";
  if (status.includes("language") || status.includes("traineddata")) return "Loading English";
  if (status.includes("initializ")) return "Getting ready";
  if (status.includes("recognizing")) return "Reading the page";
  return "Working";
}

function ModeTab({
  mode,
  current,
  onPick,
  icon,
  label,
}: {
  mode: Mode;
  current: Mode;
  onPick: (mode: Mode) => void;
  icon: "camera" | "download" | "keyboard";
  label: string;
}) {
  const active = mode === current;
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={() => onPick(mode)}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-xs transition-colors focus-visible:focus-ring",
        active ? "bg-fill-active text-ink-100" : "text-ink-400 hover:text-ink-200",
      )}
    >
      <Gi name={icon} className="size-3.5" />
      {label}
    </button>
  );
}

/** The camera preview and its framing guide. */
function CameraCapture({
  onCapture,
  onError,
}: {
  onCapture: (source: HTMLCanvasElement) => void;
  onError: (message: string) => void;
}) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [ready, setReady] = useState(false);

  useEffect(() => {
    let stream: MediaStream | null = null;
    let cancelled = false;

    // The rear camera, at as many pixels as it will give: the limit on OCR
    // accuracy here is how many pixels land on an x-height, and downscaling
    // happens later where it can be done properly.
    navigator.mediaDevices
      .getUserMedia({
        video: { facingMode: { ideal: "environment" }, width: { ideal: 2560 }, height: { ideal: 1920 } },
      })
      .then((opened) => {
        if (cancelled) {
          opened.getTracks().forEach((track) => track.stop());
          return;
        }
        stream = opened;
        if (videoRef.current) {
          videoRef.current.srcObject = opened;
          void videoRef.current.play();
          setReady(true);
        }
      })
      .catch(() => {
        if (!cancelled) {
          onError("No camera available here — pick a photo, or type a sentence from the page.");
        }
      });

    return () => {
      cancelled = true;
      stream?.getTracks().forEach((track) => track.stop());
    };
    // Deliberately empty: the camera opens once for the life of this
    // component. Depending on onError would reopen it every time the dialog
    // re-renders, which on a phone is a visible black flash each keystroke.
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const capture = () => {
    const video = videoRef.current;
    if (!video || !video.videoWidth) return;
    const frame = document.createElement("canvas");
    frame.width = video.videoWidth;
    frame.height = video.videoHeight;
    frame.getContext("2d")?.drawImage(video, 0, 0);
    onCapture(frame);
  };

  return (
    <div className="mt-4">
      <div className="relative overflow-hidden rounded-xl bg-ink-950">
        <video ref={videoRef} playsInline muted className="block max-h-[45vh] w-full object-contain" />
        {/* The guide is the whole instruction: fill it with one page. */}
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-[8%] rounded-lg border-2 border-dashed border-white/40"
        />
      </div>
      <div className="mt-3 flex items-center justify-between gap-3">
        <p className="text-xs leading-relaxed text-ink-500">
          Fill the frame with one page, as flat and as lit as you can manage.
        </p>
        <Button variant="primary" disabled={!ready} onClick={capture}>
          <Gi name="camera" className="size-3.5" />
          Capture
        </Button>
      </div>
    </div>
  );
}

/** The desktop path, and the one for a photo taken earlier. */
function PhotoPicker({ onPick }: { onPick: (source: Blob) => void }) {
  return (
    <div className="mt-4">
      <label className="flex cursor-pointer flex-col items-center gap-2 rounded-xl border border-dashed border-edge-strong px-4 py-10 text-center transition-colors hover:border-outline-strong">
        <Gi name="camera" className="size-6 text-ink-500" />
        <span className="text-sm text-ink-200">Choose a photo of a page</span>
        <span className="text-xs text-ink-500">A phone photo or a scan, straight on and in focus.</span>
        <input
          type="file"
          accept="image/*"
          className="sr-only"
          onChange={(event) => {
            const file = event.target.files?.[0];
            if (file) onPick(file);
            event.target.value = "";
          }}
        />
      </label>
    </div>
  );
}

/** Typing a sentence: no engine, no download, no lighting. */
function TypeAPassage({
  value,
  onChange,
  onSubmit,
}: {
  value: string;
  onChange: (value: string) => void;
  onSubmit: () => void;
}) {
  const words = value.trim() === "" ? 0 : value.trim().split(/\s+/).length;
  return (
    <div className="mt-4">
      <Label>A sentence or two from the page</Label>
      <textarea
        value={value}
        onChange={(event) => onChange(event.target.value)}
        rows={4}
        autoFocus
        placeholder="Type it exactly as printed — a couple of lines is plenty."
        className="mt-1.5 w-full resize-y rounded-xl border border-edge bg-ink-850 p-3 text-sm text-ink-100 placeholder:text-ink-600 focus:border-brand-500/50 focus-visible:focus-ring"
      />
      <div className="mt-3 flex items-center justify-between gap-3">
        <p className="text-xs text-ink-500">
          {words < 10 ? `${words} of about 10 words` : `${words} words — plenty`}
        </p>
        <Button variant="primary" disabled={words < 10} onClick={onSubmit}>
          <Gi name="target" className="size-3.5" />
          Find this passage
        </Button>
      </div>
    </div>
  );
}

/** The wait, with the honest reason it is long the first time. */
function Working({ phase, status, progress }: { phase: Phase; status: string; progress: number }) {
  const label = phase === "matching" ? "Finding it in the book" : status || "Working";
  const pct = phase === "matching" ? 100 : Math.round(progress * 100);
  return (
    <div className="mt-6">
      <p className="font-display text-[11px] uppercase tracking-wider text-ink-300">{label}</p>
      <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-ink-800">
        <div
          className={cn(
            "h-full rounded-full bg-status-playing transition-[width] duration-300",
            phase === "matching" && "animate-pulse",
          )}
          style={{ width: `${Math.max(4, pct)}%` }}
        />
      </div>
      <p className="mt-2 text-xs leading-relaxed text-ink-500">
        The first scan downloads the reader itself, so it takes a moment. After that it is quick.
      </p>
    </div>
  );
}

/**
 * The passage, then the page number, then the offer. Order matters: the
 * reader checks the words before they are asked to commit anything, so a bad
 * match is thrown away before it can enter the map.
 */
function MatchReview({
  result,
  page,
  onPage,
  suggestedPage,
  alsoSetProgress,
  onAlsoSetProgress,
  saving,
  canSave,
  error,
  onRetry,
  onSave,
}: {
  result: PassageResult;
  page: string;
  onPage: (value: string) => void;
  suggestedPage: number | null;
  alsoSetProgress: boolean;
  onAlsoSetProgress: (value: boolean) => void;
  saving: boolean;
  canSave: boolean;
  error: string | null;
  onRetry: () => void;
  onSave: () => void;
}) {
  return (
    <div className="mt-4">
      <p className="font-display text-[11px] uppercase tracking-wider text-ink-300">
        Found this in the book
      </p>
      {/* The one thing that makes the whole feature usable. */}
      <blockquote className="mt-2 rounded-xl border border-edge bg-ink-850 p-3 text-sm leading-relaxed">
        <span className="text-ink-600">…{result.context.before}</span>
        <mark className="bg-brand-500/25 text-ink-100">{result.context.passage}</mark>
        <span className="text-ink-600">{result.context.after}…</span>
      </blockquote>
      <p className="mt-1.5 text-xs text-ink-500">
        {Math.round(result.match.confidence * 100)}% match. If that is not the page in your hands,
        scan again — nothing is recorded until you say so.
      </p>

      {result.ambiguous && (
        <p className="mt-2 text-xs leading-relaxed text-amber-300/90">
          This passage appears {result.alternatives.length + 1} times in the book, so the place above
          is a guess between them. Scanning a different paragraph off the same page will settle it.
        </p>
      )}

      <div className="mt-4">
        <Label>The page number printed on it</Label>
        <div className="mt-1.5 flex items-center gap-2">
          <Input
            type="number"
            inputMode="numeric"
            min={1}
            value={page}
            onChange={(event) => onPage(event.target.value)}
            placeholder="e.g. 214"
            className="max-w-32"
          />
          {suggestedPage !== null && String(suggestedPage) === page && (
            <span className="text-xs text-ink-500">read off the page — correct it if it is wrong</span>
          )}
        </div>
        <p className="mt-1.5 text-xs leading-relaxed text-ink-500">
          This is what pins the printing's pages to the text. Leave it blank to skip the map and just
          move your progress.
        </p>
      </div>

      <label className="mt-4 flex items-start gap-2.5 text-sm text-ink-200">
        <input
          type="checkbox"
          checked={alsoSetProgress}
          onChange={(event) => onAlsoSetProgress(event.target.checked)}
          className="mt-0.5 size-4 accent-brand-500"
        />
        <span>
          Set my progress here
          <span className="block text-xs text-ink-500">
            Scanning a page usually means "this is where I am".
          </span>
        </span>
      </label>

      {error && <p className="mt-3 text-sm text-red-300">{error}</p>}

      <div className="mt-6 flex justify-end gap-2">
        <Button variant="ghost" onClick={onRetry}>
          <Gi name="refresh" className="size-3.5" />
          Scan another
        </Button>
        <Button variant="primary" loading={saving} disabled={!canSave} onClick={onSave}>
          <Gi name="check" className="size-3.5" />
          {page.trim() === "" ? "Set my progress" : "Record this page"}
        </Button>
      </div>
    </div>
  );
}
