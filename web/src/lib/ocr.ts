import type { Worker } from "tesseract.js";

/**
 * Reading a page of paper with the phone that is already pointing at it.
 *
 * All of this runs in the browser. The camera is on the phone and so is the
 * compute, so nothing about what anyone is reading leaves the device, and the
 * scanner works on a LAN with no internet at all. The WASM core and the
 * English model are vendored under `/assets/ocr/` by
 * `scripts/vendor-ocr.mjs` and served from our own nginx image — design
 * invariant 5 forbids reaching for a CDN, and Tesseract.js reaches for one by
 * default, so every path below is spelled out.
 *
 * The output is deliberately not "the page". It is the longest run of clean
 * prose on the page, because that is what the passage matcher wants: a
 * contiguous stretch of the author's words, without the running head, the
 * folio, or the half-line of the next page that crept into frame.
 */

/** Where the vendored runtime lives, all of it same-origin. */
const OCR_ASSETS = "/assets/ocr";

/**
 * Widest image the recogniser is given. Tesseract wants roughly 300dpi worth
 * of pixels for body text and gets slower than linearly beyond it; a phone
 * photo of a paperback page is comfortably over that, so downscaling is free
 * accuracy-neutral speed. Images already narrower than this are left alone
 * rather than upscaled, which only ever invents detail.
 */
const MAX_WIDTH = 1600;

/**
 * The most words sent to the matcher. The server caps the query anyway; this
 * keeps the request small on a phone connection, and a match is already
 * certain long before this many words.
 */
const MAX_PASSAGE_WORDS = 220;

/** What one scan of one page produced. */
export interface PageScan {
  /** The longest run of clean prose found — what the matcher is given. */
  passage: string;
  /** Everything the recogniser read, for when the run above is wrong. */
  raw: string;
  /** A folio read off the head or foot of the page, when one looked like one. */
  pageNumber: number | null;
  /** Tesseract's own mean confidence, 0–1. */
  confidence: number;
}

/** Anything a scan can start from: the camera, or a file off the disk. */
export type ScanSource = HTMLVideoElement | HTMLCanvasElement | ImageBitmap | Blob;

/** Progress for the UI: a phase name and 0–1 within it. */
export type ScanProgress = (status: string, progress: number) => void;

let workerPromise: Promise<Worker> | null = null;

/**
 * The recogniser, started once and kept for the life of the dialog. Starting
 * it downloads about seven megabytes of core and language model, which the
 * browser then caches, so the first scan of a session is slow and the rest
 * are not.
 */
function ocrWorker(onProgress?: ScanProgress): Promise<Worker> {
  if (!workerPromise) {
    // Imported here rather than at the top of the file so the recogniser is
    // its own chunk: most sessions never scan a page, and they should not
    // pay for the scanner in the bundle that renders the shelf.
    workerPromise = import("tesseract.js").then(({ createWorker }) =>
      createWorker("eng", 1, {
        workerPath: `${OCR_ASSETS}/worker.min.js`,
        corePath: `${OCR_ASSETS}/`,
        langPath: OCR_ASSETS,
        gzip: true,
        logger: (message) => onProgress?.(message.status, message.progress),
      }),
    ).catch((error) => {
      // A failed start must not poison every later attempt: the usual cause
      // is a flaky first download of the model, and retrying works.
      workerPromise = null;
      throw error;
    });
  }
  return workerPromise;
}

/**
 * Shuts the recogniser down and gives back the tens of megabytes its model
 * occupies. Called when the scan dialog closes: a phone that keeps a loaded
 * OCR engine alive behind a reading app is a phone that reloads the tab.
 */
export function releaseOcr(): void {
  const pending = workerPromise;
  workerPromise = null;
  void pending?.then((worker) => worker.terminate()).catch(() => {});
}

/**
 * Whether a camera can be opened here, and if not, why.
 *
 * The self-hosted case makes this worth spelling out: browsers only hand out
 * a camera in a secure context, so Backhog reached over a LAN on plain HTTP
 * has no camera at all no matter what hardware is in the phone. "No camera
 * found" would send someone hunting for a permissions bug that isn't there.
 */
export function cameraAvailability(): { ok: boolean; reason?: string } {
  if (typeof navigator === "undefined" || !navigator.mediaDevices?.getUserMedia) {
    if (typeof window !== "undefined" && !window.isSecureContext) {
      return {
        ok: false,
        reason:
          "Browsers only allow a camera over HTTPS (or on localhost), and this page is neither. Pick a photo, or type a sentence from the page.",
      };
    }
    return { ok: false, reason: "This browser will not open a camera here." };
  }
  return { ok: true };
}

/**
 * Reads one page. Preprocesses, recognises, then reduces the result to the
 * longest clean run of prose and whatever looked like a folio.
 */
export async function scanPage(source: ScanSource, onProgress?: ScanProgress): Promise<PageScan> {
  const canvas = await preprocess(source);
  const worker = await ocrWorker(onProgress);
  const { data } = await worker.recognize(canvas);
  const lines = data.text.split("\n").map((line) => line.trim());
  return {
    passage: longestProseRun(lines),
    raw: data.text.trim(),
    pageNumber: folioIn(lines),
    confidence: Math.max(0, Math.min(1, data.confidence / 100)),
  };
}

/**
 * Grayscale, contrast-stretch and downscale before recognition. This is a
 * few milliseconds of canvas work and it matters more than it looks: a phone
 * photo of a page is a grey, unevenly lit, four-thousand-pixel-wide JPEG, and
 * Tesseract does visibly better on a smaller, harder-edged version of it.
 */
