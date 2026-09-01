/**
 * A thin, typed door onto the browser's Barcode Detection API.
 *
 * `BarcodeDetector` ships in Chromium and is absent in Firefox and (as of
 * writing) desktop Safari, and it is not in TypeScript's DOM lib either. So
 * it is reached for through a feature test rather than declared as a global:
 * scanning is the fast path, typing the ISBN is the path that always works,
 * and the dialog has to be able to ask which one it is on.
 *
 * Nothing here loads from a third party — no scanner library, no WASM blob.
 * If the browser cannot do it, we say so and fall back.
 */

export interface DetectedBarcode {
  rawValue: string;
  format: string;
}

interface BarcodeDetectorInstance {
  detect(source: CanvasImageSource): Promise<DetectedBarcode[]>;
}

interface BarcodeDetectorConstructor {
  new (options?: { formats?: string[] }): BarcodeDetectorInstance;
  getSupportedFormats?: () => Promise<string[]>;
}

/** The formats a book barcode is actually printed in. */
export const ISBN_BARCODE_FORMATS = ["ean_13", "ean_8", "upc_a", "upc_e"];

function detectorConstructor(): BarcodeDetectorConstructor | null {
  if (typeof window === "undefined") return null;
  const ctor = (window as unknown as { BarcodeDetector?: BarcodeDetectorConstructor })
    .BarcodeDetector;
  return typeof ctor === "function" ? ctor : null;
}

/**
 * Whether this browser can scan at all. Camera permission is a separate
 * question, asked only once the user opens the scanner.
 */
export function barcodeScanningSupported(): boolean {
  return detectorConstructor() !== null && Boolean(navigator.mediaDevices?.getUserMedia);
}

/** A detector for book barcodes, or null when the browser has none. */
export function createISBNDetector(): BarcodeDetectorInstance | null {
  const ctor = detectorConstructor();
  if (!ctor) return null;
  try {
    return new ctor({ formats: ISBN_BARCODE_FORMATS });
  } catch {
    // Some builds reject an unsupported format list outright; a detector
    // over every format still finds the EAN-13 on the back of a book.
    try {
      return new ctor();
    } catch {
      return null;
    }
  }
}