async function preprocess(source: ScanSource): Promise<HTMLCanvasElement> {
  const image = source instanceof Blob ? await createImageBitmap(source) : source;
  const [width, height] = sourceSize(image);
  if (width === 0 || height === 0) throw new Error("There was nothing to read in that image.");

  const scale = Math.min(1, MAX_WIDTH / width);
  const canvas = document.createElement("canvas");
  canvas.width = Math.round(width * scale);
  canvas.height = Math.round(height * scale);

  const ctx = canvas.getContext("2d", { willReadFrequently: true });
  if (!ctx) throw new Error("This browser will not give us a canvas to read the page on.");
  ctx.drawImage(image as CanvasImageSource, 0, 0, canvas.width, canvas.height);
  if (image instanceof ImageBitmap) image.close();

  const pixels = ctx.getImageData(0, 0, canvas.width, canvas.height);
  stretchToInk(pixels.data);
  ctx.putImageData(pixels, 0, 0);
  return canvas;
}

function sourceSize(image: ScanSource | ImageBitmap): [number, number] {
  if (image instanceof HTMLVideoElement) return [image.videoWidth, image.videoHeight];
  if (image instanceof HTMLCanvasElement) return [image.width, image.height];
  if (image instanceof ImageBitmap) return [image.width, image.height];
  return [0, 0];
}

/**
 * Flattens the pixels to luminance, then rescales that luminance so the
 * darkest few percent become black and the lightest few become white.
 *
 * The percentiles are what make it safe. A hard threshold turns a shadow
 * across the gutter into a black bar and eats a column of text with it;
 * stretching between the 5th and 95th percentiles pulls faint ink off grey
 * paper without deciding anything is definitely background.
 */
function stretchToInk(data: Uint8ClampedArray): void {
  const histogram = new Uint32Array(256);
  for (let i = 0; i < data.length; i += 4) {
    const luma = (data[i] * 299 + data[i + 1] * 587 + data[i + 2] * 114) / 1000;
    const level = luma | 0;
    data[i] = level;
    histogram[level] += 1;
  }

  const pixelCount = data.length / 4;
  const low = percentile(histogram, pixelCount, 0.05);
  const high = percentile(histogram, pixelCount, 0.95);
  const span = Math.max(1, high - low);

  const curve = new Uint8ClampedArray(256);
  for (let level = 0; level < 256; level += 1) {
    curve[level] = ((level - low) / span) * 255;
  }
  for (let i = 0; i < data.length; i += 4) {
    const value = curve[data[i]];
    data[i] = value;
    data[i + 1] = value;
    data[i + 2] = value;
  }
}

function percentile(histogram: Uint32Array, total: number, fraction: number): number {
  let seen = 0;
  const target = total * fraction;
  for (let level = 0; level < 256; level += 1) {
    seen += histogram[level];
    if (seen >= target) return level;
  }
  return 255;
}

/**
 * Picks the longest unbroken stretch of body text out of the recognised
 * lines.
 *
 * A photographed page is mostly prose surrounded by junk: a running head, a
 * folio, the page's own chapter title, and often a sliver of the facing page
 * where the book would not lie flat. Sending all of it to the matcher hands
 * it words that are not in a row in the book, which is exactly what its
 * shingles are built to reject. Taking the longest *contiguous* run of
 * prose-looking lines throws the furniture away and keeps a passage that
 * really does read in order.
 */
function longestProseRun(lines: string[]): string {
  let best: string[] = [];
  let current: string[] = [];

  for (const line of lines) {
    if (isProse(line)) {
      current.push(line);
      if (wordCount(current) > wordCount(best)) best = [...current];
    } else {
      current = [];
    }
  }

  // Hyphenated line breaks are the one join worth repairing: "govern-\nment"
  // is a word the book contains and "govern ment" is not.
  const joined = best
    .join("\n")
    .replace(/(\w)-\n(\w)/g, "$1$2")
    .replace(/\s*\n\s*/g, " ")
    .replace(/\s+/g, " ")
    .trim();

  const words = joined.split(" ");
  return words.length > MAX_PASSAGE_WORDS ? words.slice(0, MAX_PASSAGE_WORDS).join(" ") : joined;
}

/**
 * Whether one recognised line looks like a sentence rather than furniture or
 * noise. Three words with a couple of letters each, mostly letters: enough to
 * exclude a folio, a running head and a row of OCR gravel, and lax enough to
 * keep dialogue and short final lines of a paragraph.
 */
function isProse(line: string): boolean {
  if (line.length < 12) return false;
  const words = line.split(/\s+/).filter((word) => /[A-Za-z]{2}/.test(word));
  if (words.length < 3) return false;
  const glyphs = line.replace(/\s/g, "");
  const letters = glyphs.replace(/[^A-Za-z'’-]/g, "");
  return letters.length / glyphs.length >= 0.7;
}

function wordCount(lines: string[]): number {
  return lines.reduce((total, line) => total + line.split(/\s+/).length, 0);
}

/**
 * Reads the folio off the head or foot of the page. Page numbers live at the
 * very top or the very bottom, usually alone and occasionally beside a
 * running head, so only the outermost couple of lines are considered and only
 * a number sitting at one end of one of them counts. The foot wins ties
 * because most books number there.
 *
 * This is a suggestion, not an answer: the scan dialog fills the field in and
 * lets the reader correct it, because a folio misread as 213 instead of 218
 * would quietly poison the page map.
 */
function folioIn(lines: string[]): number | null {
  const present = lines.filter((line) => line.length > 0);
  if (present.length === 0) return null;

  const candidates = [
    ...present.slice(-2).reverse(),
    ...present.slice(0, 2),
  ];
  for (const line of candidates) {
    const match = /^(\d{1,4})\b|\b(\d{1,4})$/.exec(line);
    if (!match) continue;
    const page = Number(match[1] ?? match[2]);
    if (page > 0 && page < 10_000) return page;
  }
  return null;
}
